package analyzer

import (
	"time"

	"github.com/nexssp/ops/analyzer/dsl"
)

type (
	TransportBinding = dsl.TransportBinding
	DSLConfig        = dsl.Config
)

type StructKey struct {
	Domain  string
	Package string
	Name    string
}

type TypeRef struct {
	ImportPath string `json:"import_path,omitempty"`
	Domain     string `json:"domain,omitempty"`
	Package    string `json:"package,omitempty"`
	Name       string `json:"name"`
}

func (t TypeRef) String() string {
	if t.Domain != "" && t.Domain != t.Package {
		return t.Domain + "." + t.Name
	}
	if t.Package != "" {
		return t.Package + "." + t.Name
	}
	return t.Name
}

type ActionContract struct {
	Name             string             `json:"name"`
	Description      string             `json:"description"`
	ReqTypeName      string             `json:"req_type"`
	ResTypeName      string             `json:"res_type"`
	ReqType          TypeRef            `json:"req_type_ref,omitempty"`
	ResType          TypeRef            `json:"res_type_ref,omitempty"`
	Method           string             `json:"method"`
	Path             string             `json:"path"`
	RouteSource      string             `json:"route_source,omitempty"`
	TypeSource       string             `json:"type_source,omitempty"`
	Transports       []TransportBinding `json:"transports,omitempty"`
	Tags             []string           `json:"tags"`
	Scope            string             `json:"scope"`
	Idempotent       bool               `json:"idempotent"`
	Timeout          time.Duration      `json:"timeout"`
	RetryMax         int                `json:"retry_max"`
	CacheTTL         time.Duration      `json:"cache_ttl,omitempty"`
	RateLimit        string             `json:"rate_limit,omitempty"`
	ConcurrencyLimit int32              `json:"concurrency_limit,omitempty"`
	Permissions      []string           `json:"permissions"`
	Roles            []string           `json:"roles"`
	Features         []string           `json:"features"`
	ReqFields        []FieldContract    `json:"req_fields"`
	ResFields        []FieldContract    `json:"res_fields"`
	SourceFile       string             `json:"source_file"`
	SourceLine       int                `json:"source_line"`
	Streaming        bool               `json:"streaming,omitempty"`
	Raw              bool               `json:"raw,omitempty"`

	// Multipart is true when at least one request field is declared with a
	// `form:"..."` tag on a []byte — the client method is emitted as
	// multipart/form-data instead of application/json.
	Multipart bool `json:"multipart,omitempty"`

	// BlobResponse is true when the action's declared response type is
	// `[]byte`. The generated client returns a Blob.
	BlobResponse bool `json:"blob_response,omitempty"`

	BudgetMicros    int64         `json:"budget_micros,omitempty"`
	RequiresAuth    bool          `json:"requires_auth,omitempty"`
	RateLimitBurst  int           `json:"rate_limit_burst,omitempty"`
	BreakerFailures int           `json:"breaker_failures,omitempty"`
	BreakerCooldown time.Duration `json:"breaker_cooldown,omitempty"`
	HITLPrompt      string        `json:"hitl_prompt,omitempty"`
	HITLOptions     []string      `json:"hitl_options,omitempty"`
	HITLTriggers    []string      `json:"hitl_triggers,omitempty"`
	SuccessStatus   int           `json:"success_status,omitempty"`

	BackoffStrategy   string        `json:"backoff_strategy,omitempty"`
	BackoffBase       time.Duration `json:"backoff_base,omitempty"`
	BackoffMax        time.Duration `json:"backoff_max,omitempty"`
	RetryPredicate    string        `json:"retry_predicate,omitempty"`
	Priority          string        `json:"priority,omitempty"`
	CacheKey          string        `json:"cache_key,omitempty"`
	Audit             bool          `json:"audit,omitempty"`
	IdempotencyHeader string        `json:"idempotency_header,omitempty"`
	Deprecated        bool          `json:"deprecated,omitempty"`
	DeprecatedSince   string        `json:"deprecated_since,omitempty"`
	DeprecatedUse     string        `json:"deprecated_use,omitempty"`
}

type FieldContract struct {
	Name       string `json:"name"`
	Key        string `json:"key"`
	Type       string `json:"type"`
	Required   bool   `json:"required"`
	Validation string `json:"validation,omitempty"`
	RawTags    string `json:"raw_tags,omitempty"`

	// Multipart marks a field that must be transported as a file part in
	// a multipart/form-data request. Set by the analyzer when a []byte field
	// carries a `form:"..."` tag.
	Multipart bool `json:"multipart,omitempty"`
}

// FlowHTTPRoute describes a route declared by an .nflow @pipeline modifier.
// Pipeline is the compiled Flow resolver name (for example, pipeline.agents_list).
type FlowHTTPRoute struct {
	Pipeline      string `json:"pipeline"`
	Method        string `json:"method"`
	Path          string `json:"path"`
	SuccessStatus int    `json:"success_status,omitempty"`
	SourceFile    string `json:"source_file,omitempty"`
	SourceLine    int    `json:"source_line,omitempty"`
}

type StructDefinition struct {
	Name       string          `json:"name"`
	Package    string          `json:"package"`
	Domain     string          `json:"domain"`
	ImportPath string          `json:"import_path,omitempty"`
	Qualified  string          `json:"qualified"`
	SourceFile string          `json:"source_file"`
	Fields     []FieldContract `json:"fields"`
	IsExposed  bool            `json:"is_exposed"`
	IsIgnored  bool            `json:"is_ignored"`
	CustomName string          `json:"custom_name,omitempty"`
	TargetFile string          `json:"target_file,omitempty"`
}

type DiagnosticSeverity string

const (
	SeverityError   DiagnosticSeverity = "ERROR"
	SeverityWarning DiagnosticSeverity = "WARNING"
)

type Diagnostic struct {
	Severity   DiagnosticSeverity `json:"severity"`
	Rule       string             `json:"rule"`
	Message    string             `json:"message"`
	Suggestion string             `json:"suggestion,omitempty"`
	File       string             `json:"file,omitempty"`
	Line       int                `json:"line,omitempty"`
	Action     string             `json:"action,omitempty"`
}

type Report struct {
	ScannedFiles   int                         `json:"scanned_files"`
	Actions        []ActionContract            `json:"actions"`
	FlowHTTPRoutes []FlowHTTPRoute             `json:"flow_http_routes,omitempty"`
	Structs        map[string]StructDefinition `json:"structs"`
	Enums          map[string]EnumDefinition   `json:"enums,omitempty"`
	Diagnostics    []Diagnostic                `json:"diagnostics"`
	Stats          ReportStats                 `json:"stats"`
}

func (r *Report) FindStruct(nameOrQualified string) (StructDefinition, bool) {
	if s, ok := r.Structs[nameOrQualified]; ok {
		return s, true
	}
	for _, s := range r.Structs {
		if s.Qualified == nameOrQualified || s.Name == nameOrQualified {
			return s, true
		}
	}
	return StructDefinition{}, false
}

type ReportStats struct {
	TotalActions    int `json:"total_actions"`
	PublicActions   int `json:"public_actions"`
	InternalActions int `json:"internal_actions"`
	SystemActions   int `json:"system_actions"`
	TotalStructs    int `json:"total_structs"`
	TotalEnums      int `json:"total_enums"`
	ErrorCount      int `json:"error_count"`
	WarningCount    int `json:"warning_count"`
}
