package analyzer

import "github.com/nexssp/ops/analyzer/core"

type (
	TransportBinding   = core.TransportBinding
	DSLConfig          = core.DSLConfig
	StructKey          = core.StructKey
	TypeRef            = core.TypeRef
	ActionContract     = core.ActionContract
	FieldContract      = core.FieldContract
	FlowHTTPRoute      = core.FlowHTTPRoute
	StructDefinition   = core.StructDefinition
	DiagnosticSeverity = core.DiagnosticSeverity
	Diagnostic         = core.Diagnostic
	Report             = core.Report
	ReportStats        = core.ReportStats
	EnumDefinition     = core.EnumDefinition
	EnumValue          = core.EnumValue
	PackageInfo        = core.PackageInfo
	GeneratedFile      = core.GeneratedFile
	StoreCluster       = core.StoreCluster
)

const (
	SeverityError   = core.SeverityError
	SeverityWarning = core.SeverityWarning
)
