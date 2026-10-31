package studio

import (
	"reflect"
	"strings"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)

type FieldMeta struct {
	Key        string `json:"key"`
	Type       string `json:"type"`
	Required   bool   `json:"required"`
	Label      string `json:"label"`
	Validation string `json:"validation,omitempty"`
	Default    any    `json:"default,omitempty"`
}

type ActionDTO struct {
	Name             string      `json:"name"`
	Description      string      `json:"description"`
	Method           string      `json:"method"`
	Path             string      `json:"path"`
	ReqType          string      `json:"req_type"`
	ResType          string      `json:"res_type"`
	Tags             []string    `json:"tags"`
	Node             string      `json:"node,omitempty"`
	IsSystem         bool        `json:"is_system"`
	RequiresAuth     bool        `json:"requires_auth"`
	RequiredRoles    []string    `json:"required_roles,omitempty"`
	RequiredPerms    []string    `json:"required_permissions,omitempty"`
	RequiredFeatures []string    `json:"required_features,omitempty"`
	Idempotent       bool        `json:"idempotent"`
	TimeoutMs        int64       `json:"timeout_ms,omitempty"`
	RetryMax         int         `json:"retry_max,omitempty"`
	CacheTTLMs       int64       `json:"cache_ttl_ms,omitempty"`
	ConcurrencyLimit int32       `json:"concurrency_limit,omitempty"`
	ReqFields        []FieldMeta `json:"req_fields"`
	ResFields        []FieldMeta `json:"res_fields"`
}

type ExecutionRecord struct {
	ID                string    `json:"id"`
	ActionName        string    `json:"action_name"`
	Timestamp         time.Time `json:"timestamp"`
	Status            int       `json:"status"`
	Success           bool      `json:"success"`
	DurationMs        int64     `json:"duration_ms"`
	DurationFormatted string    `json:"duration_formatted"`
	Payload           any       `json:"payload,omitempty"`
	Response          any       `json:"response,omitempty"`
	Error             string    `json:"error,omitempty"`
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
		if meta == nil || meta.Name == "" || seen[meta.Name] {
			continue
		}
		if strings.HasPrefix(meta.Name, "studio.") {
			continue
		}
		seen[meta.Name] = true

		dto := ActionDTO{
			Name:             meta.Name,
			Description:      meta.Description,
			Method:           "POST",
			Path:             "/" + meta.Name,
			Tags:             meta.Tags,
			Node:             meta.Node,
			IsSystem:         meta.IsSystem(),
			RequiresAuth:     meta.RequiresAuth,
			RequiredRoles:    meta.RequiredRoles,
			RequiredPerms:    meta.RequiredPermissions,
			RequiredFeatures: meta.RequiredFeatures,
			Idempotent:       meta.Idempotency.Enabled,
			TimeoutMs:        meta.Timeout.Milliseconds(),
			RetryMax:         meta.RetryMax,
			CacheTTLMs:       meta.CacheTTL.Milliseconds(),
			ConcurrencyLimit: meta.ConcurrencyLimit,
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

		ftype := "string"
		var def any = ""
		switch f.Type.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			ftype, def = "number", 0
		case reflect.Float32, reflect.Float64:
			ftype, def = "number", 0.0
		case reflect.Bool:
			ftype, def = "boolean", false
		case reflect.Slice:
			ftype, def = "array", []any{}
		}
		if f.Type == reflect.TypeOf(time.Time{}) {
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
