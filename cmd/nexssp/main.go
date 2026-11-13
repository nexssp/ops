package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexssp/ops/analyzer"
	"github.com/nexssp/ops/deploygen"
)

func main() {
	if len(os.Args) < 2 {
		handleActionsList([]string{}, false)
		return
	}

	cmd := strings.ToLower(os.Args[1])
	subArgs := os.Args[2:]

	switch cmd {
	case "ui:init", "ui:scaffold", "ui":
		handleUIScaffold(subArgs)
	case "backend", "backend:gen":
		handleBackendGen(subArgs)
	case "sdk", "sdk:gen", "codegen":
		handleSDKGen(subArgs)
	case "actions", "actions:list", "routes":
		handleActionsList(subArgs, false)
	case "actions:enhanced", "actions:full", "action:inspect":
		handleActionsList(subArgs, true)
	case "analyze", "audit":
		handleAnalyze(subArgs)
	case "ui:studio":
		handleStudioScaffold(subArgs)
	case "studio":
		handleFlowStudio(subArgs)
	case "flow", "run:flow":
		handleFlow(subArgs)
	case "help", "--help", "-h":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "❌ Unknown command %q\n", cmd)
		fmt.Fprintf(os.Stderr, "💡 Run 'nexssp help' to see the available commands.\n")
		os.Exit(1)
	}
}

// handleFlow delegates to Flow's public CLI, which provides its native defaults.
// Application-specific libraries remain the responsibility of the application's own runner.
func handleFlow(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, `❌ Usage: nexssp flow <path.nflow> [json_payload] [flags]`)
		os.Exit(2)
	}
	os.Exit(runFlowCLI(args))
}

func handleUIScaffold(args []string) {
	force := false
	for _, a := range args {
		if a == "--force" || a == "-f" {
			force = true
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Could not determine project directory: %v\n", err)
		os.Exit(1)
	}
	cfg := deploygen.UIConfig{
		ServiceName: scaffoldServiceName(cwd),
		HTTPPort:    "8080",
		ObsPort:     "9090",
	}

	fmt.Println("🚀 Scaffolding SvelteKit 3 + Svelte 5 UI from the current project in ui/...")
	count, err := deploygen.ScaffoldSvelteKitFromTemplates(context.Background(), ".", cfg, force)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ UI Scaffold error: %v\n", err)
		fmt.Fprintf(os.Stderr, "💡 Check that ui/ is writable, then re-run with --force to overwrite existing files.\n")
		os.Exit(1)
	}

	handleSDKGen([]string{})

	fmt.Printf("\n✨ Scaffolding complete (%d files processed in ui/)!\n\n", count)
	fmt.Println("👉 Quick start:")
	fmt.Println("   1. cd ui && corepack pnpm install")
	fmt.Println("   2. corepack pnpm run dev -> http://localhost:5173")
}

func scaffoldServiceName(projectDir string) string {
	var name strings.Builder
	separator := true
	for _, r := range strings.ToLower(filepath.Base(projectDir)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			name.WriteRune(r)
			separator = false
		} else if !separator {
			name.WriteByte('-')
			separator = true
		}
	}
	result := strings.Trim(name.String(), "-")
	if result == "" {
		return "app"
	}
	return result
}

func handleSDKGen(args []string) {
	apiMode := len(args) > 0 && strings.EqualFold(args[0], "api")
	apiOutputDir := ""
	if apiMode {
		apiOutputDir, args = parseSDKAPIArgs(args[1:])
	}
	opts := parseScanArgs(args, true)
	if apiMode {
		opts.APIOnly = true
		if apiOutputDir == "" {
			apiOutputDir = filepath.Join("ui", "src", "lib", "_gen", "api")
		}
		if strings.HasSuffix(strings.ToLower(apiOutputDir), ".ts") {
			apiOutputDir = filepath.Dir(apiOutputDir)
		}
		opts.UILibDir = apiOutputDir
	}
	uiLibDir := opts.UILibDir
	if uiLibDir == "" {
		uiLibDir = filepath.Join("ui", "src", "lib")
	}

	if !opts.Quiet {
		fmt.Printf("🔍 Scanning AST and Action Mesh in %v...\n", opts.Roots)
	}

	report, err := analyzer.AnalyzeAll(opts.Roots...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ AST analysis failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "💡 Verify every --scan root exists and contains Go source files.\n")
		os.Exit(1)
	}

	analyzer.ExcludeFlowPipelines(report, opts.ExcludePipelines)

	printReportSummary(report, opts.Quiet)
	if diagnostics := analyzer.ValidatePublicAPI(report); len(diagnostics) > 0 {
		fmt.Fprintln(os.Stderr, "⛔ Public SDK generation requires concrete request and response types:")
		for _, diagnostic := range diagnostics {
			fmt.Fprintf(os.Stderr, "❌ [%s] %s\n", diagnostic.Rule, diagnostic.Message)
			if diagnostic.File != "" {
				fmt.Fprintf(os.Stderr, "   %s\n", diagnostic.File)
			}
		}
		fmt.Fprintln(os.Stderr, "💡 Fix the public contract; --force does not bypass this check.")
		os.Exit(1)
	}

	if report.Stats.ErrorCount > 0 && !opts.Force {
		fmt.Fprintf(os.Stderr, "⛔ Generation aborted: %d error(s) must be resolved.\n", report.Stats.ErrorCount)
		fmt.Fprintf(os.Stderr, "💡 Fix the diagnostics above, then re-run 'nexssp sdk'.\n")
		fmt.Fprintf(os.Stderr, "   Or pass --force to emit anyway (unsafe — the TS types may not match the wire).\n")
		os.Exit(1)
	}

	var files []analyzer.GeneratedFile
	if apiMode {
		files = analyzer.GenerateTypeScriptAPI(report)
	} else if opts.APIOnly {
		files = analyzer.GenerateTypeScriptAPISDK(report)
	} else {
		files = analyzer.GenerateTypeScriptSDK(report)
	}

	if !opts.Quiet {
		fmt.Printf("✨ Generating TypeScript SDK under %s/\n", uiLibDir)
	}

	written, preserved, refreshed := 0, 0, 0
	for _, f := range files {
		full := filepath.Join(uiLibDir, f.Path)

		// ── Write-once handling ──
		//
		// Without --force:
		//   • File doesn't exist                → write it.
		//   • File exists, hash matches marker  → write it (template
		//                                          changed on the Go side;
		//                                          disk is unmodified).
		//   • File exists, hash ≠ marker        → preserve (user edited).
		//
		// With --force:
		//   • Always write, regardless.
		if !f.Overwrite {
			if existing, err := os.ReadFile(full); err == nil {
				if !opts.Force {
					if !analyzer.ContentIsUnmodified(existing) {
						preserved++
						if !opts.Quiet {
							fmt.Printf("  ├── %-56s (preserved)\n", f.Path)
						}
						continue
					}
					refreshed++
				}
			}
		}

		// Runtime/write-once files get a hash marker so future runs can
		// tell user-edited from template-unchanged.
		content := f.Content
		if !f.Overwrite {
			content = analyzer.StampContent(content, f.Path)
		}

		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "❌ mkdir %q: %v\n", filepath.Dir(full), err)
			os.Exit(1)
		}
		if err := os.WriteFile(full, content, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "❌ write %s: %v\n", full, err)
			os.Exit(1)
		}
		written++
		if !opts.Quiet {
			tag := ""
			if !f.Overwrite && opts.Force {
				tag = " (forced)"
			}
			fmt.Printf("  ├── %-56s (%d B%s)\n", f.Path, len(content), tag)
		}
	}

	pruned := 0
	if opts.PruneGenerated {
		var err error
		pruned, err = pruneStaleGeneratedStoreFiles(uiLibDir, files)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ prune generated stores: %v\n", err)
			os.Exit(1)
		}
	}

	if !opts.Quiet {
		fmt.Printf("  └── %d written", written)
		if refreshed > 0 {
			fmt.Printf(", %d refreshed", refreshed)
		}
		if preserved > 0 {
			fmt.Printf(", %d preserved", preserved)
		}
		if pruned > 0 {
			fmt.Printf(", %d stale generated store files pruned", pruned)
		}
		fmt.Println()
		fmt.Println()
	}
}

// pruneStaleGeneratedStoreFiles removes only files in directories owned by
// codegen_stores.go that are absent from the current generated manifest. The
// runtime/, custom/, and app-level stores directories are deliberately outside
// the prune scope.
func pruneStaleGeneratedStoreFiles(uiLibDir string, generated []analyzer.GeneratedFile) (int, error) {
	keep := make(map[string]bool, len(generated))
	for _, file := range generated {
		keep[filepath.Clean(filepath.FromSlash(file.Path))] = true
	}

	removed := 0
	for _, category := range []string{"entities", "forms", "streams", "system"} {
		root := filepath.Join(uiLibDir, "_gen", "stores", category)
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return removed, err
		}
		err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(uiLibDir, path)
			if err != nil {
				return err
			}
			if keep[filepath.Clean(rel)] {
				return nil
			}
			if err := os.Remove(path); err != nil {
				return err
			}
			removed++
			return nil
		})
		if err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func handleActionsList(args []string, enhanced bool) {
	for _, a := range args {
		if a == "--enhanced" || a == "-e" || a == "--full" {
			enhanced = true
		}
	}

	opts := parseScanArgs(args, false)

	report, err := analyzer.AnalyzeAll(opts.Roots...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Static analysis failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "💡 Verify every --scan root exists and contains Go source files.\n")
		os.Exit(1)
	}

	if !opts.Quiet {
		if enhanced {
			fmt.Print(analyzer.RenderEnhancedReport(report, opts.Filter))
		} else {
			fmt.Print(analyzer.RenderSummaryTable(report, opts.Filter))
		}
	} else {
		printReportSummary(report, opts.Quiet)
	}

	if report.Stats.ErrorCount > 0 {
		os.Exit(1)
	}
}

func handleAnalyze(args []string) {
	opts := parseScanArgs(args, false)

	if !opts.Quiet {
		fmt.Printf("🔍 Analyzing Go AST & action contracts in %v...\n", opts.Roots)
	}

	report, err := analyzer.AnalyzeAll(opts.Roots...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Analysis failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "💡 Verify every --scan root exists and contains Go source files.\n")
		os.Exit(1)
	}

	printReportSummary(report, opts.Quiet)

	if report.Stats.ErrorCount > 0 {
		os.Exit(1)
	}
}

func printReportSummary(report *analyzer.Report, quiet bool) {
	if quiet {
		renderDiagnostics(report)
		return
	}

	fmt.Printf("\n📊 Nexss AST System Report:\n")
	fmt.Printf("  ├── 📁 Scanned Go Files:  %d\n", report.ScannedFiles)
	fmt.Printf("  ├── ⚡ Discovered Actions: %d (Public: %d, Internal: %d, System: %d)\n",
		report.Stats.TotalActions, report.Stats.PublicActions, report.Stats.InternalActions, report.Stats.SystemActions)
	fmt.Printf("  └── 📦 Extracted Structs:  %d\n\n", report.Stats.TotalStructs)

	renderDiagnostics(report)
}

func renderDiagnostics(report *analyzer.Report) {
	if len(report.Diagnostics) == 0 {
		return
	}

	fmt.Println("🩺 Diagnostics:")
	for _, d := range report.Diagnostics {
		icon := "⚠️"
		if d.Severity == analyzer.SeverityError {
			icon = "❌"
		}

		fmt.Printf("  %s [%s] %s\n", icon, d.Rule, d.Message)

		if d.File != "" {
			if d.Line > 0 {
				fmt.Printf("      📍 %s:%d\n", d.File, d.Line)
			} else {
				fmt.Printf("      📍 %s\n", d.File)
			}
		}

		if d.Suggestion != "" {
			fmt.Printf("      💡 %s\n", d.Suggestion)
		}
	}
	fmt.Println()
}

func handleStudioScaffold(args []string) {
	force := false
	quiet := false
	for _, a := range args {
		switch a {
		case "--force", "-f":
			force = true
		case "--quiet", "-q":
			quiet = true
		}
	}

	target, err := deploygen.ScaffoldStudio(".", force)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Studio scaffold error: %v\n", err)
		fmt.Fprintf(os.Stderr, "💡 Verify ui/src/routes/ is writable, or pass --force to overwrite.\n")
		os.Exit(1)
	}

	if quiet {
		return
	}
	fmt.Printf("✨ Studio written to %s\n", target)
	fmt.Println("👉 Available at http://localhost:8080/studio once the UI is rebuilt:")
	fmt.Println("   cd ui && corepack pnpm run build")
}

func printHelp() {
	fmt.Print(`
⚡ Nexss Operations CLI (nexssp)

	Usage:
		  nexssp ui:init [--force]        Scaffold SvelteKit 3 + Svelte 5 from this project
		  nexssp backend [flags] [root...] Generate a Flow/Kernel HTTP backend from action routes
	  nexssp sdk [flags] [root...]    Generate TypeScript + Zod SDK from action contracts
	  nexssp sdk api [apiDir] [--scan <dir>...]
	                                  Generate only client.ts, types.ts, endpoints.ts, and index.ts
	  nexssp actions [flags] [root...] [filter]
	                                  List all registered actions and HTTP routes
	nexssp analyze [flags] [root...]
                                  Run static AST analysis & contract diagnostics
	nexssp studio [file.nflow]      Open a local Svelte Studio for a Flow file
	nexssp ui:studio [--force]      Emit the app-embedded Studio SPA route
	nexssp help                     Show this help

	Scan flags (backend / sdk / actions / analyze):
  --scan <dir>   Add a source root to the AST scan (repeatable)
  --scan=<dir>   Same as above, single-token form
  <dir>          Positional root (also repeatable)
  <file.go>      Single-file root

	Output flags:
	  --quiet / -q   Suppress everything except diagnostics and errors
	  backend: --out <file.go> --package <name> --function <name>
	           --exclude-pipeline <name>, --exclude-action <name> (repeatable)
	           --force to overwrite

SDK flags:
	--force / -f   Bypass regular diagnostics and overwrite write-once files.
	                 Unsafe public TypeScript contracts are never force-generated.
		--prune-generated  Remove stale generated files only under _gen/stores/{entities,forms,streams,system}
		--exclude-pipeline <name>  Omit an NFlow pipeline from SDK output (repeatable)
		--api-only  Legacy full-SDK mode: limit DTOs to routed public actions
		sdk api [apiDir]  Write only the API client files directly into apiDir (default: ui/src/lib/_gen/api)
		<uiLibDir>     Output root (default: ui/src/lib)
		<out.ts>       Legacy: use its containing directory as the output root

Generated tree (under <uiLibDir>):
  _gen/api/           Always overwritten — client, types, endpoints
  _gen/stores/        Always overwritten — entities/forms/streams/system
  _gen/stores/runtime Write-once — editable entity/form/stream primitives
  _gen/index.ts       Always overwritten
  api.ts              Write-once — merge barrel
  custom/             Write-once — your extension points
  stores/index.ts     Write-once — merge barrel for app + generated stores

Write-once behavior:
  • First run: file is created.
  • Template changes on the Go side: file is refreshed automatically.
  • You edit the file: it is preserved on every subsequent run.
  • 'nexssp sdk --force': overrides the preservation and rewrites.

		Examples:
	  nexssp backend --scan . --out app/backend_server_gen.go --package app
	  nexssp backend --scan . --exclude-pipeline chaos_retry_probe
	  nexssp backend --scan . --exclude-action catalog.rebuild
	  nexssp sdk
	  nexssp sdk api ./src/lib/_gen/api
	  nexssp sdk --scan . --scan ../ai/agent/docker
  nexssp sdk --force
  nexssp sdk -q --scan=. --scan=../ai/agent/sandbox
	  nexssp actions --enhanced .
  nexssp analyze -q --scan . --scan ../ai/agent/docker

💡 Contract rule: a struct that crosses a module boundary into a wire
   format must exist as a struct in exactly one module. Everywhere else,
   use a type alias:  type Foo = otherpkg.Foo
`)
}
