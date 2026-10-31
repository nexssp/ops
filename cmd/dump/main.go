// nexssp/ops/cmd/dump/main.go
package main

import (
	"fmt"
	"os"

	"github.com/nexssp/ops/analyzer"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: dump <path>\n")
		os.Exit(1)
	}

	r, err := analyzer.Analyze(os.Args[1])
	if err != nil {
		panic(err)
	}

	s, ok := r.FindStruct("DockerExecRecord")
	fmt.Printf("found=%v package=%s file=%s qualified=%s\n", ok, s.Package, s.SourceFile, s.Qualified)
	for _, f := range s.Fields {
		fmt.Printf("  %-12s %-8s json=%q req=%v\n", f.Name, f.Type, f.Key, f.Required)
	}
}
