package gitops

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	gitconfig "github.com/go-git/go-git/v5/plumbing/format/config"
	"github.com/go-git/go-git/v5/plumbing/object"
)

var ErrNotAGitRepository = errors.New("not a git repository")

type CommitInfo struct {
	Hash        plumbing.Hash
	ShortHash   string
	AuthorName  string
	AuthorEmail string
	AuthorDate  time.Time
	Message     string
	Parents     []plumbing.Hash
	Additions   int
	Deletions   int
}

type Repository struct {
	repo *git.Repository
	path string
}

func Open(path string) (*Repository, error) {
	repo, err := git.PlainOpen(path)
	if err != nil {
		if errors.Is(err, git.ErrRepositoryNotExists) {
			return nil, ErrNotAGitRepository
		}
		return nil, fmt.Errorf("failed to open repository: %w", err)
	}
	return &Repository{repo: repo, path: path}, nil
}

func (r *Repository) Reload() error {
	repo, err := git.PlainOpen(r.path)
	if err != nil {
		return fmt.Errorf("failed to reopen repository: %w", err)
	}
	r.repo = repo
	return nil
}

func (r *Repository) ListAllCommits() ([]CommitInfo, error) {
	r.Reload()

	refs, err := r.repo.References()
	if err != nil {
		return nil, fmt.Errorf("failed to get references: %w", err)
	}

	seen := make(map[plumbing.Hash]bool)
	var commits []CommitInfo

	if err := refs.ForEach(func(ref *plumbing.Reference) error {
		if ref.Type() != plumbing.HashReference {
			return nil
		}

		refName := ref.Name().String()
		if strings.HasPrefix(refName, "refs/backtrack-backup/") {
			return nil
		}

		hash := ref.Hash()
		if seen[hash] {
			return nil
		}

		commit, err := r.repo.CommitObject(hash)
		if err != nil {
			return nil
		}

		return commitHistory(r.repo, commit, &commits, seen)
	}); err != nil {
		return nil, err
	}

	// Sort commits by date, newest first
	sort.Slice(commits, func(i, j int) bool {
		return commits[i].AuthorDate.After(commits[j].AuthorDate)
	})

	return commits, nil
}

func (r *Repository) ListCommitsFromRef(refName string) ([]CommitInfo, error) {
	ref, err := r.repo.Reference(plumbing.ReferenceName(refName), true)
	if err != nil {
		return nil, fmt.Errorf("failed to get reference %s: %w", refName, err)
	}

	commit, err := r.repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get commit: %w", err)
	}

	seen := make(map[plumbing.Hash]bool)
	var commits []CommitInfo

	if err := commitHistory(r.repo, commit, &commits, seen); err != nil {
		return nil, err
	}

	return commits, nil
}

func (r *Repository) GetHead() (*plumbing.Reference, error) {
	return r.repo.Head()
}

func (r *Repository) ListBranches() ([]string, error) {
	branches, err := r.repo.Branches()
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}

	var names []string
	if err := branches.ForEach(func(ref *plumbing.Reference) error {
		names = append(names, ref.Name().Short())
		return nil
	}); err != nil {
		return nil, err
	}

	return names, nil
}

func commitHistory(repo *git.Repository, start *object.Commit, commits *[]CommitInfo, seen map[plumbing.Hash]bool) error {
	stack := []*object.Commit{start}

	for len(stack) > 0 {
		commit := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if seen[commit.Hash] {
			continue
		}
		seen[commit.Hash] = true

		stats, _ := commit.Stats()
		additions := 0
		deletions := 0
		for _, stat := range stats {
			additions += stat.Addition
			deletions += stat.Deletion
		}

		*commits = append(*commits, CommitInfo{
			Hash:        commit.Hash,
			ShortHash:   commit.Hash.String()[:7],
			AuthorName:  commit.Author.Name,
			AuthorEmail: commit.Author.Email,
			AuthorDate:  commit.Author.When,
			Message:     commit.Message,
			Parents:     commit.ParentHashes,
			Additions:   additions,
			Deletions:   deletions,
		})

		for _, parentHash := range commit.ParentHashes {
			if !seen[parentHash] {
				parent, err := repo.CommitObject(parentHash)
				if err != nil {
					continue
				}
				stack = append(stack, parent)
			}
		}
	}

	return nil
}

func (r *Repository) GetRepository() *git.Repository {
	return r.repo
}

type SigningConfig struct {
	SignCommits bool
	SigningKey  string
	KeyType     string
}

// readGlobalGitConfig parses ~/.gitconfig into a go-git config struct
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

// GetSigningConfig reads signing config from local repo config first,
// then falls back to the global ~/.gitconfig.

type UserIdentity struct {
	Name  string
	Email string
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

	// Collect configs: local first, then global as fallback
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

	// Derive key type from signingkey value if not set by gpg.format
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

	// Encode the commit without any existing signature to get the payload to sign
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

	// Set the signature on the commit and re-encode via go-git
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

