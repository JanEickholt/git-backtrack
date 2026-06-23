package gitops

import (
	"path/filepath"
	"testing"
)

func TestMailAuthConfigStoresPerEmailCredentials(t *testing.T) {
	dir := initGitRepo(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))

	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}

	if err := repo.SetMailAuthConfig(MailAuthConfig{
		Email:          "Alice@Example.COM",
		GitHubToken:    "ghp_alice",
		GitLabToken:    "glpat-alice",
		GPGPrivateKey:  "private-key-alice",
		GPGFingerprint: "FINGERPRINTALICE",
		GPGKeyID:       "KEYIDALICE",
	}, false); err != nil {
		t.Fatalf("set alice config: %v", err)
	}
	if err := repo.SetMailAuthConfig(MailAuthConfig{
		Email:          "bob@example.com",
		GitHubToken:    "ghp_bob",
		GPGPrivateKey:  "private-key-bob",
		GPGFingerprint: "FINGERPRINTBOB",
	}, false); err != nil {
		t.Fatalf("set bob config: %v", err)
	}

	alice, err := repo.GetMailAuthConfig("alice@example.com")
	if err != nil {
		t.Fatalf("get alice config: %v", err)
	}
	if alice.Email != "alice@example.com" || alice.GitHubToken != "ghp_alice" || alice.GitLabToken != "glpat-alice" || alice.GPGPrivateKey != "private-key-alice" || alice.GPGFingerprint != "FINGERPRINTALICE" || alice.GPGKeyID != "KEYIDALICE" {
		t.Fatalf("alice config = %+v", alice)
	}

	bob, err := repo.GetMailAuthConfig("bob@example.com")
	if err != nil {
		t.Fatalf("get bob config: %v", err)
	}
	if bob.Email != "bob@example.com" || bob.GitHubToken != "ghp_bob" || bob.GitLabToken != "" || bob.GPGPrivateKey != "private-key-bob" || bob.GPGFingerprint != "FINGERPRINTBOB" {
		t.Fatalf("bob config = %+v", bob)
	}

	configs, err := repo.ListMailAuthConfigs()
	if err != nil {
		t.Fatalf("list configs: %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("len(configs) = %d, want 2: %+v", len(configs), configs)
	}
	if configs[0].Email != "alice@example.com" || configs[1].Email != "bob@example.com" {
		t.Fatalf("configs sorted by email = %+v", configs)
	}
}

func TestMailAuthConfigGlobalOverridesCanBeRead(t *testing.T) {
	dir := initGitRepo(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))

	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}

	if err := repo.SetMailAuthConfig(MailAuthConfig{Email: "alice@example.com", GitHubToken: "global-token", GPGPrivateKey: "global-key", GPGFingerprint: "GLOBAL"}, true); err != nil {
		t.Fatalf("set global auth config: %v", err)
	}
	if err := repo.SetMailAuthConfig(MailAuthConfig{Email: "bob@example.com", GitLabToken: "local-token"}, false); err != nil {
		t.Fatalf("set local auth config: %v", err)
	}

	alice, err := repo.GetMailAuthConfig("alice@example.com")
	if err != nil {
		t.Fatalf("get global alice config: %v", err)
	}
	if alice.GitHubToken != "global-token" || alice.GPGPrivateKey != "global-key" || alice.GPGFingerprint != "GLOBAL" {
		t.Fatalf("alice config = %+v", alice)
	}

	configs, err := repo.ListMailAuthConfigs()
	if err != nil {
		t.Fatalf("list configs: %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("len(configs) = %d, want 2: %+v", len(configs), configs)
	}
}

func TestSigningConfigForEmailUsesPerEmailGPGPrivateKey(t *testing.T) {
	dir := initGitRepo(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	gitOutput(t, dir, "config", "commit.gpgsign", "true")
	gitOutput(t, dir, "config", "user.signingkey", "DEFAULT")

	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	if err := repo.SetMailAuthConfig(MailAuthConfig{Email: "alice@example.com", GPGPrivateKey: "private-key", GPGFingerprint: "ALICEFPR"}, false); err != nil {
		t.Fatalf("set mail auth: %v", err)
	}

	alice, err := repo.GetSigningConfigForEmail("alice@example.com")
	if err != nil {
		t.Fatalf("get alice signing config: %v", err)
	}
	if !alice.SignCommits || alice.SigningKey != "ALICEFPR" || alice.PrivateKey != "private-key" || alice.KeyType != "gpg" {
		t.Fatalf("alice signing config = %+v", alice)
	}

	bob, err := repo.GetSigningConfigForEmail("bob@example.com")
	if err != nil {
		t.Fatalf("get bob signing config: %v", err)
	}
	if !bob.SignCommits || bob.SigningKey != "DEFAULT" || bob.KeyType != "gpg" {
		t.Fatalf("bob signing config = %+v", bob)
	}
}

func TestMailAuthConfigReadsLegacyGPGKeyFallback(t *testing.T) {
	dir := initGitRepo(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	gitOutput(t, dir, "config", mailAuthKey("alice@example.com", "email"), "alice@example.com")
	gitOutput(t, dir, "config", mailAuthKey("alice@example.com", "gpg-key"), "LEGACY")

	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	cfg, err := repo.GetMailAuthConfig("alice@example.com")
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if cfg.GPGKey != "LEGACY" {
		t.Fatalf("legacy gpg key = %+v", cfg)
	}
}

func TestMailAuthConfigStoresSSHKeys(t *testing.T) {
	dir := initGitRepo(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))

	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}

	if err := repo.SetMailAuthConfig(MailAuthConfig{
		Email:         "alice@example.com",
		SSHPrivateKey: "ssh-private-key-alice",
	}, false); err != nil {
		t.Fatalf("set alice config: %v", err)
	}

	alice, err := repo.GetMailAuthConfig("alice@example.com")
	if err != nil {
		t.Fatalf("get alice config: %v", err)
	}
	if alice.SSHPrivateKey != "ssh-private-key-alice" {
		t.Fatalf("alice ssh keys = %+v", alice)
	}

	if err := repo.SetMailAuthConfig(MailAuthConfig{
		Email:         "bob@example.com",
		SSHPrivateKey: "ssh-private-key-bob",
	}, false); err != nil {
		t.Fatalf("set bob config: %v", err)
	}

	bob, err := repo.GetMailAuthConfig("bob@example.com")
	if err != nil {
		t.Fatalf("get bob config: %v", err)
	}
	if bob.SSHPrivateKey != "ssh-private-key-bob" {
		t.Fatalf("bob ssh keys = %+v", bob)
	}

	// Verify unsetting works
	if err := repo.SetMailAuthConfig(MailAuthConfig{
		Email: "alice@example.com",
	}, false); err != nil {
		t.Fatalf("unset alice config: %v", err)
	}
	alice2, err := repo.GetMailAuthConfig("alice@example.com")
	if err != nil {
		t.Fatalf("get alice config after unset: %v", err)
	}
	if alice2.SSHPrivateKey != "" {
		t.Fatalf("alice ssh keys should be empty after unset = %+v", alice2)
	}
}

func TestSigningConfigForEmailUsesPerEmailSSHKey(t *testing.T) {
	dir := initGitRepo(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	gitOutput(t, dir, "config", "commit.gpgsign", "true")
	gitOutput(t, dir, "config", "user.signingkey", "DEFAULT")

	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	if err := repo.SetMailAuthConfig(MailAuthConfig{
		Email:         "alice@example.com",
		SSHPrivateKey: "ssh-private-key-alice",
	}, false); err != nil {
		t.Fatalf("set mail auth: %v", err)
	}

	alice, err := repo.GetSigningConfigForEmail("alice@example.com")
	if err != nil {
		t.Fatalf("get alice signing config: %v", err)
	}
	if !alice.SignCommits || alice.PrivateKey != "ssh-private-key-alice" || alice.KeyType != "ssh" {
		t.Fatalf("alice signing config = %+v", alice)
	}

	bob, err := repo.GetSigningConfigForEmail("bob@example.com")
	if err != nil {
		t.Fatalf("get bob signing config: %v", err)
	}
	if !bob.SignCommits || bob.SigningKey != "DEFAULT" || bob.KeyType != "gpg" {
		t.Fatalf("bob signing config = %+v", bob)
	}
}

func TestParseGPGFingerprintRequiresSecretKey(t *testing.T) {
	secret := "sec:-:4096:1:ABC:0:0::::::scESC:::+:::23::0:\n" +
		"fpr:::::::::0123456789ABCDEF0123456789ABCDEF01234567:\n" +
		"uid:::::::::Alice <alice@example.com>:\n"
	if got := parseGPGFingerprint(secret); got != "0123456789ABCDEF0123456789ABCDEF01234567" {
		t.Fatalf("secret fingerprint = %q", got)
	}

	public := "pub:-:4096:1:ABC:0:0::::::scESC::::::23::0:\n" +
		"fpr:::::::::0123456789ABCDEF0123456789ABCDEF01234567:\n"
	if got := parseGPGFingerprint(public); got != "" {
		t.Fatalf("public fingerprint = %q, want empty", got)
	}
}
