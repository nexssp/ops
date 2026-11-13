package main

import (
	"reflect"
	"testing"
)

func TestParseSDKAPIArgs(t *testing.T) {
	outputDir, scanArgs := parseSDKAPIArgs([]string{
		"./src/lib/_gen/api",
		"--scan", ".",
		"--exclude-pipeline", "legacy_debug",
		"--force",
	})
	if outputDir != "./src/lib/_gen/api" {
		t.Fatalf("output directory = %q, want ./src/lib/_gen/api", outputDir)
	}
	want := []string{"--scan", ".", "--exclude-pipeline", "legacy_debug", "--force"}
	if !reflect.DeepEqual(scanArgs, want) {
		t.Fatalf("scan args = %#v, want %#v", scanArgs, want)
	}
}

func TestParseSDKAPIArgsWithoutOutputFolder(t *testing.T) {
	outputDir, scanArgs := parseSDKAPIArgs([]string{"--scan=./src", "--quiet"})
	if outputDir != "" {
		t.Fatalf("output directory = %q, want default", outputDir)
	}
	want := []string{"--scan=./src", "--quiet"}
	if !reflect.DeepEqual(scanArgs, want) {
		t.Fatalf("scan args = %#v, want %#v", scanArgs, want)
	}
}
