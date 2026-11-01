package main

import "testing"

func TestParseScanArgsExcludePipelines(t *testing.T) {
	opts := parseScanArgs([]string{"--exclude-pipeline", "chaos_retry_probe", "--exclude-pipeline=pipeline.experimental"}, true)
	if len(opts.ExcludePipelines) != 2 || opts.ExcludePipelines[0] != "chaos_retry_probe" || opts.ExcludePipelines[1] != "pipeline.experimental" {
		t.Fatalf("unexpected exclusions: %#v", opts.ExcludePipelines)
	}
}

func TestParseScanArgsAPIOnly(t *testing.T) {
	if opts := parseScanArgs([]string{"--api-only"}, true); !opts.APIOnly {
		t.Fatal("--api-only was not recorded for the SDK command")
	}
	if opts := parseScanArgs([]string{"--api-only"}, false); opts.APIOnly {
		t.Fatal("--api-only should only apply to the SDK command")
	}
}
