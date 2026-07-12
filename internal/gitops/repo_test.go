package gitops

import (
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
)

func TestParseCommitInfoLogKeepsMultilineMessageAndStats(t *testing.T) {
	output := commitRecordMarker + "1111111111111111111111111111111111111111\x1f" +
		"Test User\x1ftest@example.com\x1f2024-01-02T03:04:05+02:00\x1f" +
		"2222222222222222222222222222222222222222 3333333333333333333333333333333333333333\x1f" +
		"subject\n\nbody paragraph\x1f\n\n" +
		"3\t1\tfile.go\n" +
		"-\t-\tbinary.dat\n" +
		"7\t2\tdir/file.go\n"

	commits := parseCommitInfoLog(output, true)
	if len(commits) != 1 {
		t.Fatalf("len(commits) = %d, want 1", len(commits))
	}

	commit := commits[0]
	if commit.Hash != plumbing.NewHash("1111111111111111111111111111111111111111") {
		t.Fatalf("hash = %s", commit.Hash)
	}
	if commit.ShortHash != "1111111" {
		t.Fatalf("short hash = %q", commit.ShortHash)
	}
	if commit.AuthorName != "Test User" || commit.AuthorEmail != "test@example.com" {
		t.Fatalf("author = %q <%q>", commit.AuthorName, commit.AuthorEmail)
	}
	wantDate := time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("", 2*60*60))
	if !commit.AuthorDate.Equal(wantDate) {
		t.Fatalf("date = %s, want %s", commit.AuthorDate, wantDate)
	}
	if commit.Message != "subject\n\nbody paragraph" {
		t.Fatalf("message = %q", commit.Message)
	}
	if len(commit.Parents) != 2 {
		t.Fatalf("parents = %d, want 2", len(commit.Parents))
	}
	if commit.Additions != 10 || commit.Deletions != 3 {
		t.Fatalf("stats = +%d -%d, want +10 -3", commit.Additions, commit.Deletions)
	}
	if !commit.StatsLoaded {
		t.Fatal("StatsLoaded = false, want true")
	}
}

func TestParseCommitInfoLogCanSkipStats(t *testing.T) {
	output := commitRecordMarker + "1111111111111111111111111111111111111111\x1f" +
		"Test User\x1ftest@example.com\x1f2024-01-02T03:04:05+02:00\x1f" +
		"\x1fsubject\n\nbody paragraph\x1f"

	commits := parseCommitInfoLog(output, false)
	if len(commits) != 1 {
		t.Fatalf("len(commits) = %d, want 1", len(commits))
	}

	commit := commits[0]
	if commit.Additions != 0 || commit.Deletions != 0 {
		t.Fatalf("stats = +%d -%d, want +0 -0", commit.Additions, commit.Deletions)
	}
	if commit.StatsLoaded {
		t.Fatal("StatsLoaded = true, want false")
	}
}
