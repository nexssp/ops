package core

import (
	"fmt"
	"sort"
	"strings"
)

func RenderSummaryTable(report *Report, actionFilter string) string {
	var sb strings.Builder

	sb.WriteString("\n⚡ NEXSS ACTION REGISTRY\n")
	sb.WriteString("┌────────────────────────────────────┬──────────┬─────────────────────────────────────┬──────────────┬───────────────────────────────┐\n")
	sb.WriteString("│ Action Name                        │ Method   │ HTTP Route                          │ Resilience   │ Description                   │\n")
	sb.WriteString("├────────────────────────────────────┼──────────┼─────────────────────────────────────┼──────────────┼───────────────────────────────┤\n")

	actions := make([]ActionContract, 0)
	for _, a := range report.Actions {
		if actionFilter == "" || strings.Contains(strings.ToLower(a.Name), strings.ToLower(actionFilter)) || strings.Contains(strings.ToLower(a.Path), strings.ToLower(actionFilter)) {
			actions = append(actions, a)
		}
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i].Name < actions[j].Name })

	for _, a := range actions {
		resilience := []string{}
		if a.Idempotent {
			resilience = append(resilience, "IDEM")
		}
		if a.Timeout > 0 {
			resilience = append(resilience, fmt.Sprintf("%v", a.Timeout))
		}
		if a.RetryMax > 0 {
			resilience = append(resilience, fmt.Sprintf("%dx", a.RetryMax))
		}
		if a.CacheTTL > 0 {
			resilience = append(resilience, fmt.Sprintf("cache:%v", a.CacheTTL))
		}
		resStr := strings.Join(resilience, "·")
		if resStr == "" {
			resStr = "standard"
		}

		desc := a.Description
		if len(desc) > 29 {
			desc = desc[:26] + "..."
		}
		if desc == "" {
			desc = "-"
		}

		name := padRight(a.Name, 34)
		method := padRight(a.Method, 8)
		route := padRight(a.Path, 35)
		resPadded := padRight(resStr, 12)
		descPadded := padRight(desc, 29)

		fmt.Fprintf(&sb, "│ %s │ %s │ %s │ %s │ %s │\n", name, method, route, resPadded, descPadded)
	}

	sb.WriteString("└────────────────────────────────────┴──────────┴─────────────────────────────────────┴──────────────┴───────────────────────────────┘\n")
	fmt.Fprintf(&sb, "Showing: %d actions (Total: %d)\n\n", len(actions), report.Stats.TotalActions)

	return sb.String()
}

func RenderEnhancedReport(report *Report, actionFilter string) string {
	var sb strings.Builder

	actions := make([]ActionContract, 0)
	for _, a := range report.Actions {
		if actionFilter == "" || strings.Contains(strings.ToLower(a.Name), strings.ToLower(actionFilter)) || strings.Contains(strings.ToLower(a.Path), strings.ToLower(actionFilter)) {
			actions = append(actions, a)
		}
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i].Name < actions[j].Name })

	sb.WriteString(fmt.Sprintf("\n🔬 NEXSS ACTION REPORT ENHANCED (%d action(s))\n", len(actions)))
	sb.WriteString("════════════════════════════════════════════════════════════════════════════════\n\n")

	for i, a := range actions {
		fmt.Fprintf(&sb, "📦 [%d/%d] ACTION: \033[1;36m%s\033[0m\n", i+1, len(actions), a.Name)
		fmt.Fprintf(&sb, "  ├── 📝 Description:   %s\n", defaultStr(a.Description, "(no description)"))
		fmt.Fprintf(&sb, "  ├── 🌐 HTTP Route:     \033[1;32m%s\033[0m %s\n", a.Method, a.Path)
		fmt.Fprintf(&sb, "  ├── 🏷️  Scope / Tags:   [%s] %v\n", strings.ToUpper(a.Scope), a.Tags)
		fmt.Fprintf(&sb, "  ├── 📍 Source Code:   %s:%d\n", a.SourceFile, a.SourceLine)

		reqType := defaultStr(a.ReqTypeName, "struct{} (empty)")
		fmt.Fprintf(&sb, "  ├── 📥 Request DTO:   \033[1;33m%s\033[0m (%d fields)\n", reqType, len(a.ReqFields))
		for _, f := range a.ReqFields {
			reqBadge := "opt"
			if f.Required {
				reqBadge = "\033[1;31mREQ\033[0m"
			}
			fmt.Fprintf(&sb, "  │    ├── %-18s %-12s json:%-16s %-4s validate:%q\n", f.Name, f.Type, `"`+f.Key+`"`, reqBadge, f.Validation)
		}

		resType := defaultStr(a.ResTypeName, "struct{} (empty)")
		fmt.Fprintf(&sb, "  └── 📤 Response DTO:  \033[1;32m%s\033[0m (%d fields)\n", resType, len(a.ResFields))
		for _, f := range a.ResFields {
			fmt.Fprintf(&sb, "       ├── %-18s %-12s json:\"%s\"\n", f.Name, f.Type, f.Key)
		}

		sb.WriteString("\n────────────────────────────────────────────────────────────────────────────────\n\n")
	}

	return sb.String()
}

func defaultStr(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
