package gitops

import "testing"

func TestTokenPushURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "https", in: "https://github.com/acme/repo.git", want: "https://github.com/acme/repo.git"},
		{name: "https credentials", in: "https://old:secret@gitlab.com/acme/repo.git", want: "https://gitlab.com/acme/repo.git"},
		{name: "ssh", in: "git@github.com:acme/repo.git", want: "https://github.com/acme/repo.git"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tokenPushURL(tc.in, "GitHub")
			if err != nil {
				t.Fatalf("tokenPushURL error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("tokenPushURL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTokenPushURLRejectsHTTP(t *testing.T) {
	if _, err := tokenPushURL("http://github.com/acme/repo.git", "GitHub"); err == nil {
		t.Fatalf("tokenPushURL accepted http remote")
	}
}

func TestValidatePushHost(t *testing.T) {
	if err := validatePushHost("https://github.com/acme/repo.git", "GitHub"); err != nil {
		t.Fatalf("validate github host: %v", err)
	}
	if err := validatePushHost("https://evil.example/acme/repo.git", "GitHub"); err == nil {
		t.Fatalf("validatePushHost accepted wrong GitHub host")
	}
	if err := validatePushHost("https://gitlab.com/acme/repo.git", "GitLab"); err != nil {
		t.Fatalf("validate gitlab host: %v", err)
	}
}

func TestShellSingleQuote(t *testing.T) {
	got := shellSingleQuote("a'b$(touch nope)`x")
	want := "'a'\\''b$(touch nope)`x'"
	if got != want {
		t.Fatalf("shellSingleQuote = %q, want %q", got, want)
	}
}

func TestListPushAccounts(t *testing.T) {
	dir := initGitRepo(t)
	t.Setenv("HOME", t.TempDir())

	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	if err := repo.SetMailAuthConfig(MailAuthConfig{Email: "alice@example.com", GitHubToken: "gh"}, false); err != nil {
		t.Fatalf("set alice config: %v", err)
	}
	if err := repo.SetMailAuthConfig(MailAuthConfig{Email: "bob@example.com", GitLabToken: "gl"}, false); err != nil {
		t.Fatalf("set bob config: %v", err)
	}

	accounts, err := repo.ListPushAccounts()
	if err != nil {
		t.Fatalf("list push accounts: %v", err)
	}
	if len(accounts) != 2 || accounts[0].Email != "alice@example.com" || accounts[0].Forge != "GitHub" || accounts[1].Email != "bob@example.com" || accounts[1].Forge != "GitLab" {
		t.Fatalf("accounts = %+v", accounts)
	}
}
