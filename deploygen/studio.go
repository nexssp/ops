package deploygen

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed studio/web/src/App.svelte
var studioAppSource embed.FS

// ScaffoldStudio writes the shared action-workbench Svelte route into ui/src/routes/studio/.
// Returns the written path. When the file already exists and force is false, the
// existing file is preserved and the path is returned unchanged.
func ScaffoldStudio(baseDir string, force bool) (string, error) {
	target := filepath.Join(baseDir, "ui", "src", "routes", "studio", "+page.svelte")

	if _, err := os.Stat(target); err == nil && !force {
		return target, nil
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("mkdir studio: %w", err)
	}
	source, err := studioAppSource.ReadFile("studio/web/src/App.svelte")
	if err != nil {
		return "", fmt.Errorf("read Studio Svelte source: %w", err)
	}
	if err := os.WriteFile(target, source, 0o600); err != nil {
		return "", fmt.Errorf("write studio: %w", err)
	}
	return target, nil
}
