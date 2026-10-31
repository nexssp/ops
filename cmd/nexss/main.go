// Command nexss runs .nflow files through the supported Flow CLI and its
// canonical native bundle set. Application-specific libraries remain the
// responsibility of the application's own runner or @require declarations.
package main

import (
	"os"

	flowcli "github.com/nexssp/flow/cli"
	"github.com/nexssp/flow/native"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "flow" {
		args[0] = "run"
	}
	os.Exit(flowcli.RunWithBundles(args, native.Bundles()))
}
