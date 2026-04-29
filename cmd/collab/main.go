// Command collab is the CLI entry point for the collab-ai project. The
// binary is named `collab` for ergonomics; the project, module and brand
// remain "collab-ai". See docs/08-cli.md.
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
		Use:   "collab",
		Short: "Multiplayer for AI coding agents — peer-to-peer over Gensyn Axl",
		Run: func(cmd *cobra.Command, _ []string) {
			printIntro(os.Stderr)
		},
	}
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		if cmd == root {
			printIntro(os.Stderr)
			return
		}
		_ = cmd.Usage()
	})

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

// printIntro is what `collab` (no args) and `collab help` show. Cobra's
// default usage page is verbose; this is a curated landing for the verbs
// we actually want users to reach for.
func printIntro(w *os.File) {
	c := newPalette(w)
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.bold("collab")+c.dim("  multiplayer for AI coding agents  ")+c.dim("v"+protocol.Version))
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  USAGE"))
	fmt.Fprintln(w, "    collab "+c.accent("create")+c.dim(" [your-name]            ")+"start a new pairing session")
	fmt.Fprintln(w, "    collab "+c.accent("connect")+c.dim(" <invite> [your-name]  ")+"join a session by invite code")
	fmt.Fprintln(w, "    collab "+c.accent("status")+c.dim("                         ")+"see who's in the running session")
	fmt.Fprintln(w, "    collab "+c.accent("export")+c.dim(" --out session.json      ")+"dump the conversation log")
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  EXAMPLES"))
	fmt.Fprintln(w, "    "+c.dim("$")+" collab create "+c.accent("alice"))
	fmt.Fprintln(w, "      "+c.dim("→ session ready · share the invite that prints"))
	fmt.Fprintln(w, "    "+c.dim("$")+" collab connect "+c.accent("COLLAB-... bob"))
	fmt.Fprintln(w, "      "+c.dim("→ joins the session as 'bob'"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  MORE"))
	fmt.Fprintln(w, "    collab help "+c.dim("<command>")+"     "+c.dim("detailed help for a single verb"))
	fmt.Fprintln(w, "    collab --help              "+c.dim("full list of every verb (including low-level escape hatches)"))
	fmt.Fprintln(w, "    collab version             "+c.dim("build info"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  https://github.com/Aman035/collab-ai"))
	fmt.Fprintln(w)
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
