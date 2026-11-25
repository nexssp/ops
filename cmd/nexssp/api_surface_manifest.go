package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/nexssp/ops/analyzer"
)

func loadSDKAPISurfaceManifest(path string) (analyzer.APISurfaceManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return analyzer.APISurfaceManifest{}, fmt.Errorf("open API surface manifest %q: %w", path, err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest analyzer.APISurfaceManifest
	if err := decoder.Decode(&manifest); err != nil {
		return analyzer.APISurfaceManifest{}, fmt.Errorf("decode API surface manifest %q: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return analyzer.APISurfaceManifest{}, fmt.Errorf("decode API surface manifest %q: multiple JSON values", path)
		}
		return analyzer.APISurfaceManifest{}, fmt.Errorf("decode API surface manifest %q: %w", path, err)
	}
	return manifest, nil
}
