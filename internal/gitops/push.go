package gitops

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

type PushAccount struct {
	Email string
	Forge string
	Token string
}

func (r *Repository) ListPushAccounts() ([]PushAccount, error) {
	configs, err := r.ListMailAuthConfigs()
	if err != nil {
		return nil, err
	}
	accounts := make([]PushAccount, 0, len(configs))
	for _, cfg := range configs {
		if cfg.GitHubToken != "" {
			accounts = append(accounts, PushAccount{Email: cfg.Email, Forge: "GitHub", Token: cfg.GitHubToken})
		}
		if cfg.GitLabToken != "" {
			accounts = append(accounts, PushAccount{Email: cfg.Email, Forge: "GitLab", Token: cfg.GitLabToken})
		}
	}
	return accounts, nil
}

func (r *Repository) PushWithAccount(account PushAccount) error {
	return r.pushWithAccount(account, "--force-with-lease")
}

func (r *Repository) ForcePushWithAccount(account PushAccount) error {
	return r.pushWithAccount(account, "--force")
}

func (r *Repository) pushWithAccount(account PushAccount, forceFlag string) error {
	branch := strings.TrimSpace(r.gitOutputIgnoreError("branch", "--show-current"))
	if branch == "" {
		return fmt.Errorf("cannot push detached HEAD")
	}
	remote := strings.TrimSpace(r.gitOutputIgnoreError("config", "branch."+branch+".remote"))
	if remote == "" {
		remote = "origin"
	}
	remoteURL, err := r.gitOutput("remote", "get-url", remote)
	if err != nil {
		return err
	}
	pushURL, err := tokenPushURL(strings.TrimSpace(remoteURL), account.Forge)
	if err != nil {
		return err
	}
	if err := validatePushHost(pushURL, account.Forge); err != nil {
		return err
	}
	askpass, err := writeAskpass(account)
	if err != nil {
		return err
	}
	defer os.Remove(askpass)
	cmd := exec.Command("git", "-C", r.path, "push", forceFlag, pushURL, "HEAD:"+branch)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS="+askpass)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (r *Repository) gitOutputIgnoreError(args ...string) string {
	output, err := r.gitOutput(args...)
	if err != nil {
		return ""
	}
	return output
}

func tokenPushURL(remoteURL, forge string) (string, error) {
	remoteURL = strings.TrimSpace(remoteURL)
	if strings.HasPrefix(remoteURL, "git@") {
		parts := strings.SplitN(strings.TrimPrefix(remoteURL, "git@"), ":", 2)
		if len(parts) == 2 {
			return "https://" + parts[0] + "/" + parts[1], nil
		}
	}
	u, err := url.Parse(remoteURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("unsupported remote URL for %s token push: %s", forge, remoteURL)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("unsupported remote URL scheme for token push: %s", u.Scheme)
	}
	u.User = nil
	return u.String(), nil
}

func validatePushHost(pushURL, forge string) error {
	u, err := url.Parse(pushURL)
	if err != nil {
		return err
	}
	host := strings.ToLower(u.Hostname())
	switch forge {
	case "GitHub":
		if host == "github.com" {
			return nil
		}
	case "GitLab":
		// Trust the configured remote host for GitLab tokens because GitLab is
		// commonly self-hosted and the remote URL is already trusted by git.
		if u.Scheme == "https" {
			return nil
		}
	}
	return fmt.Errorf("refusing to send %s token to %s", forge, host)
}

func writeAskpass(account PushAccount) (string, error) {
	username := "oauth2"
	if account.Forge == "GitHub" {
		username = "x-access-token"
	}
	script := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n*Username*) printf '%%s\\n' %s ;;\n*) printf '%%s\\n' %s ;;\nesac\n", shellSingleQuote(username), shellSingleQuote(account.Token))
	f, err := os.CreateTemp("", "git-backtrack-askpass-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	if err := os.Chmod(name, 0700); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
