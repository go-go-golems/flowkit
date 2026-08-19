package main

import (
	"fmt"
	"os"

	"github.com/go-go-golems/flowkit"
	"github.com/go-go-golems/glazed/pkg/cmds/logging"
	"github.com/go-go-golems/glazed/pkg/help"
	help_cmd "github.com/go-go-golems/glazed/pkg/help/cmd"
	"github.com/spf13/cobra"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "flowkit",
	Short: "Flowkit — bounded, cache-aware, policy-controlled Go data flows",
	Long: `Flowkit executes deterministic work over collections while preserving input
order. This CLI hosts the Flowkit help system and documentation so that help
sections can be inspected and exported (e.g. for docsctl publishing).

The reusable libraries live in the execution and flow packages; this binary is
intentionally minimal and wires up the Glazed help system.`,
	Version: version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return logging.InitLoggerFromCobra(cmd)
	},
}

func main() {
	if err := logging.AddLoggingSectionToRootCommand(rootCmd, "flowkit"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	helpSystem := help.NewHelpSystem()
	if err := flowkit.AddDocToHelpSystem(helpSystem); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	help_cmd.SetupCobraRootCommand(helpSystem, rootCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
