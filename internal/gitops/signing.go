package gitops

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	gitconfig "github.com/go-git/go-git/v5/plumbing/format/config"
)

func readGlobalGitConfig() (*gitconfig.Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(home, ".gitconfig"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfg := gitconfig.New()
	if err := gitconfig.NewDecoder(f).Decode(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (r *Repository) GetUserIdentity() (*UserIdentity, error) {
	type rawCfg struct {
		cfg *gitconfig.Config
	}
	var cfgs []rawCfg
	if localCfg, err := r.repo.Config(); err == nil {
		cfgs = append(cfgs, rawCfg{localCfg.Raw})
	}
	if globalCfg, err := readGlobalGitConfig(); err == nil {
		cfgs = append(cfgs, rawCfg{globalCfg})
	}

	id := &UserIdentity{}
	for _, c := range cfgs {
		if s := c.cfg.Section("user"); s != nil {
			if id.Name == "" {
				id.Name = s.Option("name")
			}
			if id.Email == "" {
				id.Email = s.Option("email")
			}
		}
		if id.Name != "" && id.Email != "" {
			break
		}
	}
	return id, nil
}

func (r *Repository) GetSigningConfig() (*SigningConfig, error) {
	cfg := &SigningConfig{}

	type rawCfg struct {
		cfg *gitconfig.Config
	}
	var cfgs []rawCfg

	if localCfg, err := r.repo.Config(); err == nil {
		cfgs = append(cfgs, rawCfg{localCfg.Raw})
	}
	if globalCfg, err := readGlobalGitConfig(); err == nil {
		cfgs = append(cfgs, rawCfg{globalCfg})
	}

	for _, c := range cfgs {
		if !cfg.SignCommits {
			if s := c.cfg.Section("commit"); s != nil {
				if v := s.Option("gpgsign"); v == "true" || v == "1" {
					cfg.SignCommits = true
				}
			}
		}
		if cfg.SigningKey == "" {
			if s := c.cfg.Section("user"); s != nil {
				if v := s.Option("signingkey"); v != "" {
					cfg.SigningKey = v
				}
			}
		}
		if cfg.KeyType == "" {
			if s := c.cfg.Section("gpg"); s != nil {
				if v := s.Option("format"); v != "" {
					cfg.KeyType = v
				}
			}
		}
	}

	if cfg.KeyType == "" && cfg.SigningKey != "" {
		if strings.HasPrefix(cfg.SigningKey, "ssh:") ||
			strings.Contains(cfg.SigningKey, ".pub") ||
			strings.HasPrefix(cfg.SigningKey, "key::") {
			cfg.KeyType = "ssh"
		} else {
			cfg.KeyType = "gpg"
		}
	}

	return cfg, nil
}

func normalizeMail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func mailAuthSection(email string) string {
	return "git-backtrack-mail-" + hex.EncodeToString([]byte(normalizeMail(email)))
}

func mailAuthKey(email, key string) string {
	return mailAuthSection(email) + "." + key
}

func expandTilde(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

func ReadSSHKey(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("ssh key path is required")
	}
	path, err := expandTilde(path)
	if err != nil {
		return "", err
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(key) == 0 {
		return "", fmt.Errorf("ssh key file is empty: %s", path)
	}
	return string(key), nil
}

func ReadGPGPrivateKey(path string) (string, string, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", "", "", fmt.Errorf("gpg private key path is required")
	}
	path, err := expandTilde(path)
	if err != nil {
		return "", "", "", err
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return "", "", "", err
	}
	cmd := exec.Command("gpg", "--import-options", "show-only", "--with-colons", "--import", path)
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", "", fmt.Errorf("gpg inspect failed: %s: %w", strings.TrimSpace(string(output)), err)
	}
	fingerprint := parseGPGFingerprint(string(output))
	if fingerprint == "" {
		return "", "", "", fmt.Errorf("gpg private key fingerprint not found")
	}
	keyID := fingerprint
	if len(fingerprint) > 16 {
		keyID = fingerprint[len(fingerprint)-16:]
	}
	return string(key), fingerprint, keyID, nil
}

func parseGPGFingerprint(output string) string {
	wantSecretFingerprint := false
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, ":")
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "sec" {
			wantSecretFingerprint = true
			continue
		}
		if wantSecretFingerprint && len(fields) > 9 && fields[0] == "fpr" && strings.TrimSpace(fields[9]) != "" {
			return strings.TrimSpace(fields[9])
		}
	}
	return ""
}

func (r *Repository) configGet(scope, key string) (string, bool, error) {
	args := []string{}
	if scope != "" {
		args = append(args, scope)
	}
	args = append(args, "--get", key)
	cmd := exec.Command("git", append([]string{"-C", r.path, "config"}, args...)...)
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return "", false, nil
		}
		text := strings.TrimSpace(string(output))
		if text == "" {
			return "", false, err
		}
		return "", false, fmt.Errorf("git config --get %s: %s: %w", key, text, err)
	}
	return strings.TrimRight(string(output), "\r\n"), true, nil
}

func (r *Repository) configSet(global bool, key, value string) error {
	scope := "--local"
	if global {
		scope = "--global"
	}
	cmd := exec.Command("git", "-C", r.path, "config", scope, key, value)
	cmd.Env = os.Environ()
	if output, err := cmd.CombinedOutput(); err != nil {
		text := strings.TrimSpace(string(output))
		if text == "" {
			return err
		}
		return fmt.Errorf("git config %s %s: %s: %w", scope, key, text, err)
	}
	return nil
}

func (r *Repository) configUnset(global bool, key string) error {
	scope := "--local"
	if global {
		scope = "--global"
	}
	cmd := exec.Command("git", "-C", r.path, "config", scope, "--unset", key)
	cmd.Env = os.Environ()
	if output, err := cmd.CombinedOutput(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 5 {
			return nil
		}
		text := strings.TrimSpace(string(output))
		if text == "" {
			return err
		}
		return fmt.Errorf("git config %s --unset %s: %s: %w", scope, key, text, err)
	}
	return nil
}

func (r *Repository) configSetOrUnset(global bool, key, value string) error {
	if value == "" {
		return r.configUnset(global, key)
	}
	return r.configSet(global, key, value)
}

func (r *Repository) SetMailAuthConfig(cfg MailAuthConfig, global bool) error {
	cfg.Email = normalizeMail(cfg.Email)
	if cfg.Email == "" {
		return fmt.Errorf("email is required")
	}
	if err := r.configSet(global, mailAuthKey(cfg.Email, "email"), cfg.Email); err != nil {
		return err
	}
	if err := r.configSetOrUnset(global, mailAuthKey(cfg.Email, "github-token"), cfg.GitHubToken); err != nil {
		return err
	}
	if err := r.configSetOrUnset(global, mailAuthKey(cfg.Email, "gitlab-token"), cfg.GitLabToken); err != nil {
		return err
	}
	if err := r.configSetOrUnset(global, mailAuthKey(cfg.Email, "gpg-private-key"), base64.StdEncoding.EncodeToString([]byte(cfg.GPGPrivateKey))); err != nil {
		return err
	}
	if cfg.GPGPrivateKey == "" {
		if err := r.configUnset(global, mailAuthKey(cfg.Email, "gpg-private-key")); err != nil {
			return err
		}
	}
	if err := r.configSetOrUnset(global, mailAuthKey(cfg.Email, "gpg-fingerprint"), cfg.GPGFingerprint); err != nil {
		return err
	}
	if err := r.configSetOrUnset(global, mailAuthKey(cfg.Email, "gpg-key-id"), cfg.GPGKeyID); err != nil {
		return err
	}
	if err := r.configSetOrUnset(global, mailAuthKey(cfg.Email, "gpg-private-key-path"), cfg.GPGPrivateKeyPath); err != nil {
		return err
	}
	if cfg.GPGPrivateKey != "" || cfg.GPGKey == "" {
		if err := r.configUnset(global, mailAuthKey(cfg.Email, "gpg-key")); err != nil {
			return err
		}
		if err := r.configUnset(global, mailAuthKey(cfg.Email, "pgp-key")); err != nil {
			return err
		}
	}
	if err := r.configSetOrUnset(global, mailAuthKey(cfg.Email, "ssh-private-key"), base64.StdEncoding.EncodeToString([]byte(cfg.SSHPrivateKey))); err != nil {
		return err
	}
	if cfg.SSHPrivateKey == "" {
		if err := r.configUnset(global, mailAuthKey(cfg.Email, "ssh-private-key")); err != nil {
			return err
		}
	}
	if err := r.configSetOrUnset(global, mailAuthKey(cfg.Email, "ssh-private-key-path"), cfg.SSHPrivateKeyPath); err != nil {
		return err
	}
	return nil
}

func (r *Repository) GetMailAuthConfig(email string) (*MailAuthConfig, error) {
	return r.getMailAuthConfig(email, []string{"--local", "--global"})
}

func (r *Repository) GetGlobalMailAuthConfig(email string) (*MailAuthConfig, error) {
	return r.getMailAuthConfig(email, []string{"--global"})
}

func (r *Repository) GetLocalMailAuthConfig(email string) (*MailAuthConfig, error) {
	return r.getMailAuthConfig(email, []string{"--local"})
}

func (r *Repository) getMailAuthConfig(email string, scopes []string) (*MailAuthConfig, error) {
	email = normalizeMail(email)
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}
	cfg := &MailAuthConfig{Email: email}
	for _, item := range []struct {
		key string
		set func(string)
	}{
		{"github-token", func(v string) { cfg.GitHubToken = v }},
		{"gitlab-token", func(v string) { cfg.GitLabToken = v }},
		{"gpg-private-key", func(v string) {
			decoded, err := base64.StdEncoding.DecodeString(v)
			if err == nil {
				cfg.GPGPrivateKey = string(decoded)
			}
		}},
		{"gpg-fingerprint", func(v string) { cfg.GPGFingerprint = v }},
		{"gpg-key-id", func(v string) { cfg.GPGKeyID = v }},
		{"gpg-key", func(v string) { cfg.GPGKey = v }},
		{"pgp-key", func(v string) {
			if cfg.GPGKey == "" {
				cfg.GPGKey = v
			}
		}},
		{"ssh-private-key", func(v string) {
			decoded, err := base64.StdEncoding.DecodeString(v)
			if err == nil {
				cfg.SSHPrivateKey = string(decoded)
			}
		}},
		{"gpg-private-key-path", func(v string) { cfg.GPGPrivateKeyPath = v }},
		{"ssh-private-key-path", func(v string) { cfg.SSHPrivateKeyPath = v }},
	} {
		for _, scope := range scopes {
			value, ok, err := r.configGet(scope, mailAuthKey(email, item.key))
			if err != nil {
				return nil, err
			}
			if ok {
				item.set(value)
				break
			}
		}
	}
	return cfg, nil
}

func (r *Repository) ListMailAuthConfigs() ([]MailAuthConfig, error) {
	emails := map[string]bool{}
	for _, scope := range []string{"--global", "--local"} {
		cmd := exec.Command("git", "-C", r.path, "config", scope, "--get-regexp", `^git-backtrack-mail-[0-9a-f]+\.email$`)
		cmd.Env = os.Environ()
		output, err := cmd.CombinedOutput()
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
				continue
			}
			text := strings.TrimSpace(string(output))
			if text == "" {
				return nil, err
			}
			return nil, fmt.Errorf("git config --get-regexp mail auth: %s: %w", text, err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				emails[normalizeMail(fields[1])] = true
			}
		}
	}

	keys := make([]string, 0, len(emails))
	for email := range emails {
		keys = append(keys, email)
	}
	sort.Strings(keys)

	configs := make([]MailAuthConfig, 0, len(keys))
	for _, email := range keys {
		cfg, err := r.GetMailAuthConfig(email)
		if err != nil {
			return nil, err
		}
		configs = append(configs, *cfg)
	}
	return configs, nil
}

func (r *Repository) GetSigningConfigForEmail(email string) (*SigningConfig, error) {
	cfg, err := r.GetSigningConfig()
	if err != nil {
		return nil, err
	}
	mailCfg, err := r.GetMailAuthConfig(email)
	if err != nil {
		return nil, err
	}
	if mailCfg.GPGPrivateKey != "" {
		cfg.PrivateKey = mailCfg.GPGPrivateKey
		cfg.SigningKey = mailCfg.GPGFingerprint
		if cfg.SigningKey == "" {
			cfg.SigningKey = mailCfg.GPGKeyID
		}
		cfg.KeyType = "gpg"
	} else if mailCfg.GPGKey != "" {
		cfg.SigningKey = mailCfg.GPGKey
		cfg.KeyType = "gpg"
	} else if mailCfg.SSHPrivateKey != "" {
		cfg.PrivateKey = mailCfg.SSHPrivateKey
		cfg.KeyType = "ssh"
	}
	return cfg, nil
}

func (r *Repository) SignCommit(commitHash plumbing.Hash) (plumbing.Hash, error) {
	signingConfig, err := r.GetSigningConfig()
	if err != nil || !signingConfig.SignCommits {
		return commitHash, nil
	}

	switch signingConfig.KeyType {
	case "ssh":
		return r.signCommitSSH(commitHash, signingConfig)
	case "gpg", "":
		return r.signCommitGPG(commitHash, signingConfig)
	default:
		return commitHash, fmt.Errorf("unsupported signing key type: %s", signingConfig.KeyType)
	}
}

func (r *Repository) SignCommitForEmail(commitHash plumbing.Hash, email string) (plumbing.Hash, error) {
	signingConfig, err := r.GetSigningConfigForEmail(email)
	if err != nil || !signingConfig.SignCommits {
		return commitHash, nil
	}

	switch signingConfig.KeyType {
	case "ssh":
		return r.signCommitSSH(commitHash, signingConfig)
	case "gpg", "":
		return r.signCommitGPG(commitHash, signingConfig)
	default:
		return commitHash, fmt.Errorf("unsupported signing key type: %s", signingConfig.KeyType)
	}
}

func (r *Repository) signCommitGPG(commitHash plumbing.Hash, signingConfig *SigningConfig) (plumbing.Hash, error) {
	commit, err := r.repo.CommitObject(commitHash)
	if err != nil {
		return commitHash, err
	}

	commit.PGPSignature = ""
	tmpObj := r.repo.Storer.NewEncodedObject()
	tmpObj.SetType(plumbing.CommitObject)
	if err := commit.Encode(tmpObj); err != nil {
		return commitHash, err
	}
	reader, err := tmpObj.Reader()
	if err != nil {
		return commitHash, err
	}
	payload, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		return commitHash, err
	}

	env := os.Environ()
	if signingConfig.PrivateKey != "" {
		gnupgHome, err := os.MkdirTemp("", "git-backtrack-gnupg-*")
		if err != nil {
			return commitHash, err
		}
		defer os.RemoveAll(gnupgHome)
		if err := os.Chmod(gnupgHome, 0700); err != nil {
			return commitHash, err
		}
		importCmd := exec.Command("gpg", "--batch", "--import")
		importCmd.Env = append(env, "GNUPGHOME="+gnupgHome)
		importCmd.Stdin = strings.NewReader(signingConfig.PrivateKey)
		if out, err := importCmd.CombinedOutput(); err != nil {
			return commitHash, fmt.Errorf("gpg import failed: %s: %w", strings.TrimSpace(string(out)), err)
		}
		env = append(env, "GNUPGHOME="+gnupgHome)
	}
	cmd := exec.Command("gpg", "--batch", "--yes", "--status-fd=2", "-bsau", signingConfig.SigningKey)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(string(payload))
	sig, err := cmd.Output()
	if err != nil {
		return commitHash, fmt.Errorf("gpg sign failed: %w", err)
	}

	commit.PGPSignature = string(sig)
	newObj := r.repo.Storer.NewEncodedObject()
	newObj.SetType(plumbing.CommitObject)
	if err := commit.Encode(newObj); err != nil {
		return commitHash, err
	}

	newHash, err := r.repo.Storer.SetEncodedObject(newObj)
	if err != nil {
		return commitHash, err
	}

	return newHash, nil
}

func (r *Repository) signCommitSSH(commitHash plumbing.Hash, signingConfig *SigningConfig) (plumbing.Hash, error) {
	commit, err := r.repo.CommitObject(commitHash)
	if err != nil {
		return commitHash, err
	}

	tmpObj := r.repo.Storer.NewEncodedObject()
	tmpObj.SetType(plumbing.CommitObject)
	if err := commit.Encode(tmpObj); err != nil {
		return commitHash, err
	}

	reader, err := tmpObj.Reader()
	if err != nil {
		return commitHash, err
	}
	commitBytes, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		return commitHash, err
	}
	raw := string(commitBytes)

	tmpFile, err := os.CreateTemp("", "git-commit-*")
	if err != nil {
		return commitHash, err
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Write(commitBytes)
	tmpFile.Close()

	sigFile := tmpFile.Name() + ".sig"
	defer os.Remove(sigFile)

	var keyPath string
	if signingConfig.PrivateKey != "" {
		tmpDir, err := os.MkdirTemp("", "git-backtrack-ssh-*")
		if err != nil {
			return commitHash, err
		}
		defer os.RemoveAll(tmpDir)
		idPath := filepath.Join(tmpDir, "id")
		if err := os.WriteFile(idPath, []byte(signingConfig.PrivateKey), 0600); err != nil {
			return commitHash, fmt.Errorf("failed to write ssh private key: %w", err)
		}
		// derive public key from private key
		deriveCmd := exec.Command("ssh-keygen", "-y", "-f", idPath)
		deriveCmd.Env = os.Environ()
		pubKey, err := deriveCmd.Output()
		if err != nil {
			return commitHash, fmt.Errorf("failed to derive ssh public key from private key: %w", err)
		}
		pubPath := filepath.Join(tmpDir, "id.pub")
		if err := os.WriteFile(pubPath, pubKey, 0644); err != nil {
			return commitHash, fmt.Errorf("failed to write ssh public key: %w", err)
		}
		keyPath = idPath
	} else {
		keyPath = strings.TrimPrefix(signingConfig.SigningKey, "key::")
	}
	cmd := exec.Command("ssh-keygen", "-Y", "sign", "-f", keyPath, "-n", "git", tmpFile.Name())
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		return commitHash, fmt.Errorf("ssh-keygen sign failed: %s: %w", out, err)
	}

	sig, err := os.ReadFile(sigFile)
	if err != nil {
		return commitHash, fmt.Errorf("failed to read signature: %w", err)
	}

	sigStr := strings.TrimSpace(string(sig))
	gpgHeader := "gpgsig " + strings.ReplaceAll(sigStr, "\n", "\n ") + "\n"

	insertAt := strings.Index(raw, "\nauthor")
	if insertAt == -1 {
		return commitHash, fmt.Errorf("malformed commit object")
	}
	signed := raw[:insertAt+1] + gpgHeader + raw[insertAt+1:]

	newObj := r.repo.Storer.NewEncodedObject()
	newObj.SetType(plumbing.CommitObject)
	w, err := newObj.Writer()
	if err != nil {
		return commitHash, err
	}
	w.Write([]byte(signed))
	w.Close()

	newHash, err := r.repo.Storer.SetEncodedObject(newObj)
	if err != nil {
		return commitHash, err
	}

	return newHash, nil
}
