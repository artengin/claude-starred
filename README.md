# Starred for Claude Code

[![ci](https://github.com/artengin/claude-starred/actions/workflows/ci.yml/badge.svg)](https://github.com/artengin/claude-starred/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/artengin/claude-starred)](https://github.com/artengin/claude-starred/releases/latest)

Bookmarks for your Claude Code sessions.

Star the sessions worth coming back to with `/star`, then find and reopen them from one small tree in your terminal. Starred sessions survive Claude Code's automatic cleanup.

```
 ★ Starred
 ──────────────────────────────────────

 > my-app
   api-server
   web-client
```

`Enter` opens a project:

```
 ★ my-app
 ──────────────────────────────────────

 > ● Checkout flow redesign
     Payment retries
     Price migration
```

## Why

- **Good sessions get lost.** After a few weeks there are dozens of sessions across projects, and the one with the right context is hard to find.
- **Old sessions disappear.** Claude Code deletes sessions after 30 days by default, including the ones you meant to come back to.

`starred` keeps only the sessions you chose, under names you gave them, for as long as you need them.

## Features

- `/star` inside any Claude Code session adds it to the list and asks for a name.
- One tree for all projects: project first, then its sessions. Git worktrees are grouped under their main repository.
- `Enter` reopens a session right in your current terminal, in its original directory.
- Starred sessions are kept safe from Claude Code's cleanup.
- Rename, unstar and search without leaving the keyboard.
- Linux, macOS and Windows. English and Russian interface.

## Quick start

1. Install (Linux and macOS):

   ```sh
   curl -fsSL https://raw.githubusercontent.com/artengin/claude-starred/main/install.sh | sh
   ```

   This installs `starred` into `~/.local/bin` and the `/star` skill into Claude Code.

2. In a Claude Code session you want to keep, run:

   ```
   /star
   ```

   or give the name right away: `/star Checkout flow redesign`.

3. Later, in any terminal:

   ```sh
   starred
   ```

**Windows:** download `claude-starred_windows_amd64.zip` from [releases](https://github.com/artengin/claude-starred/releases), put `starred.exe` on your `PATH` and run `starred install`.

**With Go:** `go install github.com/artengin/claude-starred/cmd/starred@latest && starred install`.

## Keys

| Key | Action |
|---|---|
| `j` / `k`, arrows | down / up |
| `l`, `Enter`, `→` | open |
| `h`, `Esc`, `←` | clear the search, or go back |
| `/` | search |
| `g` / `G` | first / last |
| `r` | rename |
| `d` | unstar (asks for confirmation) |
| `p` | toggle directory names / full paths |
| `?` | help |
| `q` | quit |

`●` marks a session that is running right now. Opening it asks for confirmation, because two Claude processes on one session write to the same transcript.

## Good to know

- Claude Code deletes old sessions after `cleanupPeriodDays`. Starred sessions are kept: `starred` brings them back, so `claude --resume` keeps working. Subagent history and file checkpoints of old sessions are still removed by Claude.
- The kept copy is a hard link of the transcript, so it always matches. When a hard link is impossible (the data directory is on another filesystem), `/star` warns that the copy is a snapshot; it is refreshed every time you open `starred`.
- `starred` never changes Claude Code's settings or transcripts. Names are stored separately, so they don't show up in Claude's `/resume`.
- The interface language follows `LANG`.
- Claude Code's session format is undocumented, so a Claude Code update may break `starred`.

## Update and uninstall

```sh
starred update
starred uninstall
```

`update` replaces the binary and refreshes the `/star` skill. `uninstall` removes the `/star` skill, the starred list with its kept copies, and the binary. If Claude has already deleted some starred sessions, you are warned that they will be lost.

## Data

Starred sessions and their kept copies are stored in:

- Linux: `~/.local/share/claude-starred` (or `$XDG_DATA_HOME/claude-starred`)
- macOS: `~/Library/Application Support/claude-starred`
- Windows: `%LOCALAPPDATA%\claude-starred`

## License

This project is open-source software licensed under the [MIT license](LICENSE.md).
