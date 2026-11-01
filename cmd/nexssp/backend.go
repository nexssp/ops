package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexssp/ops/analyzer"
	"github.com/nexssp/ops/backendgen"
)

type backendGenOptions struct {
	Roots            []string
	Output           string
	PackageName      string
	FunctionName     string
	ExcludePipelines []string
	ExcludeActions   []string
	Force            bool
	Quiet            bool
}

var errBackendHelp = errors.New("backend help requested")

func parseBackendGenArgs(args []string) (backendGenOptions, error) {
	opts := backendGenOptions{
		Output:       "backend_server_gen.go",
		PackageName:  "main",
		FunctionName: "RunGeneratedBackendServer",
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		value := func(flag string) (string, error) {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return "", fmt.Errorf("%s requires a value", flag)
			}
			i++
			return args[i], nil
		}
		switch {
		case arg == "--help" || arg == "-h":
			return opts, errBackendHelp
		case arg == "--force" || arg == "-f":
			opts.Force = true
		case arg == "--quiet" || arg == "-q":
			opts.Quiet = true
		case arg == "--scan":
			v, err := value(arg)
			if err != nil {
				return opts, err
			}
			opts.Roots = append(opts.Roots, v)
		case strings.HasPrefix(arg, "--scan="):
			opts.Roots = append(opts.Roots, strings.TrimPrefix(arg, "--scan="))
		case arg == "--out":
			v, err := value(arg)
			if err != nil {
				return opts, err
			}
			opts.Output = v
		case strings.HasPrefix(arg, "--out="):
			opts.Output = strings.TrimPrefix(arg, "--out=")
		case arg == "--package":
			v, err := value(arg)
			if err != nil {
				return opts, err
			}
			opts.PackageName = v
		case strings.HasPrefix(arg, "--package="):
			opts.PackageName = strings.TrimPrefix(arg, "--package=")
		case arg == "--function":
			v, err := value(arg)
			if err != nil {
				return opts, err
			}
			opts.FunctionName = v
		case strings.HasPrefix(arg, "--function="):
			opts.FunctionName = strings.TrimPrefix(arg, "--function=")
		case arg == "--exclude-pipeline":
			v, err := value(arg)
			if err != nil {
				return opts, err
			}
			opts.ExcludePipelines = append(opts.ExcludePipelines, v)
		case strings.HasPrefix(arg, "--exclude-pipeline="):
			opts.ExcludePipelines = append(opts.ExcludePipelines, strings.TrimPrefix(arg, "--exclude-pipeline="))
		case arg == "--exclude-action":
			v, err := value(arg)
			if err != nil {
				return opts, err
			}
			opts.ExcludeActions = append(opts.ExcludeActions, v)
		case strings.HasPrefix(arg, "--exclude-action="):
			opts.ExcludeActions = append(opts.ExcludeActions, strings.TrimPrefix(arg, "--exclude-action="))
		case strings.HasPrefix(arg, "-"):
			return opts, fmt.Errorf("unknown backend flag %q", arg)
		default:
			info, err := os.Stat(arg)
			if err != nil {
				return opts, fmt.Errorf("unknown positional argument %q; pass scan roots with --scan", arg)
			}
			if info.IsDir() || strings.HasSuffix(arg, ".go") || strings.HasSuffix(arg, ".nflow") {
				opts.Roots = append(opts.Roots, arg)
			} else {
				return opts, fmt.Errorf("unsupported scan input %q", arg)
			}
		}
	}
	if len(opts.Roots) == 0 {
		opts.Roots = []string{"."}
	}
	if !strings.HasSuffix(opts.Output, ".go") {
		return opts, fmt.Errorf("backend output must be a .go file: %q", opts.Output)
	}
	return opts, nil
}

func handleBackendGen(args []string) {
	opts, err := parseBackendGenArgs(args)
	if errors.Is(err, errBackendHelp) {
		printBackendHelp()
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "backend: %v\n", err)
		printBackendHelp()
		os.Exit(2)
	}

	report, err := analyzer.AnalyzeAll(opts.Roots...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backend: analysis failed: %v\n", err)
		os.Exit(1)
	}
	printReportSummary(report, opts.Quiet)
	if report.Stats.ErrorCount > 0 {
		fmt.Fprintf(os.Stderr, "backend: generation aborted: %d analyzer error(s)\n", report.Stats.ErrorCount)
		os.Exit(1)
	}

	source, err := backendgen.GenerateFromActions(report.Actions, report.FlowHTTPRoutes, backendgen.Config{
		PackageName:      opts.PackageName,
		FunctionName:     opts.FunctionName,
		ExcludePipelines: opts.ExcludePipelines,
		ExcludeActions:   opts.ExcludeActions,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "backend: generation failed: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stat(opts.Output); err == nil && !opts.Force {
		fmt.Fprintf(os.Stderr, "backend: refusing to overwrite %q; pass --force to replace it\n", opts.Output)
		os.Exit(1)
	} else if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "backend: inspect output %q: %v\n", opts.Output, err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(opts.Output), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "backend: create output directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(opts.Output, source, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "backend: write %q: %v\n", opts.Output, err)
		os.Exit(1)
	}
	if !opts.Quiet {
		fmt.Printf("Generated Flow/Kernel backend bootstrap: %s (%d mounted HTTP action routes)\n", opts.Output, selectedBackendRouteCount(report.Actions, report.FlowHTTPRoutes, opts.ExcludePipelines, opts.ExcludeActions))
		fmt.Println("The generated server accepts application bundles and an optional UI mux hook; it binds to loopback only by default.")
	}
}

func selectedBackendRouteCount(actions []analyzer.ActionContract, flowRoutes []analyzer.FlowHTTPRoute, excludedPipelines, excludedActions []string) int {
	pipelines := make(map[string]bool, len(excludedPipelines))
	for _, name := range excludedPipelines {
		name = strings.TrimSpace(name)
		if name != "" && !strings.HasPrefix(name, "pipeline.") {
			name = "pipeline." + name
		}
		pipelines[name] = true
	}
	actionNames := make(map[string]bool, len(excludedActions))
	for _, name := range excludedActions {
		actionNames[strings.TrimSpace(name)] = true
	}
	count := 0
	for _, action := range actions {
		if action.RouteSource == "nflow" || action.Method == "" || action.Path == "" {
			continue
		}
		name := strings.TrimSpace(action.Name)
		if actionNames[name] {
			continue
		}
		count++
	}
	for _, route := range flowRoutes {
		name := strings.TrimSpace(route.Pipeline)
		if name != "" && !strings.HasPrefix(name, "pipeline.") {
			name = "pipeline." + name
		}
		if !pipelines[name] {
			count++
		}
	}
	return count
}

func printBackendHelp() {
	fmt.Fprintln(os.Stderr, `Usage: nexssp backend [flags] [root...]

Generate a self-contained Flow/Kernel HTTP server bootstrap from explicitly routed actions and NFlow pipelines.

Flags:
  --scan <dir|file>             Add a scan root (repeatable; defaults to .)
  --out <file.go>               Output path (default: backend_server_gen.go)
  --package <name>              Generated Go package (default: main)
  --function <name>             Exported server function (default: RunGeneratedBackendServer)
  --exclude-pipeline <name>     Do not mount a route pipeline (repeatable; accepts raw or pipeline.* name)
  --exclude-action <name>       Do not mount a Go action route (repeatable)
  --force                       Overwrite an existing output file
  --quiet / -q                  Suppress summary output except diagnostics/errors
  --help                        Show this help

Example:
  nexssp backend --scan . --out app/backend_server_gen.go --package app --exclude-pipeline chaos_retry_probe

The generated function accepts app compiler bundles, an optional http.ServeMux configurator, and middleware.
Only routes explicitly declared by Go actions or NFlow pipelines are mounted; OPS does not invent routes or action implementations.
For NFlow routes, a bundle that already registers :route is reused; otherwise OPS emits a safe /api/* route modifier.
Excluded pipelines are still compiled from the workflow but are not mounted as HTTP endpoints.`)
}
