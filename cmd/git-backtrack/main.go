package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

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
	if len(os.Args) > 1 && os.Args[1] == "auth" {
		os.Exit(runAuth(os.Args[2:], os.Stdout, os.Stderr))
	}

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

func runAuth(args []string, stdout, stderr *os.File) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printAuthUsage(stdout)
		return 0
	}

	switch args[0] {
	case "set":
		fs := flag.NewFlagSet("auth set", flag.ContinueOnError)
		fs.SetOutput(stderr)
		path := fs.String("path", ".", "path to git repository")
		email := fs.String("email", "", "email address to attach credentials to")
		githubToken := fs.String("github-token", "", "GitHub auth token for the email")
		gitlabToken := fs.String("gitlab-token", "", "GitLab auth token for the email")
		gpgPrivateKey := fs.String("gpg-private-key", "", "GPG private key file for the email")
		global := fs.Bool("global", true, "store in global git config (default)")
		local := fs.Bool("local", false, "store in local repository config instead of global config")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		repo, err := gitops.Open(*path)
		if err != nil {
			fmt.Fprintf(stderr, "Error opening repository: %v\n", err)
			return 1
		}
		useGlobal := *global && !*local
		cfg := gitops.MailAuthConfig{Email: *email}
		getExisting := repo.GetLocalMailAuthConfig
		if useGlobal {
			getExisting = repo.GetGlobalMailAuthConfig
		}
		if existing, err := getExisting(*email); err == nil {
			cfg = *existing
		}
		cfg.Email = *email
		provided := providedFlags(fs)
		if provided["github-token"] {
			cfg.GitHubToken = *githubToken
		}
		if provided["gitlab-token"] {
			cfg.GitLabToken = *gitlabToken
		}
		if provided["gpg-private-key"] && strings.TrimSpace(*gpgPrivateKey) == "" {
			cfg.GPGPrivateKey = ""
			cfg.GPGFingerprint = ""
			cfg.GPGKeyID = ""
			cfg.GPGKey = ""
		}
		if strings.TrimSpace(*gpgPrivateKey) != "" {
			privateKey, fingerprint, keyID, err := gitops.ReadGPGPrivateKey(*gpgPrivateKey)
			if err != nil {
				fmt.Fprintf(stderr, "Error reading GPG private key: %v\n", err)
				return 1
			}
			cfg.GPGPrivateKey = privateKey
			cfg.GPGFingerprint = fingerprint
			cfg.GPGKeyID = keyID
			cfg.GPGKey = ""
		}
		if err := repo.SetMailAuthConfig(cfg, useGlobal); err != nil {
			fmt.Fprintf(stderr, "Error storing auth config: %v\n", err)
			return 1
		}
		scope := "local"
		if useGlobal {
			scope = "global"
		}
		fmt.Fprintf(stdout, "stored %s auth config for %s\n", scope, strings.TrimSpace(*email))
		return 0

	case "get":
		fs := flag.NewFlagSet("auth get", flag.ContinueOnError)
		fs.SetOutput(stderr)
		path := fs.String("path", ".", "path to git repository")
		email := fs.String("email", "", "email address to read credentials for")
		showTokens := fs.Bool("show-tokens", false, "print full token values")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		repo, err := gitops.Open(*path)
		if err != nil {
			fmt.Fprintf(stderr, "Error opening repository: %v\n", err)
			return 1
		}
		cfg, err := repo.GetMailAuthConfig(*email)
		if err != nil {
			fmt.Fprintf(stderr, "Error reading auth config: %v\n", err)
			return 1
		}
		printMailAuth(stdout, *cfg, *showTokens)
		return 0

	case "list":
		fs := flag.NewFlagSet("auth list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		path := fs.String("path", ".", "path to git repository")
		showTokens := fs.Bool("show-tokens", false, "print full token values")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		repo, err := gitops.Open(*path)
		if err != nil {
			fmt.Fprintf(stderr, "Error opening repository: %v\n", err)
			return 1
		}
		configs, err := repo.ListMailAuthConfigs()
		if err != nil {
			fmt.Fprintf(stderr, "Error listing auth configs: %v\n", err)
			return 1
		}
		for _, cfg := range configs {
			printMailAuth(stdout, cfg, *showTokens)
		}
		return 0

	default:
		fmt.Fprintf(stderr, "unknown auth command: %s\n", args[0])
		printAuthUsage(stderr)
		return 2
	}
}

func providedFlags(fs *flag.FlagSet) map[string]bool {
	provided := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		provided[f.Name] = true
	})
	return provided
}

func printAuthUsage(out *os.File) {
	fmt.Fprintln(out, `Usage:
  git-backtrack auth set --email EMAIL [--github-token TOKEN] [--gitlab-token TOKEN] [--gpg-private-key PATH] [--global] [--local] [--path PATH]
  git-backtrack auth get --email EMAIL [--show-tokens] [--path PATH]
  git-backtrack auth list [--show-tokens] [--path PATH]`)
}

func printMailAuth(out *os.File, cfg gitops.MailAuthConfig, showTokens bool) {
	fmt.Fprintf(out, "%s\n", cfg.Email)
	fmt.Fprintf(out, "  github-token: %s\n", printableSecret(cfg.GitHubToken, showTokens))
	fmt.Fprintf(out, "  gitlab-token: %s\n", printableSecret(cfg.GitLabToken, showTokens))
	fmt.Fprintf(out, "  gpg-fingerprint: %s\n", valueOrUnset(gpgStatus(cfg)))
}

func gpgStatus(cfg gitops.MailAuthConfig) string {
	if cfg.GPGFingerprint != "" {
		return cfg.GPGFingerprint
	}
	if cfg.GPGKeyID != "" {
		return cfg.GPGKeyID
	}
	return cfg.GPGKey
}

func printableSecret(value string, show bool) string {
	if value == "" {
		return "<unset>"
	}
	if show {
		return value
	}
	if len(value) <= 4 {
		return "****"
	}
	return "****" + value[len(value)-4:]
}

func valueOrUnset(value string) string {
	if value == "" {
		return "<unset>"
	}
	return value
}
