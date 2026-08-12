package flowkit_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPackagesDoNotImportRagkit(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "./...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list dependencies: %v\n%s", err, out)
	}
	for _, dependency := range strings.Fields(string(out)) {
		if strings.HasPrefix(dependency, "github.com/go-go-golems/ragkit") {
			t.Errorf("Flowkit dependency tree includes forbidden ragkit import %q", dependency)
		}
	}
}
