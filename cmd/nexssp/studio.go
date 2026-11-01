package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/nexssp/ops/flowstudio"
)

func handleFlowStudio(args []string) {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Usage: nexssp studio [file.nflow]")
		fmt.Println("Open a local Svelte Studio for one Flow file. With no path, opens app.nflow or the first .nflow file in the current directory.")
		return
	}
	filePath, err := resolveStudioFile(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Flow Studio error: %v\n", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := flowstudio.Run(ctx, filePath, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Flow Studio error: %v\n", err)
		os.Exit(1)
	}
}

func resolveStudioFile(args []string) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("usage: nexssp studio [file.nflow]")
	}
	if len(args) == 1 {
		if strings.HasPrefix(args[0], "-") {
			return "", fmt.Errorf("unsupported option %q; use `nexssp studio --help`", args[0])
		}
		return args[0], nil
	}
	if _, err := os.Stat("app.nflow"); err == nil {
		return "app.nflow", nil
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		return "", fmt.Errorf("read current directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".nflow" {
			return entry.Name(), nil
		}
	}
	return "", fmt.Errorf("no .nflow file found; pass one explicitly, for example `nexssp studio ./app.nflow`")
}
