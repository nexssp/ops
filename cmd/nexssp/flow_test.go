package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunFlowDoesNotDuplicateNativeBundles(t *testing.T) {
	flowPath := filepath.Join(t.TempDir(), "native-defaults-smoke.nflow")
	source := `@description "nexssp Flow native-defaults regression test"
@assert: result.value == 7
{ value: 7 }
`
	if err := os.WriteFile(flowPath, []byte(source), 0o600); err != nil {
		t.Fatalf("write Flow smoke program: %v", err)
	}

	if code := runFlowCLI([]string{flowPath, "--json"}); code != 0 {
		t.Fatalf("nexssp Flow execution exited with code %d; Flow's native defaults must be installed exactly once", code)
	}
}
