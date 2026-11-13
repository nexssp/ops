package main

import "strings"

// parseSDKAPIArgs treats the first non-flag argument as the API output folder.
// Additional source roots must use --scan so the short command stays unambiguous.
func parseSDKAPIArgs(args []string) (outputDir string, scanArgs []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--scan" || arg == "--exclude-pipeline":
			scanArgs = append(scanArgs, arg)
			if i+1 < len(args) {
				i++
				scanArgs = append(scanArgs, args[i])
			}
		case strings.HasPrefix(arg, "--scan=") || strings.HasPrefix(arg, "--exclude-pipeline=") || strings.HasPrefix(arg, "-"):
			scanArgs = append(scanArgs, arg)
		default:
			if outputDir == "" {
				outputDir = arg
			} else {
				scanArgs = append(scanArgs, arg)
			}
		}
	}
	return outputDir, scanArgs
}
