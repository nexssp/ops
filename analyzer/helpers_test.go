package analyzer_test

import "github.com/nexssp/ops/analyzer"

// generatedByName indexes the []GeneratedFile tree by its relative Path so
// tests can assert on specific outputs without depending on iteration order.
func generatedByName(files []analyzer.GeneratedFile) map[string][]byte {
	out := make(map[string][]byte, len(files))
	for _, f := range files {
		out[f.Path] = f.Content
	}
	return out
}
