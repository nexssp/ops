package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveStudioFile(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	if err := os.WriteFile("z-flow.nflow", []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("a-flow.nflow", []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	selected, err := resolveStudioFile(nil)
	if err != nil || selected != "a-flow.nflow" {
		t.Fatalf("default selection = %q, err=%v; want first .nflow", selected, err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.nflow"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	selected, err = resolveStudioFile(nil)
	if err != nil || selected != "app.nflow" {
		t.Fatalf("app selection = %q, err=%v; want app.nflow", selected, err)
	}
	selected, err = resolveStudioFile([]string{"z-flow.nflow"})
	if err != nil || selected != "z-flow.nflow" {
		t.Fatalf("explicit selection = %q, err=%v", selected, err)
	}
	if _, err := resolveStudioFile([]string{"a.nflow", "b.nflow"}); err == nil {
		t.Fatal("multiple paths should be rejected")
	}
}
