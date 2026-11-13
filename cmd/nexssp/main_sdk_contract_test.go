package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const unsafeSDKFixtureEnv = "NEXSSP_UNSAFE_SDK_FIXTURE"

func TestSDKForceCannotBypassPublicContractValidation(t *testing.T) {
	if root := os.Getenv(unsafeSDKFixtureEnv); root != "" {
		if err := os.Chdir(root); err != nil {
			t.Fatal(err)
		}
		handleSDKGen([]string{"api", filepath.Join(root, "out", "api"), "--scan", ".", "--force", "--quiet"})
		return
	}

	root := t.TempDir()
	source := `package records
import (
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)
type CreateRequest struct { Metadata any ` + "`json:\"metadata\"`" + ` }
type CreateResponse struct { ID string ` + "`json:\"id\"`" + ` }
func Register() action.AnyAction {
	return action.New[CreateRequest, CreateResponse]("records.create", nil).
		Route(thttp.POST("/records")).Build()
}
`
	if err := os.WriteFile(filepath.Join(root, "actions.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestSDKForceCannotBypassPublicContractValidation$")
	cmd.Env = append(os.Environ(), unsafeSDKFixtureEnv+"="+root)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("SDK unexpectedly generated an unsafe contract; output:\n%s", output)
	}
	if !strings.Contains(string(output), "--force does not bypass this check") {
		t.Fatalf("SDK did not report the non-bypassable contract check; output:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(root, "out", "api", "types.ts")); !os.IsNotExist(err) {
		t.Fatalf("unsafe SDK output was written (stat error: %v)", err)
	}
}
