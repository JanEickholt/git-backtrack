package gitops

import (
	"time"

	"github.com/go-git/go-git/v5/plumbing"
)

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
	IsUnpushed  bool
}

type SigningConfig struct {
	SignCommits bool
	SigningKey  string
	PrivateKey  string
	KeyType     string
}

type MailAuthConfig struct {
	Email          string
	GitHubToken    string
	GitLabToken    string
	GPGKey         string
	GPGPrivateKey  string
	GPGFingerprint string
	GPGKeyID       string
}

type UserIdentity struct {
	Name  string
	Email string
}

type ForgeOperation int

const (
	ForgeEdit ForgeOperation = iota
	ForgeDrop
	ForgeCombine
)

type ForgeChange struct {
	OriginalHash  plumbing.Hash
	Operation     ForgeOperation
	NewAuthor     *AuthorInfo
	NewMessage    string
	NewDate       *time.Time
	CombineGroup  []plumbing.Hash
	CombineAnchor plumbing.Hash
}

func (c ForgeChange) HasChanges() bool {
	return c.Operation != ForgeEdit || c.NewAuthor != nil || c.NewDate != nil || c.NewMessage != ""
}

type AuthorInfo struct {
	Name  string
	Email string
}

type RewriteResult struct {
	BackupRef   string
	ChangedRefs map[plumbing.Hash]plumbing.Hash
	Errors      []error
}
