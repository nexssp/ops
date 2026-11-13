package core

import (
	"fmt"
	"strings"
)

func computeReachableStructs(report *Report, tsStructs map[string]StructDefinition, colliding map[string]bool, includeExposed bool) map[string]StructDefinition {
	reachable := make(map[string]StructDefinition)
	queue := []string{}
	visited := make(map[string]bool)

	for _, act := range report.Actions {
		if act.Scope == "public" && act.Path != "" {
			if sDef, ok := resolveActionStruct(act, true, report.Structs); ok {
				queue = append(queue, sDef.Qualified)
			}
			if sDef, ok := resolveActionStruct(act, false, report.Structs); ok {
				queue = append(queue, sDef.Qualified)
			}
		}
	}

	if includeExposed {
		for _, sDef := range report.Structs {
			if sDef.IsIgnored {
				continue
			}
			if sDef.IsExposed {
				queue = append(queue, sDef.Qualified)
			}
		}
	}

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]

		if name == "" || name == "unknown" || visited[name] {
			continue
		}
		visited[name] = true

		if sDef, ok := findStructByAny(name, tsStructs, report.Structs); ok {
			reachable[sDef.Qualified] = sDef
			for _, f := range sDef.Fields {
				baseT := extractBaseType(f.Type)
				if baseT != "string" && baseT != "number" && baseT != "boolean" && baseT != "IsoDateString" && baseT != "unknown" {
					queue = append(queue, baseT)
				}
			}
		}
	}
	return reachable
}

func analyzeCollisions(structs map[string]StructDefinition) (map[string]bool, map[string]StructDefinition) {
	namePkgCounts := make(map[string]map[string]bool)
	uniqueDefs := make(map[string]StructDefinition)

	for _, sDef := range structs {
		uniqueDefs[sDef.Qualified] = sDef
		if namePkgCounts[sDef.Name] == nil {
			namePkgCounts[sDef.Name] = make(map[string]bool)
		}
		pkgKey := sDef.Package
		if pkgKey == "" {
			pkgKey = sDef.Domain
		}
		namePkgCounts[sDef.Name][pkgKey] = true
	}

	colliding := make(map[string]bool)
	for name, pkgs := range namePkgCounts {
		if len(pkgs) > 1 {
			colliding[name] = true
		}
	}

	tsStructs := make(map[string]StructDefinition)
	for _, sDef := range uniqueDefs {
		tsStructs[targetTSName(sDef, colliding)] = sDef
	}
	return colliding, tsStructs
}

func targetTSName(sDef StructDefinition, colliding map[string]bool) string {
	if sDef.CustomName != "" {
		return sDef.CustomName
	}
	if colliding[sDef.Name] {
		prefix := toPascalCase(sDef.Package)
		if prefix == "" {
			prefix = toPascalCase(sDef.Domain)
		}
		return prefix + sDef.Name
	}
	return sDef.Name
}

func resolveActionStruct(act ActionContract, isReq bool, raw map[string]StructDefinition) (StructDefinition, bool) {
	ref := act.ReqType
	typeName := act.ReqTypeName
	if !isReq {
		ref = act.ResType
		typeName = act.ResTypeName
	}

	if ref.Package != "" && ref.Name != "" {
		refName := stripArrayTypePrefixes(ref.Name)
		if s, ok := raw[ref.Package+"."+refName]; ok {
			return s, true
		}
	}

	if idx := strings.IndexByte(act.Name, '.'); idx > 0 {
		pkg := act.Name[:idx]
		cleanType := typeName
		if dotIdx := strings.LastIndex(cleanType, "."); dotIdx >= 0 {
			cleanType = cleanType[dotIdx+1:]
		}
		if s, ok := raw[pkg+"."+cleanType]; ok {
			return s, true
		}
	}

	clean := typeName
	if idx := strings.LastIndex(clean, "."); idx >= 0 {
		clean = clean[idx+1:]
	}
	clean = stripArrayTypePrefixes(clean)
	for _, s := range raw {
		if s.Name == clean || s.Qualified == typeName || s.Qualified == clean {
			return s, true
		}
	}

	return StructDefinition{}, false
}

func resolveActionTypeTS(act ActionContract, isReq bool, tsStructs, raw map[string]StructDefinition, colliding map[string]bool) string {
	if sDef, ok := resolveActionStruct(act, isReq, raw); ok {
		typeName := act.ReqTypeName
		if !isReq {
			typeName = act.ResTypeName
		}
		return targetTSName(sDef, colliding) + strings.Repeat("[]", arrayTypeDepth(typeName))
	}

	typeName := act.ReqTypeName
	if !isReq {
		typeName = act.ResTypeName
	}
	if typeName == "" {
		return "unknown"
	}

	clean := typeName
	if idx := strings.LastIndex(clean, "."); idx >= 0 {
		clean = clean[idx+1:]
	}
	return mapGoTypeToTS(clean)
}

func stripArrayTypePrefixes(typeName string) string {
	for strings.HasPrefix(typeName, "[]") {
		typeName = strings.TrimSpace(strings.TrimPrefix(typeName, "[]"))
	}
	return typeName
}

func arrayTypeDepth(typeName string) int {
	if idx := strings.LastIndexByte(typeName, '.'); idx >= 0 {
		typeName = typeName[idx+1:]
	}
	depth := 0
	for strings.HasPrefix(typeName, "[]") {
		depth++
		typeName = strings.TrimSpace(strings.TrimPrefix(typeName, "[]"))
	}
	return depth
}

func findStructByAny(name string, tsStructs, raw map[string]StructDefinition) (StructDefinition, bool) {
	if s, ok := tsStructs[name]; ok {
		return s, true
	}
	if s, ok := raw[name]; ok {
		return s, true
	}
	for _, s := range raw {
		if s.Qualified == name || s.Name == name {
			return s, true
		}
	}
	return StructDefinition{}, false
}

func extractBaseType(tsType string) string {
	t := strings.TrimSuffix(tsType, "[]")
	if strings.HasPrefix(t, "Record<") {
		idx := strings.LastIndex(t, ", ")
		if idx > 0 {
			t = t[idx+2 : len(t)-1]
		}
	}
	return strings.TrimSpace(t)
}

func mapGoTypeToTS(t string) string {
	t = strings.TrimPrefix(strings.TrimSpace(t), "*")
	switch t {
	case "[]byte":
		return "string"
	case "string", "TaskStatus", "AgentRole", "EventType":
		return "string"
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64", "time.Duration", "Duration":
		return "number"
	case "bool":
		return "boolean"
	case "time.Time", "Time", "CustomDate":
		return "IsoDateString"
	case "any", "interface{}", "error":
		return "unknown"
	case "struct{}":
		return "void"
	default:
		if strings.HasPrefix(t, "[]") {
			return mapGoTypeToTS(t[2:]) + "[]"
		}
		if strings.HasPrefix(t, "map[string]") {
			return fmt.Sprintf("Record<string, %s>", mapGoTypeToTS(t[11:]))
		}
		if idx := strings.Index(t, "."); idx >= 0 {
			pkg := t[:idx]
			elem := t[idx+1:]
			if pkg == "time" && elem == "Duration" {
				return "number"
			}
			return toPascalCase(pkg) + elem
		}
		return t
	}
}

func toCamelCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '_' || r == '-' || r == '/' })
	if len(parts) == 0 {
		return s
	}
	var b strings.Builder
	b.WriteString(strings.ToLower(parts[0]))
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			b.WriteString(strings.ToUpper(parts[i][:1]))
			b.WriteString(parts[i][1:])
		}
	}
	return b.String()
}

func toPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '_' || r == '-' || r == '/' })
	var b strings.Builder
	for _, p := range parts {
		if len(p) > 0 {
			b.WriteString(strings.ToUpper(p[:1]))
			b.WriteString(p[1:])
		}
	}
	return b.String()
}
