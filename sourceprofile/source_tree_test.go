package sourceprofile

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildSourceTreeProfile_ResolvesAliasesAndExtensions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, path := range []string{
		"main.go",
		"app/index.ts",
		"app/nested/view.tsx",
		"scripts/run.mjs",
		"config/settings.CUSTOM",
		"config/nested/extra.custom",
		"docs/readme.md",
		"vendor/ignored.ts",
		"node_modules/pkg/ignored.tsx",
		"dist/ignored.mjs",
		".hidden/ignored.go",
	} {
		writeSourceTreeFile(t, root, path)
	}

	got, err := BuildSourceTreeProfile(SourceTreeProfileOptions{
		Root:            root,
		ProfileName:     "service",
		Types:           []string{" TypeScript, .GO "},
		TypeDefinitions: map[string][]string{"typescript": {"ts", "tsx"}},
		Extensions:      ".MJS, .CUSTOM",
	})
	if err != nil {
		t.Fatalf("BuildSourceTreeProfile() error = %v", err)
	}

	if got.Name != "service" {
		t.Errorf("Name = %q, want service", got.Name)
	}
	if got.BaseDir != root {
		t.Errorf("BaseDir = %q, want %q", got.BaseDir, root)
	}
	wantDescription := "Auto-generated context profile for " + filepath.Base(root)
	if got.Description != wantDescription {
		t.Errorf("Description = %q, want %q", got.Description, wantDescription)
	}
	if got.Options.RemoveBodyFunc {
		t.Error("RemoveBodyFunc = true, want false")
	}
	if got.Options.ExportedOnly {
		t.Error("ExportedOnly = true, want false")
	}
	if got.Options.ExtensionFilter != "custom,go,mjs,ts,tsx" {
		t.Errorf("ExtensionFilter = %q, want custom,go,mjs,ts,tsx", got.Options.ExtensionFilter)
	}
	if want := []string{".", "app", "config", "scripts"}; !reflect.DeepEqual(got.Directories, want) {
		t.Errorf("Directories = %v, want %v", got.Directories, want)
	}
}

func TestBuildSourceTreeProfile_AllTypesAndExcludedDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, path := range []string{
		"README.md",
		"src/main.rs",
		"src/nested/child.txt",
		"private/ignored.go",
		"src/private/ignored.go",
		"vendor/ignored.go",
		".git/config",
	} {
		writeSourceTreeFile(t, root, path)
	}

	got, err := BuildSourceTreeProfile(SourceTreeProfileOptions{
		Root:        root,
		ProfileName: "full",
		ExcludeDirs: []string{"private", "src/private"},
	})
	if err != nil {
		t.Fatalf("BuildSourceTreeProfile() error = %v", err)
	}
	if got.Options.ExtensionFilter != "" {
		t.Errorf("ExtensionFilter = %q, want empty for all-types mode", got.Options.ExtensionFilter)
	}
	if want := []string{".", "src"}; !reflect.DeepEqual(got.Directories, want) {
		t.Errorf("Directories = %v, want %v", got.Directories, want)
	}
}

func TestBuildSourceTreeProfile_RejectsInvalidRootAndEscapingExclusion(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "file.go")
	if err := os.WriteFile(file, []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, root := range []string{filepath.Join(t.TempDir(), "missing"), file} {
		if _, err := BuildSourceTreeProfile(SourceTreeProfileOptions{Root: root}); err == nil {
			t.Errorf("BuildSourceTreeProfile(root=%q) error = nil, want error", root)
		}
	}
	if _, err := BuildSourceTreeProfile(SourceTreeProfileOptions{
		Root:        t.TempDir(),
		ExcludeDirs: []string{"../outside"},
	}); err == nil {
		t.Fatal("BuildSourceTreeProfile() error = nil, want escaping exclusion error")
	}
}

func writeSourceTreeFile(t *testing.T, root, path string) {
	t.Helper()
	fullPath := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	if err := os.WriteFile(fullPath, []byte("source\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
