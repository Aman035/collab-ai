// Command collab-ai is the CLI entry point. See docs/08-cli.md.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Aman035/collab-ai/pkg/protocol"
)

// Build-time injected via goreleaser ldflags. Empty in dev builds.
var (
	commit string
	date   string
)

func main() {
	root := &cobra.Command{
		Use:   "collab-ai",
		Short: "Multiplayer for AI coding agents — peer-to-peer over Gensyn Axl",
	}
	root.AddCommand(createCmd())
	root.AddCommand(connectCmd())
	root.AddCommand(hostCmd())
	root.AddCommand(joinCmd())
	root.AddCommand(statusCmd())
	root.AddCommand(exportCmd())
	root.AddCommand(versionCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print collab-ai version",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Printf("collab-ai %s", protocol.Version)
			if commit != "" {
				fmt.Printf(" (%s", commit[:min(8, len(commit))])
				if date != "" {
					fmt.Printf(" · %s", date)
				}
				fmt.Print(")")
			}
			fmt.Println()
		},
	}
}
