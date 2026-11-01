package analyzer_test

import (
	"strings"
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func TestCodegen_ValidationRulesFlowToZod(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "users/types.go", `package users

type CreateReq struct {
	Email    string   `+"`json:\"email\" validate:\"required,email\"`"+`
	Name     string   `+"`json:\"name\"  validate:\"required,min=2,max=64\"`"+`
	Age      int      `+"`json:\"age\"   validate:\"required,gte=18,lte=120\"`"+`
	Budget   int64    `+"`json:\"budget\" validate:\"omitempty,gte=1,lte=500\"`"+`
	Country  string   `+"`json:\"country\" validate:\"required,len=2\"`"+`
	Tier     string   `+"`json:\"tier\"  validate:\"required,oneof=free pro enterprise\"`"+`
	Referrer string   `+"`json:\"referrer\" validate:\"omitempty,url\"`"+`
	Status   string   `+"`json:\"status\" validate:\"omitempty,oneofci=queued running\"`"+`
	Provider *string  `+"`json:\"provider,omitempty\" validate:\"omitempty,oneofci=openai ollama\"`"+`
	Tags     []string `+"`json:\"tags\"  validate:\"required,min=1,dive,min=2\"`"+`
}

type User struct {
	ID string `+"`json:\"id\"`"+`
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	ts := string(generatedByName(analyzer.GenerateTypeScriptSDK(report))["_gen/api/types.ts"])

	cases := []struct {
		field  string
		expect string
	}{
		{"email", `email: z.string().min(1).email()`},
		{"name", `name: z.string().min(2).max(64)`},
		{"age", `age: z.number().gte(18).lte(120)`},
		{"budget", `budget: z.number().refine(v => v === 0 || v >= 1).refine(v => v === 0 || v <= 500).optional()`},
		{"country", `country: z.string().length(2)`},
		{"tier", `tier: z.enum(["free", "pro", "enterprise"])`},
		{"referrer", `referrer: z.string().url().optional()`},
		{"status", `status: z.string().refine(v => v === "" || ["queued", "running"].some(choice => choice.toLowerCase() === v.toLowerCase())).optional()`},
		{"provider", `provider: z.string().refine(v => v === "" || ["openai", "ollama"].some(choice => choice.toLowerCase() === v.toLowerCase())).optional()`},
		{"tags", `tags: z.array(z.string().min(2)).min(1)`},
	}
	for _, c := range cases {
		if !strings.Contains(ts, c.expect) {
			t.Errorf("field %q: expected %q in types.ts\n--- generated ---\n%s", c.field, c.expect, ts)
		}
	}
}
