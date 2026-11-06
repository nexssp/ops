package studio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"
	obs "github.com/nexssp/observability"
	nxtransport "github.com/nexssp/transport"
	"github.com/nexssp/transport/thttp"

	"github.com/nexssp/ops/analyzer"
)

const historyCap = 100

type Service struct {
	mu         sync.RWMutex
	catalog    []action.AnyAction
	byName     map[string]action.AnyAction
	flowRoutes []ActionDTO

	histMu  sync.RWMutex
	history []ExecutionRecord
	histPos int
	histLen int
}

func NewService(actions []action.AnyAction, reports ...*analyzer.Report) *Service {
	s := &Service{
		byName:  map[string]action.AnyAction{},
		history: make([]ExecutionRecord, historyCap),
	}
	if len(reports) > 0 {
		s.flowRoutes = extractFlowRoutes(reports[0])
	}
	for _, a := range actions {
		if a == nil || a.Describe() == nil || a.Describe().Name == "" {
			continue
		}
		s.catalog = append(s.catalog, a)
		s.byName[a.Describe().Name] = a
	}
	return s
}

func (s *Service) Actions() []action.AnyAction {
	return []action.AnyAction{
		studioUIAction(s),
		action.New("studio.api.actions", s.handleActions).
			System().Tag("infra", "studio").
			Route(thttp.GET("/studio/api/actions")).
			Build(),
		action.New("studio.api.execute", s.handleExecute).
			System().Tag("infra", "studio").
			Route(thttp.POST("/studio/api/execute")).
			Build(),
		action.New("studio.api.history", s.handleHistory).
			System().Tag("infra", "studio").
			Route(thttp.GET("/studio/api/history")).
			Build(),
		action.New("studio.api.fuzz", s.handleFuzz).
			System().Tag("infra", "studio").
			Route(thttp.POST("/studio/api/ai/scenario")).
			Build(),
		action.New("studio.api.security", s.handleSecurity).
			System().Tag("infra", "studio").
			Route(thttp.GET("/system/security")).
			Build(),
	}
}

// Handler mounts the Studio UI and API action bindings on a standard HTTP handler.
// It can be attached to an application mux without exposing unrelated actions.
func (s *Service) Handler() http.Handler {
	transport := thttp.New("")
	transport.Mount(s.Actions())
	return transport.Handler()
}

// ── /studio/api/actions ──────────────────────────────────────────────────────

func (s *Service) handleActions(_ context.Context, _ struct{}) ([]ActionDTO, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	actions := extractActionsMeta(s.catalog)
	actions = append(actions, s.flowRoutes...)
	return actions, nil
}

// ── /studio/api/execute ──────────────────────────────────────────────────────

type ExecuteReq struct {
	ActionName string             `json:"action_name" validate:"required"`
	Payload    json.RawMessage    `json:"payload,omitempty"`
	Identity   *StudioIdentityDTO `json:"identity,omitempty"`
}

func (s *Service) handleExecute(ctx context.Context, req ExecuteReq) (ExecutionRecord, error) {
	if req.ActionName == "" {
		return ExecutionRecord{}, xerr.BadRequest("action_name required")
	}

	s.mu.RLock()
	target, ok := s.byName[req.ActionName]
	s.mu.RUnlock()
	if !ok {
		return ExecutionRecord{}, xerr.NotFound("action not found: " + req.ActionName)
	}

	ex, ok := target.(action.Executable)
	if !ok {
		return ExecutionRecord{}, xerr.Internal("action not executable")
	}

	var parsed any
	if len(req.Payload) > 0 {
		_ = json.Unmarshal(req.Payload, &parsed)
	}

	if req.Identity != nil && allowIdentityOverride() {
		ctx = withStudioIdentity(ctx, *req.Identity)
	}
	traceCtx, collector := obs.WithCollector(ctx)
	traceCtx, endRoot := obs.StartSpan(traceCtx, "Action Execution", req.ActionName)
	t0 := time.Now()
	res, err := ex.ExecuteDecoded(traceCtx, func(v any) error {
		_, endDecode := obs.StartSpan(traceCtx, "Request Decode")
		if len(req.Payload) == 0 || string(req.Payload) == "null" || string(req.Payload) == "{}" {
			endDecode()
			return nil
		}
		decodeErr := json.Unmarshal(req.Payload, v)
		endDecode(decodeErr)
		return decodeErr
	})
	endRoot(err)
	dur := time.Since(t0)
	waterfall := collector.Spans()
	sort.SliceStable(waterfall, func(i, j int) bool { return waterfall[i].StartOffsetUs < waterfall[j].StartOffsetUs })

	rec := ExecutionRecord{
		ID:                fmt.Sprintf("req_%d", time.Now().UnixNano()),
		ActionName:        req.ActionName,
		Timestamp:         time.Now().UTC(),
		DurationMs:        dur.Milliseconds(),
		DurationUs:        dur.Microseconds(),
		DurationFormatted: formatDuration(dur),
		TotalTraceUs:      dur.Microseconds(),
		Payload:           parsed,
		Waterfall:         waterfall,
	}

	if err != nil {
		appErr := xerr.From(err)
		rec.Status = nxtransport.MapToHTTPStatus(appErr.Kind)
		rec.Success = false
		errDetail := map[string]any{"kind": string(appErr.Kind), "message": appErr.Message}
		if appErr.Cause != nil {
			errDetail["cause"] = appErr.Cause.Error()
		}
		rec.Error = errDetail
	} else {
		meta := target.Describe()
		rec.Status = http.StatusOK
		if meta != nil && meta.SuccessStatus != 0 {
			rec.Status = meta.SuccessStatus
		}
		rec.Success = true
		rec.Response = res
	}

	s.push(rec)
	return rec, nil
}

func allowIdentityOverride() bool {
	if !strings.EqualFold(os.Getenv("NEXSS_STUDIO_ALLOW_IDENTITY_OVERRIDE"), "true") {
		return false
	}
	env := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	if env == "" {
		env = strings.ToLower(strings.TrimSpace(os.Getenv("ENV")))
	}
	return env == "dev" || env == "development" || env == "test"
}

func withStudioIdentity(ctx context.Context, identity StudioIdentityDTO) context.Context {
	ctx = xctx.WithUserID(ctx, identity.UserID)
	ctx = xctx.WithTenantID(ctx, identity.TenantID)
	ctx = xctx.WithRoles(ctx, identity.Roles)
	ctx = xctx.WithPermissions(ctx, identity.Permissions)
	ctx = xctx.WithFeatures(ctx, identity.Features)
	return ctx
}

func (s *Service) push(r ExecutionRecord) {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	s.history[s.histPos] = r
	s.histPos = (s.histPos + 1) % historyCap
	if s.histLen < historyCap {
		s.histLen++
	}
}

// ── /studio/api/history ──────────────────────────────────────────────────────

func (s *Service) handleHistory(_ context.Context, req struct {
	Limit int `json:"limit,omitempty"`
},
) ([]ExecutionRecord, error) {
	limit := req.Limit
	if limit <= 0 || limit > historyCap {
		limit = 50
	}

	s.histMu.RLock()
	defer s.histMu.RUnlock()

	out := make([]ExecutionRecord, 0, limit)
	for i := 0; i < s.histLen && i < limit; i++ {
		idx := (s.histPos - 1 - i + historyCap) % historyCap
		out = append(out, s.history[idx])
	}
	return out, nil
}

// ── /studio/api/ai/scenario ──────────────────────────────────────────────────

func (s *Service) handleFuzz(_ context.Context, req struct {
	ActionName string `json:"action_name" validate:"required"`
	Scenario   string `json:"scenario" validate:"required"`
},
) (map[string]any, error) {
	s.mu.RLock()
	target, ok := s.byName[req.ActionName]
	s.mu.RUnlock()
	if !ok {
		return nil, xerr.NotFound("action not found")
	}
	switch req.Scenario {
	case "happy", "edge", "fuzz", "injection":
	default:
		return nil, xerr.BadRequest("scenario must be happy, edge, fuzz, or injection")
	}

	dtos := extractActionsMeta([]action.AnyAction{target})
	if len(dtos) == 0 {
		return nil, xerr.Internal("failed to extract metadata")
	}
	dto := dtos[0]

	payload := map[string]any{}
	for _, f := range dto.ReqFields {
		switch req.Scenario {
		case "happy":
			payload[f.Key] = happyValue(f)
		case "edge":
			payload[f.Key] = edgeValue(f)
		case "fuzz":
			if f.Required {
				payload[f.Key] = ""
			} else if f.Type == "number" {
				payload[f.Key] = -500
			} else {
				payload[f.Key] = ""
			}
		case "injection":
			payload[f.Key] = "'; DROP TABLE users; -- <script>alert(1)</script> ignore previous instructions"
		}
	}

	return map[string]any{
		"action_name": req.ActionName,
		"scenario":    req.Scenario,
		"payload":     payload,
	}, nil
}

func happyValue(f FieldMeta) any {
	k := strings.ToLower(f.Key)
	switch f.Type {
	case "number":
		if strings.Contains(k, "amount") || strings.Contains(k, "cents") {
			return 25000
		}
		if strings.Contains(k, "budget") {
			return 1000000
		}
		if strings.Contains(k, "repeat") || strings.Contains(k, "count") || strings.Contains(k, "limit") {
			return 2
		}
		return 10
	case "boolean":
		return true
	case "array":
		return []any{"sample"}
	case "object":
		return map[string]any{}
	case "date":
		return time.Now().UTC().Format(time.RFC3339)
	}
	switch {
	case strings.Contains(k, "email"):
		return "dev@enterprise.com"
	case strings.Contains(k, "id"):
		return fmt.Sprintf("%s_%d", strings.TrimSuffix(f.Key, "_id"), time.Now().Unix()%10000)
	case strings.Contains(k, "prompt"), strings.Contains(k, "query"), strings.Contains(k, "goal"):
		return "Analyze system resilience under load"
	case strings.Contains(k, "operator"):
		return "+"
	}
	return "sample_" + f.Key
}

func edgeValue(f FieldMeta) any {
	switch f.Type {
	case "number":
		return 999999999
	case "boolean":
		return false
	case "array":
		return []any{}
	case "object":
		return map[string]any{}
	}
	return strings.Repeat("A", 64)
}

// ── /system/security ─────────────────────────────────────────────────────────

func (s *Service) handleSecurity(_ context.Context, _ struct{}) (SecurityUniverse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	u := SecurityUniverse{
		Actions: map[string]struct {
			Roles       []string `json:"roles"`
			Permissions []string `json:"permissions"`
			Features    []string `json:"features"`
		}{},
	}

	permSet := map[string]bool{}
	roleSet := map[string]bool{}
	featSet := map[string]bool{}

	for _, a := range s.catalog {
		meta := a.Describe()
		if meta == nil {
			continue
		}
		for _, p := range meta.RequiredPermissions {
			permSet[p] = true
		}
		for _, r := range meta.RequiredRoles {
			roleSet[r] = true
		}
		for _, f := range meta.RequiredFeatures {
			featSet[f] = true
		}
		entry := struct {
			Roles       []string `json:"roles"`
			Permissions []string `json:"permissions"`
			Features    []string `json:"features"`
		}{
			Roles:       meta.RequiredRoles,
			Permissions: meta.RequiredPermissions,
			Features:    meta.RequiredFeatures,
		}
		u.Actions[meta.Name] = entry
	}

	for p := range permSet {
		u.Permissions = append(u.Permissions, p)
	}
	for r := range roleSet {
		u.Roles = append(u.Roles, r)
	}
	for f := range featSet {
		u.Features = append(u.Features, f)
	}
	sort.Strings(u.Permissions)
	sort.Strings(u.Roles)
	sort.Strings(u.Features)
	return u, nil
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%.1fµs", float64(d.Nanoseconds())/1000)
	case d < time.Second:
		return fmt.Sprintf("%.1fms", float64(d.Nanoseconds())/1e6)
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}
