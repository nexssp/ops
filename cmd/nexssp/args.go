package main

import (
	"os"
	"path/filepath"
	"strings"
)

// scanArgs is the shared argument shape for every subcommand that walks the
// AST (sdk, actions, analyze).
type scanArgs struct {
	Roots    []string
	UILibDir string // SDK only: output root under ui/
	Filter   string // action-name filter for `actions`/`analyze`
	Force    bool   // emit even with errors
	Quiet    bool   // suppress all output except diagnostics + failures
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
//
// When no root is provided, the parser defaults to "." — matching the
// historical behaviour of every subcommand.
func parseScanArgs(args []string, isSDK bool) scanArgs {
	var out scanArgs

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--force" || a == "-f":
			out.Force = true

		case a == "--quiet" || a == "-q":
			out.Quiet = true

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
