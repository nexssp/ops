package core

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"time"
)

// fluentStep is one node in a fluent call chain such as
// `thttp.Stream("/api/events").WithChannel("events").WithBroadcaster(b)`.
//
// Steps are ordered outermost-first. The last step is always the base
// constructor (the one whose receiver is a package ident, not another call).
type fluentStep struct {
	pkg    string
	method string
	args   []ast.Expr
}

func walkFluent(e ast.Expr) []fluentStep {
	var steps []fluentStep
	for {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			break
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			break
		}
		pkg := ""
		if id, ok := sel.X.(*ast.Ident); ok {
			pkg = id.Name
		}
		steps = append(steps, fluentStep{pkg: pkg, method: sel.Sel.Name, args: call.Args})
		e = sel.X
	}
	return steps
}

// detectBindings inspects a single `.Route(...)` argument and returns all
// transport bindings it declares. Returns nil when the argument isn't a
// recognised binding constructor.
func detectBindings(e ast.Expr) []TransportBinding {
	steps := walkFluent(e)
	if len(steps) == 0 {
		return nil
	}
	base := steps[len(steps)-1]
	modifiers := steps[:len(steps)-1]

	switch base.pkg {
	case "thttp":
		return detectTHTTP(base, modifiers)
	case "tcli":
		return detectTCLI(base, modifiers)
	case "tbus":
		return detectTBus(base)
	case "cron":
		return detectCron(base)
	case "tworker":
		return detectTWorker(base)
	case "tnats":
		return detectTNats(base, modifiers)
	case "ta2a":
		return detectTA2A(base, modifiers)
	}
	return nil
}

func detectTHTTP(base fluentStep, modifiers []fluentStep) []TransportBinding {
	switch base.method {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD":
		path := argString(base.args, 0)
		if path == "" {
			return nil
		}
		return []TransportBinding{{
			Kind: "http", Protocol: "thttp",
			Target: base.method + " " + path,
			Method: base.method, Path: path,
		}}

	case "Stream":
		path := argString(base.args, 0)
		if path == "" {
			return nil
		}
		b := TransportBinding{
			Kind: "sse", Protocol: "thttp",
			Target: "SSE " + path,
			Method: "SSE", Path: path, Stream: true,
		}
		for _, mod := range modifiers {
			if mod.method == "WithChannel" {
				if ch := argString(mod.args, 0); ch != "" {
					if b.Meta == nil {
						b.Meta = map[string]string{}
					}
					b.Meta["channel"] = ch
				}
			}
		}
		return []TransportBinding{b}

	case "RawHandler":
		method := normaliseHTTPMethod(argString(base.args, 0))
		path := argString(base.args, 1)
		if method == "" || path == "" {
			return nil
		}
		return []TransportBinding{{
			Kind: "raw", Protocol: "thttp",
			Target: method + " " + path,
			Method: method, Path: path, Raw: true,
		}}
	}
	return nil
}

func detectTCLI(base fluentStep, modifiers []fluentStep) []TransportBinding {
	if base.method != "Command" {
		return nil
	}
	cmd := argString(base.args, 0)
	if cmd == "" {
		return nil
	}
	b := TransportBinding{
		Kind: "cli", Protocol: "tcli",
		Target: cmd,
	}
	if desc := argString(base.args, 1); desc != "" {
		b.Meta = map[string]string{"description": desc}
	}
	for _, mod := range modifiers {
		if mod.method == "WithAliases" {
			b.Aliases = append(b.Aliases, argStrings(mod.args, 0)...)
		}
	}
	return []TransportBinding{b}
}

func detectTBus(base fluentStep) []TransportBinding {
	if base.method != "Topic" && base.method != "Listen" {
		return nil
	}
	topic := argString(base.args, 0)
	if topic == "" {
		return nil
	}
	return []TransportBinding{{
		Kind: "topic", Protocol: "tbus",
		Target: topic, Subject: topic,
	}}
}

func detectCron(base fluentStep) []TransportBinding {
	switch base.method {
	case "Every":
		d, ok := parseDurationExprArg(base.args, 0)
		if !ok {
			return nil
		}
		return []TransportBinding{{
			Kind: "cron", Protocol: "cron",
			Target:   "every " + d.String(),
			Interval: d,
		}}
	case "Cron":
		expr := argString(base.args, 0)
		if expr == "" {
			return nil
		}
		return []TransportBinding{{
			Kind: "cron", Protocol: "cron",
			Target:   "cron " + expr,
			Schedule: expr,
		}}
	}
	return nil
}

func detectTWorker(base fluentStep) []TransportBinding {
	if base.method != "Every" {
		return nil
	}
	d, ok := parseDurationExprArg(base.args, 0)
	if !ok {
		return nil
	}
	return []TransportBinding{{
		Kind: "worker", Protocol: "tworker",
		Target:   "every " + d.String(),
		Interval: d,
	}}
}

func detectTNats(base fluentStep, modifiers []fluentStep) []TransportBinding {
	switch base.method {
	case "Topic", "Listen":
		subject := argString(base.args, 0)
		if subject == "" {
			return nil
		}
		b := TransportBinding{
			Kind: "nats-pubsub", Protocol: "tnats",
			Target: subject, Subject: subject,
		}
		if qg := argString(base.args, 1); qg != "" {
			b.QueueGroup = qg
		}
		return []TransportBinding{b}

	case "Request":
		subject := argString(base.args, 0)
		if subject == "" {
			return nil
		}
		b := TransportBinding{
			Kind: "nats-rpc", Protocol: "tnats",
			Target: subject, Subject: subject,
		}
		if len(base.args) > 1 {
			if d, ok := parseDurationExpr(base.args[1]); ok {
				if b.Meta == nil {
					b.Meta = map[string]string{}
				}
				b.Meta["timeout"] = d.String()
			}
		}
		return []TransportBinding{b}

	case "KV", "WatchKV":
		bucket := argString(base.args, 0)
		key := argString(base.args, 1)
		if bucket == "" || key == "" {
			return nil
		}
		return []TransportBinding{{
			Kind: "nats-kv", Protocol: "tnats",
			Target:  bucket + "/" + key,
			Subject: bucket + "." + key,
			Meta:    map[string]string{"bucket": bucket, "key": key},
		}}

	case "DurableWork":
		stream := argString(base.args, 0)
		subject := argString(base.args, 1)
		durable := argString(base.args, 2)
		dlq := argString(base.args, 3)
		if stream == "" || subject == "" || durable == "" {
			return nil
		}
		return []TransportBinding{{
			Kind: "nats-durable", Protocol: "tnats",
			Target:  stream + "/" + subject + "/" + durable,
			Subject: subject,
			Meta:    map[string]string{"stream": stream, "durable": durable, "dlq": dlq},
		}}

	case "Consumer":
		stream := argString(base.args, 0)
		subject := argString(base.args, 1)
		durable := argString(base.args, 2)
		if stream == "" || subject == "" || durable == "" {
			return nil
		}
		return []TransportBinding{{
			Kind: "nats-consumer", Protocol: "tnats",
			Target:  stream + "/" + subject + "/" + durable,
			Subject: subject,
			Meta:    map[string]string{"stream": stream, "durable": durable},
		}}

	case "ObjectStore":
		bucket := argString(base.args, 0)
		pattern := argString(base.args, 1)
		if bucket == "" {
			return nil
		}
		return []TransportBinding{{
			Kind: "nats-objectstore", Protocol: "tnats",
			Target:  bucket + "/" + pattern,
			Subject: bucket + "." + pattern,
			Meta:    map[string]string{"bucket": bucket, "pattern": pattern},
		}}

	case "Service":
		service := argString(base.args, 0)
		version := argString(base.args, 1)
		endpoint := argString(base.args, 2)
		subject := argString(base.args, 3)
		if service == "" || subject == "" {
			return nil
		}
		return []TransportBinding{{
			Kind: "nats-service", Protocol: "tnats",
			Target:  service + "/" + endpoint,
			Subject: subject,
			Meta:    map[string]string{"service": service, "version": version, "endpoint": endpoint},
		}}
	}
	return nil
}

func detectTA2A(base fluentStep, modifiers []fluentStep) []TransportBinding {
	if base.method != "Role" {
		return nil
	}
	role := argString(base.args, 0)
	if role == "" {
		return nil
	}
	b := TransportBinding{
		Kind: "a2a", Protocol: "ta2a",
		Target: role, Subject: role,
	}
	for _, mod := range modifiers {
		if mod.method == "WithDescription" {
			if desc := argString(mod.args, 0); desc != "" {
				if b.Meta == nil {
					b.Meta = map[string]string{}
				}
				b.Meta["description"] = desc
			}
		}
	}
	return []TransportBinding{b}
}

// ── argument extraction helpers ─────────────────────────────────────────────

func argString(args []ast.Expr, i int) string {
	if i < 0 || i >= len(args) {
		return ""
	}
	return cleanLiteral(exprToString(args[i]))
}

func argStrings(args []ast.Expr, from int) []string {
	out := make([]string, 0, len(args))
	for i := from; i < len(args); i++ {
		s := cleanLiteral(exprToString(args[i]))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parseDurationExprArg(args []ast.Expr, i int) (time.Duration, bool) {
	if i < 0 || i >= len(args) {
		return 0, false
	}
	return parseDurationExpr(args[i])
}

func parseDurationExpr(e ast.Expr) (time.Duration, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind == token.INT {
			n, err := strconv.ParseInt(v.Value, 10, 64)
			if err == nil {
				return time.Duration(n), true
			}
		}
	case *ast.BinaryExpr:
		if v.Op == token.MUL {
			n, ok := intLiteral(v.X)
			if !ok {
				return 0, false
			}
			unit, ok := durationUnit(v.Y)
			if !ok {
				return 0, false
			}
			return time.Duration(n) * unit, true
		}
	case *ast.CallExpr:
		sel, ok := v.Fun.(*ast.SelectorExpr)
		if !ok || exprToString(sel.X) != "time" {
			return 0, false
		}
		switch sel.Sel.Name {
		case "Duration":
			if len(v.Args) > 0 {
				if n, ok := intLiteral(v.Args[0]); ok {
					return time.Duration(n), true
				}
			}
		case "ParseDuration":
			if len(v.Args) > 0 {
				if s := argString(v.Args, 0); s != "" {
					d, err := time.ParseDuration(s)
					return d, err == nil
				}
			}
		}
	}
	return 0, false
}

func intLiteral(e ast.Expr) (int64, bool) {
	if bl, ok := e.(*ast.BasicLit); ok && bl.Kind == token.INT {
		n, err := strconv.ParseInt(bl.Value, 10, 64)
		return n, err == nil
	}
	return 0, false
}

func durationUnit(e ast.Expr) (time.Duration, bool) {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || exprToString(sel.X) != "time" {
		return 0, false
	}
	switch sel.Sel.Name {
	case "Nanosecond":
		return time.Nanosecond, true
	case "Microsecond":
		return time.Microsecond, true
	case "Millisecond":
		return time.Millisecond, true
	case "Second":
		return time.Second, true
	case "Minute":
		return time.Minute, true
	case "Hour":
		return time.Hour, true
	}
	return 0, false
}

func normaliseHTTPMethod(m string) string {
	// `http.MethodGet` → "GET"; `Get` → "GET"; "GET" → "GET".
	m = strings.TrimPrefix(m, "http.Method")
	return strings.ToUpper(m)
}
