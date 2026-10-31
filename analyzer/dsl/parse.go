// Package dsl contains OPS's compatibility parser for older action-oriented
// route manifests. New .nflow pipeline declarations are parsed through Flow's
// exported compiler preprocessing API instead.
package dsl

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

type TransportBinding struct {
	Kind       string            `json:"kind"`
	Protocol   string            `json:"protocol,omitempty"`
	Target     string            `json:"target"`
	Method     string            `json:"method,omitempty"`
	Path       string            `json:"path,omitempty"`
	Subject    string            `json:"subject,omitempty"`
	Schedule   string            `json:"schedule,omitempty"`
	QueueGroup string            `json:"queue_group,omitempty"`
	Interval   time.Duration     `json:"interval,omitempty"`
	Stream     bool              `json:"stream,omitempty"`
	Raw        bool              `json:"raw,omitempty"`
	Meta       map[string]string `json:"meta,omitempty"`
	Aliases    []string          `json:"aliases,omitempty"`
}

type Config struct {
	Strict bool
	Silent map[string]bool
}

type ParsedLine struct {
	RawName     string
	HeadAtom    string
	IsComposite bool
	Modifiers   Modifiers
}

type Modifiers struct {
	CustomName        string
	Path              string
	Method            string
	Transports        []TransportBinding
	Description       string
	Scope             string
	ReqType           string
	ResType           string
	Timeout           time.Duration
	CacheTTL          time.Duration
	RetryMax          int
	Idempotent        bool
	Roles             []string
	Permissions       []string
	Features          []string
	Tags              []string
	RequiresAuth      bool
	SuccessStatus     int
	BudgetMicros      int64
	RateLimit         string
	RateLimitBurst    int
	ConcurrencyLimit  int32
	BreakerFailures   int
	BreakerCooldown   time.Duration
	HITLPrompt        string
	HITLOptions       []string
	HITLTriggers      []string
	BackoffStrategy   string
	BackoffBase       time.Duration
	BackoffMax        time.Duration
	RetryPredicate    string
	Priority          string
	CacheKey          string
	Audit             bool
	IdempotencyHeader string
	Deprecated        bool
	DeprecatedSince   string
	DeprecatedUse     string
}

var actionTokenRE = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.-]*`)

// ParseLine parses the legacy `action.name:route="POST /path":flag` form.
// It intentionally does not parse modern @pipeline blocks; those are handled
// by Flow's preprocessor in the parent analyzer package.
func ParseLine(input string) (ParsedLine, bool) {
	line := strings.TrimSpace(input)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "@") {
		return ParsedLine{}, false
	}
	parts := splitOutside(line, ':')
	if len(parts) < 2 {
		return ParsedLine{}, false
	}

	head := strings.TrimSpace(parts[0])
	parsed := ParsedLine{}
	if left, right, ok := splitArrowOutside(head); ok {
		parsed.IsComposite = true
		parsed.HeadAtom = lastActionToken(left)
		parsed.RawName = lastActionToken(right)
	} else {
		parsed.RawName = firstActionToken(head)
	}
	if parsed.RawName == "" {
		return ParsedLine{}, false
	}

	for _, segment := range parts[1:] {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		name, value, hasValue := strings.Cut(segment, "=")
		name = strings.ToLower(strings.TrimSpace(name))
		value = unquote(strings.TrimSpace(value))
		applyModifier(&parsed.Modifiers, name, value, hasValue)
	}
	if !hasTarget(parsed.Modifiers) {
		return ParsedLine{}, false
	}
	return parsed, true
}

func ApplyConfigLine(cfg *Config, line string) {
	if cfg == nil {
		return
	}
	if cfg.Silent == nil {
		cfg.Silent = make(map[string]bool)
	}
	body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "@config:"))
	key, value, hasValue := strings.Cut(body, "=")
	key = strings.ToLower(strings.TrimSpace(key))
	value = unquote(strings.TrimSpace(value))
	switch key {
	case "strict":
		cfg.Strict = !hasValue || truthy(value)
	case "silent":
		for _, rule := range splitOutside(value, ',') {
			if rule = strings.TrimSpace(rule); rule != "" {
				cfg.Silent[rule] = true
			}
		}
	}
}

func applyModifier(m *Modifiers, name, value string, hasValue bool) {
	if m == nil {
		return
	}
	switch name {
	case "name":
		m.CustomName = value
	case "route", "http":
		if method, path := parseHTTP(value); method != "" && path != "" {
			m.Method, m.Path = method, path
			m.Transports = append(m.Transports, TransportBinding{Kind: "http", Protocol: "flow", Method: method, Path: path, Target: method + " " + path})
		}
	case "sse":
		path := value
		if method, routePath := parseHTTP(value); method != "" {
			path = routePath
		}
		if path != "" {
			m.Transports = append(m.Transports, TransportBinding{Kind: "sse", Protocol: "flow", Method: "SSE", Path: path, Target: "SSE " + path, Stream: true})
		}
	case "raw":
		if method, path := parseHTTP(value); method != "" && path != "" {
			m.Transports = append(m.Transports, TransportBinding{Kind: "raw", Protocol: "flow", Method: method, Path: path, Target: method + " " + path, Raw: true})
		}
	case "channel":
		for i := range m.Transports {
			if m.Transports[i].Kind == "sse" {
				setMeta(&m.Transports[i], "channel", value)
			}
		}
	case "cli":
		if value != "" {
			m.Transports = append(m.Transports, TransportBinding{Kind: "cli", Protocol: "flow", Target: value})
		}
	case "cli_alias":
		for i := range m.Transports {
			if m.Transports[i].Kind == "cli" {
				m.Transports[i].Aliases = splitList(value)
			}
		}
	case "cli_desc":
		for i := range m.Transports {
			if m.Transports[i].Kind == "cli" {
				setMeta(&m.Transports[i], "description", value)
			}
		}
	case "cron":
		if value != "" {
			m.Transports = append(m.Transports, cronBinding("cron", value))
		}
	case "worker":
		if value != "" {
			m.Transports = append(m.Transports, cronBinding("worker", value))
		}
	case "nats":
		if value != "" {
			m.Transports = append(m.Transports, TransportBinding{Kind: "nats-pubsub", Protocol: "tnats", Target: value, Subject: value})
		}
	case "topic":
		if value != "" {
			m.Transports = append(m.Transports, TransportBinding{Kind: "topic", Protocol: "tbus", Target: value, Subject: value})
		}
	case "nats_kv":
		parts := strings.SplitN(value, "/", 2)
		if len(parts) == 2 {
			m.Transports = append(m.Transports, TransportBinding{Kind: "nats-kv", Protocol: "tnats", Target: value, Subject: strings.ReplaceAll(value, "/", "."), Meta: map[string]string{"bucket": parts[0], "key": parts[1]}})
		}
	case "nats_durable":
		parts := strings.Split(value, "/")
		if len(parts) >= 3 {
			meta := map[string]string{"stream": parts[0], "durable": parts[2]}
			if len(parts) > 3 && parts[3] != "" {
				meta["dlq"] = parts[3]
			}
			m.Transports = append(m.Transports, TransportBinding{Kind: "nats-durable", Protocol: "tnats", Target: value, Subject: parts[1], Meta: meta})
		}
	case "nats_svc":
		parts := strings.Split(value, "/")
		if len(parts) >= 4 {
			m.Transports = append(m.Transports, TransportBinding{Kind: "nats-service", Protocol: "tnats", Target: parts[0] + "/" + parts[2], Subject: parts[3], Meta: map[string]string{"service": parts[0], "version": parts[1], "endpoint": parts[2]}})
		}
	case "nats_rpc":
		if value != "" {
			m.Transports = append(m.Transports, TransportBinding{Kind: "nats-rpc", Protocol: "tnats", Target: value, Subject: value})
		}
	case "a2a":
		if value != "" {
			m.Transports = append(m.Transports, TransportBinding{Kind: "a2a", Protocol: "ta2a", Target: value, Subject: value})
		}
	case "a2a_desc":
		setTransportMeta(m, "a2a", "description", value)
	case "a2a_example":
		setTransportMeta(m, "a2a", "example", value)
	case "type":
		if req, res, ok := strings.Cut(value, "->"); ok {
			m.ReqType, m.ResType = strings.TrimSpace(req), strings.TrimSpace(res)
		} else if value != "" {
			m.ReqType = value
		}
	case "desc", "description":
		m.Description = value
	case "scope":
		m.Scope = value
	case "tag":
		m.Tags = append(m.Tags, splitList(value)...)
	case "role":
		m.Roles = append(m.Roles, splitList(value)...)
	case "perm", "permission":
		m.Permissions = append(m.Permissions, splitList(value)...)
	case "feature":
		m.Features = append(m.Features, splitList(value)...)
	case "auth":
		m.RequiresAuth = !hasValue || truthy(value) || strings.EqualFold(value, "required")
	case "status":
		m.SuccessStatus = integer(value)
	case "timeout":
		m.Timeout = duration(value)
	case "cache":
		m.CacheTTL = duration(value)
	case "retry":
		m.RetryMax = integer(value)
	case "idempotent":
		m.Idempotent = !hasValue || truthy(value)
	case "budget":
		m.BudgetMicros = budgetMicros(value)
	case "budget_micros":
		m.BudgetMicros = int64(integer(value))
	case "rate_limit":
		m.RateLimit = value
	case "burst":
		m.RateLimitBurst = integer(value)
	case "concurrency":
		m.ConcurrencyLimit = int32(integer(value))
	case "breaker":
		for _, item := range splitOutside(value, ',') {
			key, val, ok := strings.Cut(strings.TrimSpace(item), "=")
			if !ok {
				continue
			}
			switch strings.TrimSpace(key) {
			case "failures":
				m.BreakerFailures = integer(strings.TrimSpace(val))
			case "cooldown":
				m.BreakerCooldown = duration(strings.TrimSpace(val))
			}
		}
	case "hitl":
		m.HITLPrompt = value
	case "hitl_options":
		m.HITLOptions = splitList(value)
	case "hitl_trigger", "hitl_triggers":
		m.HITLTriggers = splitList(value)
	case "backoff":
		for i, item := range splitOutside(value, ',') {
			item = strings.TrimSpace(item)
			if i == 0 && !strings.Contains(item, "=") {
				m.BackoffStrategy = item
				continue
			}
			key, val, ok := strings.Cut(item, "=")
			if !ok {
				continue
			}
			switch strings.TrimSpace(key) {
			case "base":
				m.BackoffBase = duration(strings.TrimSpace(val))
			case "max":
				m.BackoffMax = duration(strings.TrimSpace(val))
			}
		}
	case "backoff_base":
		m.BackoffBase = duration(value)
	case "backoff_max":
		m.BackoffMax = duration(value)
	case "retry_if", "retry_predicate":
		m.RetryPredicate = value
	case "priority":
		m.Priority = value
	case "cache_key":
		m.CacheKey = value
	case "audit":
		m.Audit = !hasValue || truthy(value)
	case "idempotency_header":
		m.IdempotencyHeader = value
	case "deprecated":
		m.Deprecated = !hasValue || truthy(value)
	case "since", "deprecated_since":
		m.DeprecatedSince = value
	case "use", "deprecated_use":
		m.DeprecatedUse = value
	}
}

func hasTarget(m Modifiers) bool { return m.Path != "" || len(m.Transports) != 0 }

func parseHTTP(value string) (string, string) {
	fields := strings.Fields(value)
	if len(fields) < 2 {
		return "", ""
	}
	method := strings.ToUpper(strings.TrimSpace(fields[0]))
	path := strings.TrimSpace(strings.Join(fields[1:], " "))
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return method, path
	}
	return "", ""
}

func cronBinding(kind, value string) TransportBinding {
	binding := TransportBinding{Kind: kind, Protocol: kind, Target: value}
	if strings.HasPrefix(value, "every ") {
		if d, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(value, "every "))); err == nil {
			binding.Interval = d
			binding.Schedule = value
		}
	} else {
		binding.Schedule = value
	}
	return binding
}

func splitOutside(s string, sep rune) []string {
	var out []string
	start, depth := 0, 0
	var quote rune
	escaped := false
	for i, r := range s {
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '"', '\'':
			quote = r
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			if depth > 0 {
				depth--
			}
		default:
			if r == sep && depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

func splitArrowOutside(s string) (string, string, bool) {
	parts := splitOutside(s, '|')
	if len(parts) > 1 {
		return "", "", false
	}
	var quote rune
	escaped, depth := false, 0
	for i := 0; i+1 < len(s); i++ {
		r := rune(s[i])
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '"', '\'':
			quote = r
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			if depth > 0 {
				depth--
			}
		}
		if depth == 0 && s[i:i+2] == "->" {
			return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+2:]), true
		}
	}
	return "", "", false
}

func firstActionToken(s string) string {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	return strings.Trim(s, "(){}[]")
}

func lastActionToken(s string) string {
	matches := actionTokenRE.FindAllString(s, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		if strings.Contains(matches[i], ".") {
			return strings.Trim(matches[i], ".")
		}
	}
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

func unquote(value string) string {
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1]
	}
	if decoded, err := strconv.Unquote(value); err == nil {
		return decoded
	}
	return value
}

func splitList(value string) []string {
	var out []string
	for _, item := range splitOutside(value, ',') {
		if item = strings.TrimSpace(unquote(strings.TrimSpace(item))); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func duration(value string) time.Duration {
	d, _ := time.ParseDuration(strings.TrimSpace(value))
	return d
}
func integer(value string) int { n, _ := strconv.Atoi(strings.TrimSpace(value)); return n }
func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "true", "yes", "1", "required":
		return true
	case "false", "no", "0":
		return false
	}
	return true
}

func budgetMicros(value string) int64 {
	v := strings.TrimSpace(strings.TrimPrefix(value, "$"))
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	if strings.HasPrefix(strings.TrimSpace(value), "$") || strings.Contains(v, ".") && f < 1 {
		f *= 1_000_000
	}
	return int64(f)
}

func setMeta(binding *TransportBinding, key, value string) {
	if binding.Meta == nil {
		binding.Meta = make(map[string]string)
	}
	binding.Meta[key] = value
}

func setTransportMeta(m *Modifiers, kind, key, value string) {
	for i := range m.Transports {
		if m.Transports[i].Kind == kind {
			setMeta(&m.Transports[i], key, value)
		}
	}
}
