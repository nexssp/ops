package analyzer_test

import (
	"strings"
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func TestNFlowTopLevelArrayResponseSchemasGenerateTypedAPI(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "routes.nflow", `@schema Widget {
  Name string `+"`json:\"name\" validate:\"required\"`"+`
}
@pipeline strings_list :route="GET /api/strings" :response-schema=[]string
  runtime.noop
@end
@pipeline widgets_list :route="GET /api/widgets" :response-schema=[]Widget
  runtime.noop
@end
`)

	report, err := analyzer.Analyze(root)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if report.Stats.ErrorCount != 0 {
		t.Fatalf("top-level array schemas were not accepted: %+v", report.Diagnostics)
	}
	files := generatedByName(analyzer.GenerateTypeScriptAPI(report))
	if len(files) != 4 {
		t.Fatalf("generated API files = %#v, want four flat API modules", files)
	}
	for _, name := range []string{"client.ts", "types.ts", "endpoints.ts", "index.ts"} {
		if _, ok := files[name]; !ok {
			t.Errorf("missing flat API file %q; got %#v", name, files)
		}
	}
	endpoints := string(files["endpoints.ts"])
	if !strings.Contains(endpoints, "stringsList: (): Promise<string[]> ") {
		t.Errorf("primitive array response has wrong TypeScript shape:\n%s", endpoints)
	}
	if !strings.Contains(endpoints, "widgetsList: (): Promise<Types.Widget[]> ") {
		t.Errorf("schema array response has wrong TypeScript shape:\n%s", endpoints)
	}
	if !strings.Contains(string(files["types.ts"]), "export interface Widget {") {
		t.Errorf("schema array element type is missing from generated types:\n%s", files["types.ts"])
	}
}
