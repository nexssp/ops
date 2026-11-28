package core

import (
	"strings"
	"testing"
)

func TestConfiguredAPIClientOutputs(t *testing.T) {
	cfg := &APIClientConfig{Transport: "single-fetcher", ErrorKinds: []string{"Oops", "Bad"}, EndpointMetadataFields: []string{"name", "path"}, FieldMetadataFields: []string{"key", "required"}, FacadeExportName: "generatedApi", FactoryExportName: "createGeneratedApi", PathParameterEncoding: "none"}
	report := &Report{APIClientConfig: cfg, Actions: []ActionContract{{Name: "files.get", Scope: "public", Method: "GET", Path: "/files/{id}", ResTypeName: "[]byte", BlobResponse: true, ReqTypeName: "struct{}", SDKMethodName: "getFile", ReqFields: []FieldContract{{Key: "id", Name: "ID", Type: "string", Required: true}}}}}
	client := string(generateClientFile(cfg))
	endpoints := string(generateEndpointsFile(report, map[string]StructDefinition{}, map[string]bool{}))
	for _, want := range []string{"Oops = \"Oops\"", "responseMode?: \"json\" | \"blob\"", "responseMode === \"blob\""} {
		if !strings.Contains(client, want) {
			t.Errorf("client missing %s", want)
		}
	}
	for _, want := range []string{"export interface EndpointMeta { name: string; path: string; }", "export interface FieldMeta { key: string; required: boolean; }", "export { api as generatedApi };", "export const createGeneratedApi = () => api;", "resolvePathUnencoded('/files/{id}', req)", "responseMode: 'blob'"} {
		if !strings.Contains(endpoints, want) {
			t.Errorf("endpoints missing %s\\n%s", want, endpoints)
		}
	}
}
