package studio

import (
	"reflect"
	"strings"
	"time"

	"github.com/nexssp/kernel/action"
	obs "github.com/nexssp/observability"
	"github.com/nexssp/ops/analyzer"
	"github.com/nexssp/transport/thttp"
)

type FieldMeta struct {
	Key        string `json:"key"`
	Type       string `json:"type"`
	Required   bool   `json:"required"`
	Label      string `json:"label"`
	Validation string `json:"validation,omitempty"`
	Default    any    `json:"default_value"`
}

type ActionDTO struct {
	Name             string      `json:"name"`
	Kind             string      `json:"kind"`
	Description      string      `json:"description"`
	Method           string      `json:"method"`
	Path             string      `json:"path"`
	SourceFile       string      `json:"source_file,omitempty"`
	SourceLine       int         `json:"source_line,omitempty"`
	SuccessStatus    int         `json:"success_status,omitempty"`
	FlowSchemaDriven bool        `json:"flow_schema_driven,omitempty"`
	ReqSchemaName    string      `json:"request_schema,omitempty"`
	ResSchemaName    string      `json:"response_schema,omitempty"`
	ReqType          string      `json:"req_type"`
	ResType          string      `json:"res_type"`
	Tags             []string    `json:"tags"`
	Node             string      `json:"node,omitempty"`
	Category         string      `json:"category"`
	IsSystem         bool        `json:"is_system"`
	RequiresAuth     bool        `json:"requires_auth"`
	RequiredRoles    []string    `json:"required_roles,omitempty"`
	RequiredPerms    []string    `json:"required_permissions,omitempty"`
	RequiredFeatures []string    `json:"required_features,omitempty"`
	Idempotent       bool        `json:"idempotent"`
	TimeoutMs        int64       `json:"timeout_ms,omitempty"`
	RetryMax         int         `json:"retry_max,omitempty"`
	RateLimit        string      `json:"rate_limit,omitempty"`
	CacheTTLMs       int64       `json:"cache_ttl_ms,omitempty"`
	ConcurrencyLimit int32       `json:"concurrency_limit,omitempty"`
	Deduplicated     bool        `json:"deduplicated"`
	Coalesced        bool        `json:"coalesced"`
	ReqFields        []FieldMeta `json:"req_fields"`
	ResFields        []FieldMeta `json:"res_fields"`
}

type ExecutionRecord struct {
	ID                string              `json:"id"`
	ActionName        string              `json:"action_name"`
	Timestamp         time.Time           `json:"timestamp"`
	Status            int                 `json:"status"`
	Success           bool                `json:"success"`
	DurationMs        int64               `json:"duration_ms"`
	DurationUs        int64               `json:"duration_us"`
	DurationFormatted string              `json:"duration_formatted"`
	TotalTraceUs      int64               `json:"total_trace_us"`
	Payload           any                 `json:"payload,omitempty"`
	Response          any                 `json:"response,omitempty"`
	Error             any                 `json:"error,omitempty"`
	Waterfall         []obs.CompletedSpan `json:"waterfall"`
}

type StudioIdentityDTO struct {
	UserID      string   `json:"user_id"`
	TenantID    string   `json:"tenant_id"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
	Features    []string `json:"features"`
}

type SecurityUniverse struct {
	Permissions []string `json:"permissions"`
	Roles       []string `json:"roles"`
	Features    []string `json:"features"`
	Actions     map[string]struct {
		Roles       []string `json:"roles"`
		Permissions []string `json:"permissions"`
		Features    []string `json:"features"`
	} `json:"actions"`
}

func extractActionsMeta(actions []action.AnyAction) []ActionDTO {
	var out []ActionDTO
	seen := map[string]bool{}

	for _, act := range actions {
		if act == nil {
			continue
		}
		meta := act.Describe()
		if meta == nil || meta.Name == "" || seen[meta.Name] || strings.HasPrefix(meta.Name, "studio.") {
			continue
		}
		seen[meta.Name] = true
		category := "Business"
		if meta.IsSystem() {
			category = "System / Infra"
		} else if len(meta.Tags) > 0 && meta.Tags[0] != "" {
			category = strings.ToUpper(meta.Tags[0][:1]) + meta.Tags[0][1:]
		}

		dto := ActionDTO{
			Name:             meta.Name,
			Kind:             "action",
			Description:      meta.Description,
			Method:           "POST",
			Path:             "/" + meta.Name,
			Tags:             meta.Tags,
			Node:             meta.Node,
			Category:         category,
			IsSystem:         meta.IsSystem(),
			RequiresAuth:     meta.RequiresAuth,
			RequiredRoles:    meta.RequiredRoles,
			RequiredPerms:    meta.RequiredPermissions,
			RequiredFeatures: meta.RequiredFeatures,
			Idempotent:       meta.Idempotency.Enabled,
			TimeoutMs:        meta.Timeout.Milliseconds(),
			RetryMax:         meta.RetryMax,
			RateLimit:        meta.RateLimit,
			CacheTTLMs:       meta.CacheTTL.Milliseconds(),
			ConcurrencyLimit: meta.ConcurrencyLimit,
			Deduplicated:     meta.Deduplicated,
			Coalesced:        meta.Coalesced,
			ReqFields:        []FieldMeta{},
			ResFields:        []FieldMeta{},
		}

		for _, b := range act.GetBindings() {
			switch v := b.(type) {
			case thttp.HTTPRoute:
				dto.Method, dto.Path = v.Method, v.Path
			case thttp.RawHTTPHandler:
				dto.Method, dto.Path = v.Method, v.Path
			}
		}

		if typed, ok := act.(action.TypedPayload); ok {
			if rp := typed.ReqPayload(); rp != nil {
				t := reflect.TypeOf(rp)
				dto.ReqType = typeName(t)
				dto.ReqFields = extractFields(t)
			} else {
				dto.ReqType = "struct{}"
			}
			if sp := typed.ResPayload(); sp != nil {
				t := reflect.TypeOf(sp)
				dto.ResType = typeName(t)
				dto.ResFields = extractFields(t)
			} else {
				dto.ResType = "struct{}"
			}
		} else {
			dto.ReqType, dto.ResType = "any", "any"
		}

		out = append(out, dto)
	}
	return out
}

func extractFlowRoutes(report *analyzer.Report) []ActionDTO {
	if report == nil || len(report.FlowHTTPRoutes) == 0 {
		return nil
	}

	contractsByRoute := make(map[string]analyzer.ActionContract, len(report.Actions))
	for _, contract := range report.Actions {
		if contract.RouteSource == "nflow" {
			contractsByRoute[contract.Method+" "+contract.Path] = contract
		}
	}

	routes := make([]ActionDTO, 0, len(report.FlowHTTPRoutes))
	for _, route := range report.FlowHTTPRoutes {
		if route.Pipeline == "" || route.Method == "" || route.Path == "" {
			continue
		}
		contract := contractsByRoute[route.Method+" "+route.Path]
		description := contract.Description
		if description == "" {
			description = "NFlow pipeline " + route.Pipeline
		}
		reqType, resType := contract.ReqTypeName, contract.ResTypeName
		if reqType == "" {
			reqType = "any"
		}
		if resType == "" {
			resType = "any"
		}
		sourceFile, sourceLine := route.SourceFile, route.SourceLine
		if sourceFile == "" {
			sourceFile = contract.SourceFile
		}
		if sourceLine == 0 {
			sourceLine = contract.SourceLine
		}
		successStatus := route.SuccessStatus
		if successStatus == 0 {
			successStatus = contract.SuccessStatus
		}
		routes = append(routes, ActionDTO{
			Name:             route.Pipeline,
			Kind:             "nflow",
			Description:      description,
			Method:           route.Method,
			Path:             route.Path,
			SourceFile:       sourceFile,
			SourceLine:       sourceLine,
			SuccessStatus:    successStatus,
			FlowSchemaDriven: contract.FlowSchemaDriven,
			ReqSchemaName:    contract.ReqSchemaName,
			ResSchemaName:    contract.ResSchemaName,
			ReqType:          reqType,
			ResType:          resType,
			Tags:             []string{"nflow", "route"},
			Category:         "NFlow Route",
			ReqFields:        studioFields(contract.ReqFields),
			ResFields:        studioFields(contract.ResFields),
		})
	}
	return routes
}

func studioFields(fields []analyzer.FieldContract) []FieldMeta {
	out := make([]FieldMeta, 0, len(fields))
	for _, field := range fields {
		key := field.Key
		if key == "" {
			key = field.Name
		}
		if key == "" {
			continue
		}
		kind := studioFieldType(field.Type)
		var defaultValue any = ""
		switch kind {
		case "number":
			defaultValue = 0
		case "boolean":
			defaultValue = false
		case "array":
			defaultValue = []any{}
		case "object":
			defaultValue = map[string]any{}
		}
		out = append(out, FieldMeta{
			Key:        key,
			Type:       kind,
			Required:   field.Required,
			Label:      humanize(key),
			Validation: field.Validation,
			Default:    defaultValue,
		})
	}
	return out
}

func studioFieldType(value string) string {
	t := strings.ToLower(strings.TrimSpace(value))
	t = strings.TrimPrefix(t, "*")
	switch {
	case t == "bool" || t == "boolean":
		return "boolean"
	case strings.HasPrefix(t, "[]") || t == "array":
		return "array"
	case strings.HasPrefix(t, "map[") || t == "object" || t == "any":
		return "object"
	case strings.Contains(t, "int") || strings.Contains(t, "uint") || strings.Contains(t, "float") || strings.Contains(t, "number") || strings.Contains(t, "decimal"):
		return "number"
	case strings.Contains(t, "time") || t == "date" || t == "datetime":
		return "date"
	default:
		return "string"
	}
}

func extractFields(t reflect.Type) []FieldMeta {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	var out []FieldMeta
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		jsonKey := strings.Split(f.Tag.Get("json"), ",")[0]
		if jsonKey == "-" {
			continue
		}
		if jsonKey == "" {
			jsonKey = f.Name
		}

		valTag := f.Tag.Get("validate")
		required := strings.Contains(valTag, "required")

		fieldType := f.Type
		for fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		ftype := "string"
		var def any = ""
		switch fieldType.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			ftype, def = "number", 0
		case reflect.Float32, reflect.Float64:
			ftype, def = "number", 0.0
		case reflect.Bool:
			ftype, def = "boolean", false
		case reflect.Slice:
			ftype, def = "array", []any{}
		case reflect.Array:
			ftype, def = "array", []any{}
		case reflect.Map:
			ftype, def = "object", map[string]any{}
		case reflect.Interface:
			ftype, def = "object", map[string]any{}
		}
		if fieldType == reflect.TypeOf(time.Time{}) {
			ftype, def = "date", time.Now().UTC().Format(time.RFC3339)
		}

		out = append(out, FieldMeta{
			Key:        jsonKey,
			Type:       ftype,
			Required:   required,
			Label:      humanize(jsonKey),
			Validation: valTag,
			Default:    def,
		})
	}
	return out
}

func typeName(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Name() != "" {
		return t.Name()
	}
	return "Anonymous"
}

func humanize(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	if len(s) > 0 {
		return strings.ToUpper(s[:1]) + s[1:]
	}
	return s
}
