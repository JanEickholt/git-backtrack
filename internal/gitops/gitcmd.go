package gitops

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func (hr *HistoryRewriter) runGit(dir string, args ...string) error {
	return hr.runGitEnv(dir, os.Environ(), args...)
}

func (hr *HistoryRewriter) runGitEnv(dir string, env []string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text == "" {
			return err
		}
		return fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), text, err)
	}
	return nil
}

func (hr *HistoryRewriter) gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text == "" {
			return "", err
		}
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), text, err)
	}
	return string(output), nil
}
