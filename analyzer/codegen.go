package analyzer

import "github.com/nexssp/ops/analyzer/core"

func GenerateTypeScriptSDK(report *Report) []GeneratedFile {
	return core.GenerateTypeScriptSDK(flowAwareReport(report))
}

func GenerateTypeScriptAPISDK(report *Report) []GeneratedFile {
	return core.GenerateTypeScriptAPISDK(flowAwareReport(report))
}

func ValidatePublicAPI(report *Report) []Diagnostic {
	return validateFlowPublicAPI(report)
}

func RenderSummaryTable(report *Report, actionFilter string) string {
	return core.RenderSummaryTable(report, actionFilter)
}

func RenderEnhancedReport(report *Report, actionFilter string) string {
	return core.RenderEnhancedReport(report, actionFilter)
}

func StampContent(content []byte, path string) []byte {
	return core.StampContent(content, path)
}

func ContentIsUnmodified(onDisk []byte) bool {
	return core.ContentIsUnmodified(onDisk)
}
