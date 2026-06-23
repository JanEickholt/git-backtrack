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

After applying rewrites in the TUI, the result screen lists configured accounts with GitHub or GitLab tokens so you can push the rewritten branch directly instead of pushing manually.

## JSON Tool Mode

Agents and scripts can use the JSON command interface instead of driving the TUI:

```sh
git-backtrack help --json
git-backtrack list --path . --json
git-backtrack validate --path . --plan plan.json --json
git-backtrack apply --path . --plan plan.json --json --yes
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

## MCP Server

Run `git-backtrack` as an MCP stdio server when an AI client supports MCP tools:

```sh
git-backtrack mcp
```

Example client configuration:

```json
{
  "mcpServers": {
    "git-backtrack": {
      "command": "/path/to/git-backtrack",
      "args": ["mcp"]
    }
  }
}
```

The MCP server exposes these tools:

- `git_backtrack_help`
- `git_backtrack_list`
- `git_backtrack_validate`
- `git_backtrack_apply`

`validate` and `apply` accept either an inline `plan` object or a `plan_path`. `apply` requires `yes: true`.

## Safety

History rewrites are only applied after validation. `apply` requires explicit confirmation, checks the expected head, and creates a backup ref before changing history.
