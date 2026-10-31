package analyzer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func TestGenerateJumaluSDK(t *testing.T) {
	if _, err := os.Stat("../../jumalu"); os.IsNotExist(err) {
		t.Skip("skipping local integration test: ../../jumalu not found")
	}

	report, err := analyzer.Analyze("../../jumalu")
	if err != nil {
		t.Fatalf("analyzer error: %v", err)
	}

	files := analyzer.GenerateTypeScriptSDK(report)
	outDir := "../../jumalu/ui/src/lib"

	_ = os.MkdirAll(outDir, 0o755)

	for _, f := range files {
		full := filepath.Join(outDir, f.Path)

		if !f.Overwrite {
			if _, err := os.Stat(full); err == nil {
				t.Logf("⏭️  preserved (write-once): %s", full)
				continue
			}
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", full, err)
		}
		if err := os.WriteFile(full, f.Content, 0o600); err != nil {
			t.Fatalf("failed to write %s: %v", full, err)
		}
		t.Logf("✅ wrote: %s (%d bytes)", full, len(f.Content))
	}
}
