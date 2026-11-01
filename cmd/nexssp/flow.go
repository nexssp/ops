package main

import (
	"fmt"
	"os"

	flowcli "github.com/nexssp/flow/cli"
)

func handleFlowRun(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, `Usage: nexssp flow <path.nflow> [payload] [flags]`)
		os.Exit(2)
	}
	os.Exit(runFlowCLI(args))
}

func runFlowCLI(args []string) int {
	return flowcli.Run(append([]string{"run"}, args...))
}
