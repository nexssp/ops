package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexssp/ops/analyzer"
)

func loadSDKContractManifest(path string) (analyzer.ContractManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return analyzer.ContractManifest{}, fmt.Errorf("open contract manifest %q: %w", path, err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest analyzer.ContractManifest
	if err := decoder.Decode(&manifest); err != nil {
		return analyzer.ContractManifest{}, fmt.Errorf("decode contract manifest %q: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return analyzer.ContractManifest{}, fmt.Errorf("decode contract manifest %q: multiple JSON values", path)
		}
		return analyzer.ContractManifest{}, fmt.Errorf("decode contract manifest %q: %w", path, err)
	}
	return manifest, nil
}

func writeAPICompatibilityFacade(facadePath, apiOutputDir string) error {
	if !strings.EqualFold(filepath.Ext(facadePath), ".ts") {
		return fmt.Errorf("API compatibility facade path %q must end in .ts", facadePath)
	}
	absoluteFacade, err := filepath.Abs(facadePath)
	if err != nil {
		return fmt.Errorf("resolve facade path %q: %w", facadePath, err)
	}
	absoluteOutput, err := filepath.Abs(apiOutputDir)
	if err != nil {
		return fmt.Errorf("resolve API output directory %q: %w", apiOutputDir, err)
	}

	indexImport, err := relativeTSImport(filepath.Dir(absoluteFacade), filepath.Join(absoluteOutput, "index.ts"))
	if err != nil {
		return err
	}
	endpointsImport, err := relativeTSImport(filepath.Dir(absoluteFacade), filepath.Join(absoluteOutput, "endpoints.ts"))
	if err != nil {
		return err
	}

	content := analyzer.GenerateTypeScriptAPICompatibilityFacade(indexImport, endpointsImport)
	if err := os.MkdirAll(filepath.Dir(absoluteFacade), 0o755); err != nil {
		return fmt.Errorf("create facade directory %q: %w", filepath.Dir(absoluteFacade), err)
	}
	if err := os.WriteFile(absoluteFacade, content, 0o600); err != nil {
		return fmt.Errorf("write API compatibility facade %q: %w", absoluteFacade, err)
	}
	return nil
}

func relativeTSImport(fromDir, targetFile string) (string, error) {
	relative, err := filepath.Rel(fromDir, targetFile)
	if err != nil {
		return "", fmt.Errorf("compute TypeScript import from %q to %q: %w", fromDir, targetFile, err)
	}
	modulePath := filepath.ToSlash(strings.TrimSuffix(relative, filepath.Ext(relative)))
	if !strings.HasPrefix(modulePath, ".") {
		modulePath = "./" + modulePath
	}
	return modulePath + ".js", nil
}
