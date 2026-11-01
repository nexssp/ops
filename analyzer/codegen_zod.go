package analyzer

import (
	"fmt"
	"strconv"
	"strings"
)

type validatorRule struct {
	Name  string
	Value string
}

func parseValidatorRules(raw string) []validatorRule {
	if raw == "" {
		return nil
	}
	tokens := splitValidatorTokens(raw)
	rules := make([]validatorRule, 0, len(tokens))
	for _, tok := range tokens {
		if eq := strings.IndexByte(tok, '='); eq > 0 {
			rules = append(rules, validatorRule{Name: tok[:eq], Value: tok[eq+1:]})
		} else {
			rules = append(rules, validatorRule{Name: tok})
		}
	}
	return rules
}

func splitValidatorTokens(raw string) []string {
	var out []string
	var sb strings.Builder
	inSQ, inDQ := false, false
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		switch {
		case ch == '\'' && !inDQ:
			inSQ = !inSQ
			sb.WriteByte(ch)
		case ch == '"' && !inSQ:
			inDQ = !inDQ
			sb.WriteByte(ch)
		case ch == ',' && !inSQ && !inDQ:
			if sb.Len() > 0 {
				out = append(out, sb.String())
				sb.Reset()
			}
		default:
			sb.WriteByte(ch)
		}
	}
	if sb.Len() > 0 {
		out = append(out, sb.String())
	}
	return out
}

func splitSpaceValues(s string) []string {
	parts := strings.Fields(s)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.Trim(p, "'\""))
	}
	return out
}

func rulesToTag(rules []validatorRule) string {
	parts := make([]string, 0, len(rules))
	for _, r := range rules {
		if r.Value == "" {
			parts = append(parts, r.Name)
		} else {
			parts = append(parts, r.Name+"="+r.Value)
		}
	}
	return strings.Join(parts, ",")
}

func mapFieldToZod(f FieldContract, tsStructs, raw map[string]StructDefinition, colliding map[string]bool) (string, []string) {
	expr, unknown := buildFieldZod(f, tsStructs, raw, colliding)
	if f.Nullable {
		expr += ".nullable()"
	}
	if !f.Required {
		// Emit .optional() only, not .optional().nullable(). Go's
		// non-pointer types never emit null on the wire, so accepting null
		// in the schema creates a null/undefined mismatch between the
		// schema output and the TS interface, breaking createFormStore
		// type inference at every call site.
		expr += ".optional()"
	}
	return expr, unknown
}

func buildFieldZod(f FieldContract, tsStructs, raw map[string]StructDefinition, colliding map[string]bool) (string, []string) {
	if strings.HasSuffix(f.Type, "[]") {
		elemType := strings.TrimSuffix(f.Type, "[]")
		return buildArrayZod(elemType, f, tsStructs, raw, colliding)
	}
	return buildScalarZod(f, tsStructs, raw, colliding)
}

func scalarZodBase(tsType string, tsStructs, raw map[string]StructDefinition, colliding map[string]bool) string {
	switch tsType {
	case "string", "IsoDateString":
		return "z.string()"
	case "number":
		return "z.number()"
	case "boolean":
		return "z.boolean()"
	case "unknown", "any":
		return "z.unknown()"
	case "Record<string, unknown>":
		return "z.record(z.string(), z.unknown())"
	}
	if strings.HasPrefix(tsType, "Record<string, ") && strings.HasSuffix(tsType, ">") {
		valueType := strings.TrimSuffix(strings.TrimPrefix(tsType, "Record<string, "), ">")
		return fmt.Sprintf("z.record(z.string(), %s)", scalarZodBase(valueType, tsStructs, raw, colliding))
	}
	if sDef, ok := findStructByAny(tsType, tsStructs, raw); ok {
		return fmt.Sprintf("z.lazy(() => %sSchema)", targetTSName(sDef, colliding))
	}
	return "z.unknown()"
}

func buildScalarZod(f FieldContract, tsStructs, raw map[string]StructDefinition, colliding map[string]bool) (string, []string) {
	rules := parseValidatorRules(f.Validation)
	allowEmpty := false
	for _, rule := range rules {
		if rule.Name == "omitempty" {
			allowEmpty = true
			break
		}
	}

	if f.Type == "string" || f.Type == "IsoDateString" {
		for _, r := range rules {
			if r.Name == "oneof" {
				vals := splitSpaceValues(r.Value)
				quoted := make([]string, len(vals))
				for i, v := range vals {
					quoted[i] = fmt.Sprintf("%q", v)
				}
				if allowEmpty {
					return fmt.Sprintf(`z.string().refine(v => v === "" || [%s].includes(v))`, strings.Join(quoted, ", ")), nil
				}
				return fmt.Sprintf("z.enum([%s])", strings.Join(quoted, ", ")), nil
			}
			if r.Name == "oneofci" {
				vals := splitSpaceValues(r.Value)
				quoted := make([]string, len(vals))
				for i, v := range vals {
					quoted[i] = fmt.Sprintf("%q", v)
				}
				condition := fmt.Sprintf("[%s].some(choice => choice.toLowerCase() === v.toLowerCase())", strings.Join(quoted, ", "))
				if allowEmpty {
					condition = `v === "" || ` + condition
				}
				return fmt.Sprintf("z.string().refine(v => %s)", condition), nil
			}
		}
	}

	base := scalarZodBase(f.Type, tsStructs, raw, colliding)

	var unknown []string
	hasExplicitLen := false
	for _, r := range rules {
		switch r.Name {
		case "min", "max", "len", "eq", "gt", "gte", "lt", "lte":
			hasExplicitLen = true
		}
	}

	for _, r := range rules {
		switch r.Name {
		case "required":
			if (f.Type == "string" || f.Type == "IsoDateString") && !hasExplicitLen {
				base += ".min(1)"
			}
			continue
		case "omitempty":
			continue
		}

		if f.Type == "number" && allowEmpty {
			if applied, ok := applyOptionalZeroNumberRule(base, r); ok {
				base = applied
				continue
			}
		}
		applied, ok := applyScalarRule(base, r, f.Type)
		if !ok {
			unknown = append(unknown, r.Name)
			continue
		}
		base = applied
	}
	return base, unknown
}

func applyScalarRule(base string, r validatorRule, tsType string) (string, bool) {
	if tsType == "string" || tsType == "IsoDateString" {
		return applyStringRule(base, r)
	}
	if tsType == "number" {
		return applyNumberRule(base, r)
	}
	if tsType == "boolean" {
		switch r.Name {
		case "eq":
			if r.Value == "true" || r.Value == "false" {
				return base + fmt.Sprintf(".refine(v => v === %s)", r.Value), true
			}
		case "ne":
			if r.Value == "true" || r.Value == "false" {
				return base + fmt.Sprintf(".refine(v => v !== %s)", r.Value), true
			}
		}
	}
	return "", false
}

func applyStringRule(base string, r validatorRule) (string, bool) {
	switch r.Name {
	case "email":
		return base + ".email()", true
	case "url", "uri":
		return base + ".url()", true
	case "uuid", "uuid4":
		return base + ".uuid()", true
	case "ulid":
		return base + ".ulid()", true
	case "ip":
		return base + ".ip()", true
	case "ipv4":
		return base + `.ip({ version: "v4" })`, true
	case "ipv6":
		return base + `.ip({ version: "v6" })`, true
	case "cidr":
		return base + ".cidr()", true
	case "base64":
		return base + ".base64()", true
	case "base64url":
		return base + ".base64url()", true
	case "jwt":
		return base + `.regex(/^[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+$/)`, true
	case "alpha":
		return base + `.regex(/^[a-zA-Z]+$/)`, true
	case "alphanum":
		return base + `.regex(/^[a-zA-Z0-9]+$/)`, true
	case "numeric", "number":
		return base + `.regex(/^[0-9]+(\.[0-9]+)?$/)`, true
	case "hexadecimal":
		return base + `.regex(/^(0x)?[0-9a-fA-F]+$/)`, true
	case "hexcolor":
		return base + `.regex(/^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/)`, true
	case "rgb":
		return base + `.regex(/^rgb\(\s*\d+\s*,\s*\d+\s*,\s*\d+\s*\)$/)`, true
	case "rgba":
		return base + `.regex(/^rgba\(\s*\d+\s*,\s*\d+\s*,\s*\d+\s*,\s*[\d.]+\s*\)$/)`, true
	case "hsl":
		return base + `.regex(/^hsl\(\s*\d+\s*,\s*[\d.]+%\s*,\s*[\d.]+%\s*\)$/)`, true
	case "hsla":
		return base + `.regex(/^hsla\(\s*\d+\s*,\s*[\d.]+%\s*,\s*[\d.]+%\s*,\s*[\d.]+\s*\)$/)`, true
	case "ascii":
		return base + `.regex(/^[\x00-\x7F]+$/)`, true
	case "printascii":
		return base + `.regex(/^[\x20-\x7E]+$/)`, true
	case "e164":
		return base + `.regex(/^\+[1-9]\d{1,14}$/)`, true
	case "ssn":
		return base + `.regex(/^\d{3}-\d{2}-\d{4}$/)`, true
	case "isbn13":
		return base + `.regex(/^97[89]\d{10}$/)`, true
	case "eth_addr":
		return base + `.regex(/^0x[a-fA-F0-9]{40}$/)`, true
	case "btc_addr":
		return base + `.regex(/^[13][a-km-zA-HJ-NP-Z1-9]{25,34}$/)`, true
	case "hostname":
		return base + `.regex(/^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/)`, true
	case "fqdn":
		return base + `.regex(/^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$/)`, true
	case "mac":
		return base + `.regex(/^([0-9A-Fa-f]{2}[:-]){5}([0-9A-Fa-f]{2})$/)`, true
	case "min", "gte":
		return base + fmt.Sprintf(".min(%s)", r.Value), true
	case "max", "lte":
		return base + fmt.Sprintf(".max(%s)", r.Value), true
	case "len", "eq":
		return base + fmt.Sprintf(".length(%s)", r.Value), true
	case "gt":
		n, err := strconv.Atoi(r.Value)
		if err != nil {
			return "", false
		}
		return base + fmt.Sprintf(".min(%d)", n+1), true
	case "lt":
		n, err := strconv.Atoi(r.Value)
		if err != nil {
			return "", false
		}
		return base + fmt.Sprintf(".max(%d)", n-1), true
	case "ne":
		return base + fmt.Sprintf(`.refine(v => v.length !== %s, { message: "length must not be %s" })`, r.Value, r.Value), true
	case "contains":
		return base + fmt.Sprintf(`.includes(%q)`, r.Value), true
	case "excludes":
		return base + fmt.Sprintf(`.refine(v => !v.includes(%q))`, r.Value), true
	case "startswith":
		return base + fmt.Sprintf(`.startsWith(%q)`, r.Value), true
	case "endswith":
		return base + fmt.Sprintf(`.endsWith(%q)`, r.Value), true
	case "lowercase":
		return base + `.toLowerCase()`, true
	case "uppercase":
		return base + `.toUpperCase()`, true
	case "notoneof":
		vals := splitSpaceValues(r.Value)
		quoted := make([]string, len(vals))
		for i, v := range vals {
			quoted[i] = fmt.Sprintf("%q", v)
		}
		return base + fmt.Sprintf(`.refine(v => ![%s].includes(v))`, strings.Join(quoted, ", ")), true
	}
	return "", false
}

func applyNumberRule(base string, r validatorRule) (string, bool) {
	switch r.Name {
	case "min", "gte":
		return base + fmt.Sprintf(".gte(%s)", r.Value), true
	case "max", "lte":
		return base + fmt.Sprintf(".lte(%s)", r.Value), true
	case "gt":
		return base + fmt.Sprintf(".gt(%s)", r.Value), true
	case "lt":
		return base + fmt.Sprintf(".lt(%s)", r.Value), true
	case "eq":
		return base + fmt.Sprintf(".refine(v => v === %s)", r.Value), true
	case "ne":
		return base + fmt.Sprintf(".refine(v => v !== %s)", r.Value), true
	case "positive":
		return base + ".positive()", true
	case "negative":
		return base + ".negative()", true
	case "multipleof":
		return base + fmt.Sprintf(".multipleOf(%s)", r.Value), true
	}
	return "", false
}

func applyOptionalZeroNumberRule(base string, r validatorRule) (string, bool) {
	switch r.Name {
	case "min", "gte":
		return base + fmt.Sprintf(".refine(v => v === 0 || v >= %s)", r.Value), true
	case "max", "lte":
		return base + fmt.Sprintf(".refine(v => v === 0 || v <= %s)", r.Value), true
	case "gt":
		return base + fmt.Sprintf(".refine(v => v === 0 || v > %s)", r.Value), true
	case "lt":
		return base + fmt.Sprintf(".refine(v => v === 0 || v < %s)", r.Value), true
	case "eq":
		return base + fmt.Sprintf(".refine(v => v === 0 || v === %s)", r.Value), true
	case "ne":
		return base + fmt.Sprintf(".refine(v => v === 0 || v !== %s)", r.Value), true
	case "positive":
		return base + ".refine(v => v === 0 || v > 0)", true
	case "negative":
		return base + ".refine(v => v === 0 || v < 0)", true
	case "multipleof":
		return base + fmt.Sprintf(".refine(v => v === 0 || v %% %s === 0)", r.Value), true
	}
	return "", false
}

func buildArrayZod(elemType string, f FieldContract, tsStructs, raw map[string]StructDefinition, colliding map[string]bool) (string, []string) {
	rules := parseValidatorRules(f.Validation)

	var pre, post []validatorRule
	seenDive := false
	for _, r := range rules {
		if r.Name == "dive" {
			seenDive = true
			continue
		}
		if seenDive {
			post = append(post, r)
		} else {
			pre = append(pre, r)
		}
	}

	var unknown []string
	var elemZod string
	if len(post) > 0 {
		elemField := FieldContract{Type: elemType, Validation: rulesToTag(post)}
		e, u := buildScalarZod(elemField, tsStructs, raw, colliding)
		elemZod = e
		unknown = append(unknown, u...)
	} else {
		elemZod = scalarZodBase(elemType, tsStructs, raw, colliding)
	}

	arr := fmt.Sprintf("z.array(%s)", elemZod)

	for _, r := range pre {
		switch r.Name {
		case "required", "omitempty":
			continue
		case "min", "gte":
			arr += fmt.Sprintf(".min(%s)", r.Value)
		case "max", "lte":
			arr += fmt.Sprintf(".max(%s)", r.Value)
		case "len":
			arr += fmt.Sprintf(".length(%s)", r.Value)
		case "unique":
			arr += `.refine(v => new Set(v).size === v.length, { message: "must be unique" })`
		default:
			unknown = append(unknown, r.Name)
		}
	}
	return arr, unknown
}
