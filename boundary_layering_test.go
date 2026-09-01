package flowkit_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestExecutionAndInternalDoNotImportFlow locks the one-way package layering:
// the low-level execution package and everything beneath it (internal/*) must
// never depend on the higher-level flow package. The existing
// TestPackagesDoNotImportRagkit only guards the ragkit boundary; this test
// guards the internal layering so a refactor cannot silently invert it.
//
// Target layering (enforced):
//
//	flowkit/flow -> flowkit/execution -> flowkit/internal/*
//
// Forbidden: any execution/* or internal/* package importing flowkit/flow.
func TestExecutionAndInternalDoNotImportFlow(t *testing.T) {
	for _, pkg := range []string{"./execution/...", "./internal/..."} {
		out, err := exec.Command("go", "list", "-deps", pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
		}
		for _, dependency := range strings.Fields(string(out)) {
			if dependency == "github.com/go-go-golems/flowkit/flow" {
				t.Errorf("low-level package %s imports forbidden flowkit/flow; the layering must be flow -> execution -> internal", pkg)
			}
		}
	}
}
