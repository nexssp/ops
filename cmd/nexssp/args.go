package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// scanArgs is the shared argument shape for every subcommand that walks the
// AST (sdk, actions, analyze).
type scanArgs struct {
	Roots              []string
	UILibDir           string   // SDK only: output root under ui/
	Filter             string   // action-name filter for `actions`/`analyze`
	Force              bool     // emit even with errors
	Quiet              bool     // suppress all output except diagnostics + failures
	PruneGenerated     bool     // remove stale files from generator-owned store directories
	ExcludePipelines   []string // SDK only: omit runtime-conditional NFlow pipelines
	APIOnly            bool     // SDK only: emit DTOs reachable from routed public actions
	ContractManifest   string   // SDK only: explicit request/response wire types
	APISurfaceManifest string   // SDK API only: explicit public route/contract surface
	APIFacade          string   // sdk api only: optional stable import facade file
	ParseError         string   // invalid option syntax detected while parsing
}

// parseScanArgs understands, for any AST-scanning subcommand:
//
//	--scan <dir>   --scan=<dir>   (repeatable)
//	<dir>          <file.go>      (positional roots, repeatable)
//	<filter>                      (only if it doesn't exist on disk)
//	<uiLibDir>                    (SDK only: a ui/... or lib path)
//	<out.ts>                      (SDK legacy: parent dir becomes uiLibDir)
//	--force / -f                  emit even with errors
//	--quiet / -q                  suppress everything except diagnostics/errors
//	--exclude-pipeline <name>     SDK only: omit an NFlow pipeline (repeatable)
//	--api-only                    SDK only: omit unrelated convention-exposed DTOs
//	--contract-manifest <file>    SDK only: fill explicit wire-contract types
//	--api-manifest <file>         sdk api only: select the complete public route surface
//	--api-facade <file.ts>        sdk api only: emit a single-file import facade
//
// When no root is provided, the parser defaults to "." — matching the
// historical behaviour of every subcommand.
func parseScanArgs(args []string, isSDK bool) scanArgs {
	var out scanArgs

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--api-only" && isSDK:
			out.APIOnly = true

		case a == "--contract-manifest" && isSDK:
			if i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "-") {
				setMissingOptionValue(&out, a)
			} else {
				i++
				out.ContractManifest = args[i]
			}

		case strings.HasPrefix(a, "--contract-manifest=") && isSDK:
			out.ContractManifest = strings.TrimPrefix(a, "--contract-manifest=")
			if out.ContractManifest == "" {
				setMissingOptionValue(&out, "--contract-manifest")
			}

		case a == "--api-manifest" && isSDK:
			if i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "-") {
				setMissingOptionValue(&out, a)
			} else {
				i++
				out.APISurfaceManifest = args[i]
			}

		case strings.HasPrefix(a, "--api-manifest=") && isSDK:
			out.APISurfaceManifest = strings.TrimPrefix(a, "--api-manifest=")
			if out.APISurfaceManifest == "" {
				setMissingOptionValue(&out, "--api-manifest")
			}

		case a == "--api-facade" && isSDK:
			if i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "-") {
				setMissingOptionValue(&out, a)
			} else {
				i++
				out.APIFacade = args[i]
			}

		case strings.HasPrefix(a, "--api-facade=") && isSDK:
			out.APIFacade = strings.TrimPrefix(a, "--api-facade=")
			if out.APIFacade == "" {
				setMissingOptionValue(&out, "--api-facade")
			}

		case a == "--force" || a == "-f":
			out.Force = true

		case a == "--quiet" || a == "-q":
			out.Quiet = true

		case a == "--prune-generated":
			out.PruneGenerated = true

		case a == "--exclude-pipeline":
			if i+1 < len(args) {
				i++
				out.ExcludePipelines = append(out.ExcludePipelines, args[i])
			}

		case strings.HasPrefix(a, "--exclude-pipeline="):
			out.ExcludePipelines = append(out.ExcludePipelines, strings.TrimPrefix(a, "--exclude-pipeline="))

		case a == "--scan":
			if i+1 < len(args) {
				i++
				out.Roots = append(out.Roots, args[i])
			}

		case strings.HasPrefix(a, "--scan="):
			out.Roots = append(out.Roots, strings.TrimPrefix(a, "--scan="))

		case strings.HasPrefix(a, "-"):
			// Unknown flag — ignore. Callers that need to detect specific
			// flags (e.g. --enhanced) scan args themselves before calling.

		default:
			if info, err := os.Stat(a); err == nil && (info.IsDir() || strings.HasSuffix(a, ".go")) {
				out.Roots = append(out.Roots, a)
				continue
			}

			if isSDK {
				switch {
				case strings.HasSuffix(a, ".ts"):
					out.UILibDir = filepath.Dir(a)
					continue
				case strings.Contains(a, "ui/") || strings.Contains(a, "lib"):
					out.UILibDir = a
					continue
				}
			}

			// Doesn't exist on disk and isn't an SDK path → action filter.
			out.Filter = a
		}
	}

	if len(out.Roots) == 0 {
		out.Roots = []string{"."}
	}
	return out
}

func setMissingOptionValue(args *scanArgs, option string) {
	if args.ParseError == "" {
		args.ParseError = fmt.Sprintf("missing value for %s", option)
	}
}
