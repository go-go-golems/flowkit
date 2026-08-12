package flowkit_test

import (
	"os"
	"testing"

	"github.com/go-go-golems/glazed/pkg/help"
)

func TestDeveloperGuideLoadsAsGlazedHelpEntry(t *testing.T) {
	helpSystem := help.NewHelpSystem()
	if err := helpSystem.LoadSectionsFromFS(os.DirFS("docs"), "."); err != nil {
		t.Fatalf("load Glazed help entries: %v", err)
	}
	section, err := helpSystem.GetSectionWithSlug("flowkit-developer-guide")
	if err != nil {
		t.Fatalf("find developer guide by slug: %v", err)
	}
	if section.Title != "Flowkit Developer Guide" {
		t.Fatalf("guide title = %q", section.Title)
	}
}
