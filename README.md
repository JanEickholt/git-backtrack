# git-backtrack

`git-backtrack` is an interactive history editor for local Git branches. It can edit commit metadata, drop commits, and fold commits together using a guarded replay workflow that creates backups before rewriting history.

## Usage

Start the TUI in the current repository:

```sh
git-backtrack
```

Open a different repository:

```sh
git-backtrack --path /path/to/repo
```

The default view is clean and compact. Optional display flags are available when launching the TUI:

```sh
git-backtrack --graph
git-backtrack --graph --graph-order author-date
git-backtrack --plain
git-backtrack --timezone
git-backtrack --email
git-backtrack --line-diffs
```

Graph order can be `topo`, `date`, `author-date`, or `first-parent`.

Inside the TUI, press `s` to toggle display settings at runtime.

## Per-email auth and signing

Store GitHub/GitLab tokens and a GPG private signing key per author email in Git config:

```sh
git-backtrack auth set --email alice@example.com --github-token ghp_xxx --gitlab-token glpat_xxx --gpg-private-key ~/.gnupg/alice.asc
git-backtrack auth set --email work@example.com --gpg-private-key ~/work.asc --local
git-backtrack auth list
git-backtrack auth get --email alice@example.com --show-tokens
```

Global Git config is used by default (`--global` is accepted for compatibility). Add `--local` to store entries only in the current repository. The TUI Settings screen also includes an Auth keys submenu for editing GitHub token, GitLab token, and importing a GPG private key per email. The key material is stored encoded, not encrypted, in Git config; auth output shows only the derived fingerprint/status. During rewrites, a configured per-email GPG private key overrides the default `user.signingkey` when signing commits for that email.

Rewrites now sign commits automatically when a per-email GPG (or SSH) key is configured for the commit author's email, even if global `commit.gpgsign` is not set to `true`. Commits authored by emails without a configured key remain unsigned unless `commit.gpgsign=true` is set globally. This keeps the no-sign default while letting opted-in accounts produce signed history without extra flags.

After applying rewrites in the TUI, the result screen lists configured accounts with GitHub or GitLab tokens so you can push the rewritten branch directly instead of pushing manually.

## JSON Tool Mode

Agents and scripts can use the JSON command interface instead of driving the TUI:

```sh
git-backtrack help --json
git-backtrack list --path . --json
git-backtrack validate --path . --plan plan.json --json
git-backtrack apply --path . --plan plan.json --json --yes
git-backtrack backups --path . --json
git-backtrack restore --path . --json --yes --backup 20250101-120000
```

`list` returns the 5 newest commits by default to keep responses small. Pass `--limit N` to request a different count, `--offset N` to skip newer commits (paginate), `--stats` to include per-commit `additions`/`deletions`, or `--limit 0` (or `--all`) for every reachable commit. The response includes `total` (reachable count), `limit` (applied cap, omitted when uncapped), `offset` (skipped newest count, omitted when 0), `remaining` (commits beyond the returned window, omitted when 0), and `truncated` (true when the response was capped or offset):

```sh
git-backtrack list --path . --json --limit 20
git-backtrack list --path . --json --offset 5          # next page after the default 5
git-backtrack list --path . --json --stats             # include additions/deletions per commit
git-backtrack list --path . --json --all
```

Add `--compact` to any JSON command to emit the response on a single line with no indentation (useful when piping into `jq -c` or another agent):

```sh
git-backtrack help --json --compact
git-backtrack list --path . --json --compact
```

`backups` lists every `refs/backtrack-backup/<timestamp>` prefix in the repository:

```json
{
  "ok": true,
  "backups": [
    {"name": "20250101-120000", "ref": "refs/backtrack-backup/20250101-120000", "created_at": "2025-01-01T12:00:00Z"}
  ]
}
```

`restore` re-points branch refs from a backup. It requires `--yes`. Without `--backup`, the latest backup is chosen. `--backup` accepts a bare name (`20250101-120000`), the full prefix ref (`refs/backtrack-backup/20250101-120000`), or a ref with branch suffix (`refs/backtrack-backup/20250101-120000/main`). Pass `--ref main` to check out a branch after restoring.

```sh
git-backtrack restore --path . --json --yes --backup 20250101-120000 --ref main
```

Plans require an expected head so stale rewrites are rejected:

```json
{
  "version": 1,
  "ref": "main",
  "expected_head": "0123456789abcdef0123456789abcdef01234567",
  "operations": [
    {
      "op": "edit",
      "hash": "89abcde",
      "message": "Improve release notes"
    },
    {
      "op": "drop",
      "hash": "fedcba9"
    },
    {
      "op": "fold",
      "hashes": ["1111111", "2222222"],
      "anchor": "1111111"
    }
  ]
}
```

Hashes may be full 40-character hashes or unambiguous hex prefixes of at least 7 characters.

### Warnings

`validate` and `apply` responses may include informational `warnings` entries. Warnings never block the rewrite — they surface potential mistakes the caller should review.

- `date_before_parent` — an edited `author_date` is earlier than the parent commit's `author_date`.
- `date_after_child` — an edited `author_date` is later than a child commit's `author_date`.
- `empty_edit` — an `edit` operation did not change any fields.

## Safety

History rewrites are only applied after validation. `apply` requires explicit confirmation, checks the expected head, and creates a backup ref before changing history.
