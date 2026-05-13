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
git-backtrack --plain
git-backtrack --timezone
git-backtrack --email
git-backtrack --line-diffs
```

Inside the TUI, press `s` to toggle display settings at runtime.

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
