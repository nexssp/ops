package analyzer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nexssp/ops/analyzer/core"
)

type Analyzer struct {
	*core.Analyzer
	flow *flowAnalyzer
}

func New() *Analyzer {
	return &Analyzer{Analyzer: core.New(), flow: newFlowAnalyzer(nil)}
}

func (a *Analyzer) ScanFlowFiles(root string) {
	if a.flow == nil {
		a.flow = newFlowAnalyzer(nil)
	}
	a.flow.ScanFlowFiles(root)
}

// Analyze preserves the existing Flow-aware single-root API.
func Analyze(root string) (*Report, error) {
	return AnalyzeAll(root)
}

// AnalyzeAll analyzes Go contracts and merges Flow DSL and NFlow inputs.
func AnalyzeAll(roots ...string) (*Report, error) {
	report, err := core.AnalyzeGoAll(roots...)
	if err != nil {
		return nil, err
	}

	flow := newFlowAnalyzer(report.Actions)
	scanRoots := roots
	if len(scanRoots) == 0 {
		scanRoots = []string{"."}
	}
	for _, root := range scanRoots {
		if root == "" {
			root = "."
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			absRoot = filepath.Clean(root)
		}
		info, err := os.Stat(absRoot)
		if err != nil {
			return nil, fmt.Errorf("scan root %q: %w", root, err)
		}
		scanDir := absRoot
		if !info.IsDir() {
			scanDir = filepath.Dir(absRoot)
		}

		if err := flow.scanEmbeddedDSL(scanDir); err != nil {
			return nil, err
		}
		for _, extra := range core.LocalDependencyRoots(scanDir) {
			if err := flow.scanEmbeddedDSL(extra); err != nil {
				return nil, err
			}
		}
		flow.ScanFlowFiles(scanDir)
	}

	flow.finishReport(report)
	return report, nil
}
