package main

import (
	"fmt"
	"os"

	flowcli "github.com/nexssp/flow/cli"
	"github.com/nexssp/flow/native"
)

func handleFlowRun(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, `Usage: nexssp flow <path.nflow> [payload] [flags]`)
		os.Exit(2)
	}
	os.Exit(flowcli.RunWithBundles(append([]string{"run"}, args...), native.Bundles()))
}
