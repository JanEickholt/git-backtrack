package gitops

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

	cmd := exec.Command("gpg", "--status-fd=2", "-bsau", signingConfig.SigningKey)
	cmd.Env = os.Environ()
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

	keyPath := strings.TrimPrefix(signingConfig.SigningKey, "key::")
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
