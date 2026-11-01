package deploygen

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

type UIFileSpec struct {
	RelPath  string
	Template *template.Template
}

type UIConfig struct {
	ServiceName string
	HTTPPort    string
	ObsPort     string
}

// ScaffoldSvelteKitFromTemplates renderuje pełny zestaw szablonów SvelteKit do folderu ui/
func ScaffoldSvelteKitFromTemplates(ctx context.Context, baseDir string, cfg UIConfig, force bool) (int, error) {
	if cfg.HTTPPort == "" {
		cfg.HTTPPort = "8080"
	}
	if cfg.ObsPort == "" {
		cfg.ObsPort = "9090"
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = "nexss-app"
	}

	uiDir := filepath.Join(baseDir, "ui")

	specs := []UIFileSpec{
		// 1. Konfiguracja projektu
		{"package.json", uiPackageJSONTmpl},
		{"vite.config.ts", uiViteConfigTmpl},
		{"tsconfig.json", uiTSConfigTmpl},
		{filepath.Join("src", "app.html"), uiAppHTMLTmpl},
		{filepath.Join("src", "app.css"), uiAppCSSTmpl},

		// 2. Trasy SvelteKit
		{filepath.Join("src", "routes", "+layout.ts"), uiLayoutTSTmpl},
		{filepath.Join("src", "routes", "+layout.svelte"), uiLayoutSvelteTmpl},
		{filepath.Join("src", "routes", "+page.svelte"), uiPageSvelteTmpl},
		{filepath.Join("src", "routes", "+error.svelte"), uiErrorSvelteTmpl},

		// 3. Reusable CRUD utilities (Svelte 5 Runes)
		{filepath.Join("src", "lib", "utils", "form-action.svelte.ts"), uiFormActionTmpl},
		{filepath.Join("src", "lib", "utils", "useCrud.svelte.ts"), uiUseCrudTmpl},
		{filepath.Join("src", "lib", "utils", "table.ts"), uiTableHelperTmpl},

		// 4. Komponenty UI
		{filepath.Join("src", "lib", "components", "ui", "AppButton.svelte"), uiAppButtonTmpl},
		{filepath.Join("src", "lib", "components", "ui", "AppTable.svelte"), uiAppTableTmpl},
		{filepath.Join("src", "lib", "components", "ui", "AppPagination.svelte"), uiAppPaginationTmpl},
		{filepath.Join("src", "lib", "components", "ui", "ConfirmModal.svelte"), uiConfirmModalTmpl},
		{filepath.Join("src", "lib", "components", "ui", "AppListView.svelte"), uiAppListViewTmpl},
	}

	written := 0
	for _, spec := range specs {
		targetPath := filepath.Join(uiDir, spec.RelPath)

		if _, err := os.Stat(targetPath); err == nil && !force {
			continue // Zachowaj istniejący plik, chyba że użyto flagi --force
		}

		var buf bytes.Buffer
		if err := spec.Template.Execute(&buf, cfg); err != nil {
			return written, fmt.Errorf("render %s: %w", spec.RelPath, err)
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return written, fmt.Errorf("mkdir %s: %w", filepath.Dir(targetPath), err)
		}

		if err := os.WriteFile(targetPath, buf.Bytes(), 0o600); err != nil {
			return written, fmt.Errorf("write %s: %w", targetPath, err)
		}

		written++
		fmt.Printf("  ├── %s\n", targetPath)
	}

	return written, nil
}
