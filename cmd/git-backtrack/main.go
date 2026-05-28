package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Jan/git-backtrack/internal/gitops"
	"github.com/Jan/git-backtrack/internal/mcp"
	"github.com/Jan/git-backtrack/internal/tool"
	"github.com/Jan/git-backtrack/internal/tui"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		if err := mcp.Serve(os.Stdin, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if len(os.Args) > 1 && tool.IsCommand(os.Args[1]) {
		os.Exit(tool.Run(os.Args[1:], os.Stdout, os.Stderr))
	}

	repoPath := flag.String("path", ".", "path to git repository")
	showVersion := flag.Bool("version", false, "show version information")
	debugMode := flag.Bool("debug", false, "debug mode - test git operations without TUI")
	graphView := flag.Bool("graph", false, "show graph rendering in the overview")
	graphOrderValue := flag.String("graph-order", "topo", "graph order: topo, date, author-date, or first-parent")
	cleanView := flag.Bool("clean", false, "disable graph and aligned column spacing in the overview (default)")
	plainView := flag.Bool("plain", false, "disable graph rendering but keep aligned column spacing")
	timezoneView := flag.Bool("timezone", false, "show timezone offsets in commit timestamps")
	showTimezone := flag.Bool("show-timezone", false, "alias for --timezone")
	emailView := flag.Bool("email", false, "show author emails in the overview")
	showEmail := flag.Bool("show-email", false, "alias for --email")
	lineDiffs := flag.Bool("line-diffs", false, "show +N/-N line diff stats in the overview")
	showLineDiffs := flag.Bool("show-line-diffs", false, "alias for --line-diffs")
	flag.Parse()

	if *showVersion {
		fmt.Printf("git-backtrack %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	graphOrder, err := gitops.ParseGraphOrder(*graphOrderValue)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}

	repo, err := gitops.Open(*repoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening repository: %v\n", err)
		os.Exit(1)
	}

	if *debugMode {
		fmt.Println("Debug mode - testing git operations...")
		commits, err := repo.ListAllCommits()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing commits: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Found %d commits\n", len(commits))
		for i, c := range commits {
			if i >= 10 {
				fmt.Println("... (showing first 10)")
				break
			}
			fmt.Printf("  %s %s <%s> %s\n", c.ShortHash, c.AuthorName, c.AuthorEmail, c.AuthorDate.Format("2006-01-02"))
		}

		sigCfg, err := repo.GetSigningConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "signing config error: %v\n", err)
		} else {
			fmt.Printf("SignCommits: %v\nSigningKey: %q\nKeyType: %q\n", sigCfg.SignCommits, sigCfg.SigningKey, sigCfg.KeyType)
		}

		if len(commits) > 0 {
			newHash, err := repo.SignCommit(commits[0].Hash)
			if err != nil {
				fmt.Fprintf(os.Stderr, "sign error: %v\n", err)
			} else {
				fmt.Printf("original: %s\nsigned:   %s\n", commits[0].Hash.String()[:7], newHash.String()[:7])
			}
		}
		fmt.Println("Debug mode completed successfully.")
		os.Exit(0)
	}

	startCleanView := !*graphView && !*plainView || *cleanView
	startPlainView := *plainView && !*cleanView
	startLineDiffs := *lineDiffs || *showLineDiffs
	model := tui.NewModelWithOptions(repo, tui.Options{CleanView: startCleanView, PlainView: startPlainView, ShowTimezone: *timezoneView || *showTimezone, ShowEmail: *emailView || *showEmail, HideLineDiffs: !startLineDiffs, GraphOrder: graphOrder})
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}
