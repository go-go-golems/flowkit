package flowkit

import (
	"embed"

	"github.com/go-go-golems/glazed/pkg/help"
)

// DocFS embeds Flowkit's help/documentation markdown sections (the docs/
// directory). It is loaded into the Glazed help system during CLI
// initialization so that `flowkit help` and `flowkit help export` work.
//
// See cmd/flowkit/main.go.
//
//go:embed docs
var DocFS embed.FS

// AddDocToHelpSystem loads Flowkit's embedded help sections into hs.
func AddDocToHelpSystem(hs *help.HelpSystem) error {
	return hs.LoadSectionsFromFS(DocFS, "docs")
}
