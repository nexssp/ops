package deploygen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScaffoldStudioUsesSharedWorkbench(t *testing.T) {
	root := t.TempDir()
	target, err := ScaffoldStudio(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if target != filepath.Join(root, "ui", "src", "routes", "studio", "+page.svelte") {
		t.Fatalf("unexpected target path: %s", target)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"/studio/api/actions", "RESPONSE", "HISTORY", "#c9a96e", "TRACE"} {
		if !strings.Contains(string(content), expected) {
			t.Errorf("scaffold is missing %q", expected)
		}
	}

	if err := os.WriteFile(target, []byte("user edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ScaffoldStudio(root, false); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "user edit" {
		t.Fatal("existing Studio route was overwritten without --force")
	}
	if _, err := ScaffoldStudio(root, true); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "user edit") || !strings.Contains(string(content), "Action Workbench") {
		t.Fatal("--force did not restore the shared workbench source")
	}
}
