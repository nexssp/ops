package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func TestParseScanArgsPruneGenerated(t *testing.T) {
	opts := parseScanArgs([]string{"--prune-generated"}, true)
	if !opts.PruneGenerated {
		t.Fatal("--prune-generated was not recorded")
	}
}

func TestPruneStaleGeneratedStoreFiles(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"_gen/stores/entities/old.svelte.ts":   "stale",
		"_gen/stores/forms/current.svelte.ts":  "current",
		"_gen/stores/runtime/entity.svelte.ts": "runtime",
		"custom/stores.ts":                     "custom",
	}
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := pruneStaleGeneratedStoreFiles(root, []analyzer.GeneratedFile{{Path: "_gen/stores/forms/current.svelte.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed %d files, want 1", removed)
	}
	for rel, wantPresent := range map[string]bool{
		"_gen/stores/entities/old.svelte.ts":   false,
		"_gen/stores/forms/current.svelte.ts":  true,
		"_gen/stores/runtime/entity.svelte.ts": true,
		"custom/stores.ts":                     true,
	} {
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		present := err == nil
		if present != wantPresent {
			t.Errorf("%s present=%v, want %v (stat err %v)", rel, present, wantPresent, err)
		}
	}
}
