package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Jan/git-backtrack/internal/gitops"
	"github.com/Jan/git-backtrack/internal/tui"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	repoPath := flag.String("path", ".", "path to git repository")
	showVersion := flag.Bool("version", false, "show version information")
	debugMode := flag.Bool("debug", false, "debug mode - test git operations without TUI")
	flag.Parse()

	if *showVersion {
		fmt.Printf("git-backtrack %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
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

	model := tui.NewModel(repo)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}
