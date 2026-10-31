package deploygen

import (
	"fmt"
	"text/template"
	"time"
)

var funcMap = template.FuncMap{
	"now":    func() string { return time.Now().UTC().Format(time.RFC3339) },
	"printf": fmt.Sprintf,
}

var (
	// SvelteKit Project Scaffolding
	uiPackageJSONTmpl  = template.Must(template.New("ui_package.json.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_package.json.tmpl"))
	uiSvelteConfigTmpl = template.Must(template.New("ui_svelte.config.js.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_svelte.config.js.tmpl"))
	uiViteConfigTmpl   = template.Must(template.New("ui_vite.config.ts.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_vite.config.ts.tmpl"))
	uiTSConfigTmpl     = template.Must(template.New("ui_tsconfig.json.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_tsconfig.json.tmpl"))
	uiAppHTMLTmpl      = template.Must(template.New("ui_app.html.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_app.html.tmpl"))
	uiAppCSSTmpl       = template.Must(template.New("ui_app.css.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_app.css.tmpl"))

	// SvelteKit Routes
	uiLayoutTSTmpl     = template.Must(template.New("ui_layout.ts.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_layout.ts.tmpl"))
	uiLayoutSvelteTmpl = template.Must(template.New("ui_layout.svelte.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_layout.svelte.tmpl"))
	uiPageSvelteTmpl   = template.Must(template.New("ui_page.svelte.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_page.svelte.tmpl"))
	uiErrorSvelteTmpl  = template.Must(template.New("ui_error.svelte.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_error.svelte.tmpl"))
	uiLoginSvelteTmpl  = template.Must(template.New("ui_login.svelte.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_login.svelte.tmpl"))

	// Libraries, Client & Stores
	uiClientTSTmpl  = template.Must(template.New("ui_client.ts.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_client.ts.tmpl"))
	uiAPIBarrelTmpl = template.Must(template.New("ui_api.ts.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_api.ts.tmpl"))
	uiCustomAPITmpl = template.Must(template.New("ui_api.custom.ts.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_api.custom.ts.tmpl"))
	uiAuthStoreTmpl = template.Must(template.New("ui_auth.svelte.ts.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_auth.svelte.ts.tmpl"))
	uiRouterTmpl    = template.Must(template.New("ui_router.svelte.ts.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_router.svelte.ts.tmpl"))

	// Svelte 5 Runes CRUD Utilities
	uiFormActionTmpl  = template.Must(template.New("ui_form_action.svelte.ts.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_form_action.svelte.ts.tmpl"))
	uiUseCrudTmpl     = template.Must(template.New("ui_use_crud.svelte.ts.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_use_crud.svelte.ts.tmpl"))
	uiTableHelperTmpl = template.Must(template.New("ui_table.ts.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_table.ts.tmpl"))

	// UI Atomic Components
	uiAppButtonTmpl     = template.Must(template.New("ui_app_button.svelte.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_app_button.svelte.tmpl"))
	uiAppTableTmpl      = template.Must(template.New("ui_app_table.svelte.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_app_table.svelte.tmpl"))
	uiAppPaginationTmpl = template.Must(template.New("ui_app_pagination.svelte.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_app_pagination.svelte.tmpl"))
	uiConfirmModalTmpl  = template.Must(template.New("ui_confirm_modal.svelte.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_confirm_modal.svelte.tmpl"))
	uiAppListViewTmpl   = template.Must(template.New("ui_app_list_view.svelte.tmpl").Delims("<%", "%>").Funcs(funcMap).ParseFS(tmplFS, "tmpl/ui_app_list_view.svelte.tmpl"))
)
