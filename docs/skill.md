# diskcli Skill

---
name: diskcli
description: CLI for operating cloud drives (currently Quark pan only) — list, upload, download, search, rename, share, and extract files non-interactively. Use when the task involves files on a cloud disk.
version: 1.0.0
---

> 中文版见 [skill-zh.md](skill-zh.md)。

## Overview

`diskcli` is a multi-cloud-disk command-line tool written in Go. Only the
**Quark pan** provider is currently implemented; others (baidu / aliyun /
onedrive / google-drive / lanzou / tianyi) are planned behind the same
interface.

## Setup

### Install

Install a prebuilt binary from GitHub Releases (recommended — no Go toolchain
required). Assets are named `diskcli-<os>-<arch>[.exe]`, covering
linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, and windows/amd64:

```bash
# Discover the latest release tag (requires curl + jq)
VERSION=$(curl -fsSL https://api.github.com/repos/phpgao/diskcli/releases/latest | jq -r .tag_name)

# macOS / Linux (adjust OS/ARCH as needed)
curl -fsSL -o diskcli \
  "https://github.com/phpgao/diskcli/releases/download/${VERSION}/diskcli-darwin-arm64"
chmod +x diskcli && sudo mv diskcli /usr/local/bin/   # or anywhere on PATH

# Windows (PowerShell)
curl.exe -fsSL -o diskcli.exe `
  "https://github.com/phpgao/diskcli/releases/download/${VERSION}/diskcli-windows-amd64.exe"
```

Alternatively, install with Go or build from source:

```bash
go install github.com/phpgao/diskcli/cmd/diskcli@latest
# or build from source
go build -o diskcli ./cmd/diskcli
```

Verify the installation with `diskcli version`.

### Authentication (Quark cookie)

The Quark cookie must contain **both `__pus` and `__puus`** — a missing
`__puus` silently breaks upload/download callback auth. Source priority
(high → low):

1. `--qk-cookie` CLI flag — always wins, bypasses account selection
2. `QK_COOKIE` env var — overrides the active account's cookie
3. Config file `~/.diskcli/config` (TOML) — `[account.<name>]` section selected
   by `DISK_ACCOUNT` env, or top-level keys when unset

#### How to obtain the Quark cookie

The cookie comes from a logged-in Quark web session. Ask the user to provide
it — agents cannot (and must not try to) log in or extract credentials
themselves.

1. Open <https://pan.quark.cn> in a desktop browser (Chrome / Edge / Safari)
   and sign in.
2. Open DevTools: press `F12` (macOS Safari: `Cmd+Option+I`).
3. Go to the **Network** tab, then refresh the page (`F5`) so requests are
   captured.
4. Click any request to `pan.quark.cn` (e.g. the document load or an XHR to
   `/api/...`).
5. In **Request Headers** (Safari: under the request detail), find the
   `cookie:` header and copy its **entire value**.
6. Verify it contains both `__pus=` and `__puus=` before using it.

Alternative: DevTools → **Application** tab → **Cookies** →
`https://pan.quark.cn` → copy the values of `__pus` and `__puus` and join
them as `__pus=<value>; __puus=<value>`.

Then configure it:

```bash
# One-off (flag)
diskcli --qk-cookie "__pus=xxx; __puus=yyy" info

# Session (env)
export QK_COOKIE="__pus=xxx; __puus=yyy"
diskcli info
```

```toml
# ~/.diskcli/config — persistent
qk_cookie = "__pus=xxx; __puus=yyy"   # top-level keys = default account
upload_threads = 4

[account.work]                          # switch via: DISK_ACCOUNT=work diskcli ...
qk_cookie = "work-cookie-value"
```

**Treat the cookie as a secret credential.** Never log it, echo it into
shared output, or commit it to version control.

**Cookies expire.** A cookie typically survives days to weeks depending on
Quark session policy; if `diskcli info` fails with an auth error, ask the
user for a fresh cookie using the steps above.

**Always verify auth before batch operations:** `diskcli info` returns the
nickname / member type / capacity, and fails fast on an expired cookie.

## Global flags

| Flag | Meaning |
|------|---------|
| `-v` | Debug logging (slog, stderr) |
| `--log-json` | Emit logs as JSON |
| `--qk-cookie <string>` | Quark cookie (highest priority) |
| `--config <path>` | Config file path (default `~/.diskcli/config`) |
| `--provider <name>` | Cloud-disk provider (only `quark` currently) |
| `--qk-user-agent` / `--qk-referer` / `--qk-origin` | Custom browser headers |

## Command reference

### Read-only commands

```bash
diskcli info                              # account info (nickname/capacity)
diskcli ls /                              # list dir (names only)
diskcli ls /backup -o wide                # table: type/size/time/fid/name
diskcli ls / -o json                      # machine-readable output
diskcli tree /backup -L 2 -d -h           # tree: max depth / dirs only / sizes
diskcli search "report"                   # whole drive
diskcli search "invoice" /work -o json    # within a directory
diskcli version -o json                   # version/commit/date/go
```

### Directory operations

```bash
diskcli mkdir /backup/photos
diskcli mv /file.txt /backup/             # move into existing dir
diskcli mv /file.txt /backup/new.txt      # rename via move
diskcli cp /file.txt /backup/
diskcli rm /old.txt -y                    # -y skips confirmation
diskcli rename /old.txt new.txt
```

### Rename with undo

```bash
diskcli rename /file.txt new.txt --undo -y   # record + auto-keep
diskcli brename /photos --match ".jpeg" --to ".jpg" --dry-run   # preview only
diskcli brename /photos --match 'photo_(\d+)' --to 'img_$1' --regex -y
diskcli brename /docs --lower -y
diskcli brename /docs --match "draft" --to "final" --undo       # rollback on reject
```

Modes: `--match`+`--to` (literal), `--regex` (with `$1` capture groups,
single-quote `--to` to avoid shell expansion), `--upper`, `--lower`.
Flags: `--dry-run` (preview), `--undo` (record + confirm/rollback), `-y`.

### Transfer

```bash
# Upload
diskcli upload ./file.txt /backup/file.txt
diskcli upload ./folder /backup/folder --threads 8          # recursive
diskcli upload ./file.txt /backup/ --on-conflict overwrite -y
diskcli upload ./big.iso /backup/ --resume

# Download
diskcli download /backup/file.txt ./file.txt
diskcli download /backup/folder ./folder --resume
diskcli download /backup/file.txt ./ --on-conflict rename
```

Transfer flags: `--threads` (chunk concurrency, 0 = config/server-suggested,
default 3), `--file-threads` (per-file concurrency in a folder, default 1 to
avoid rate limits), `--on-conflict` `skip` (default) / `overwrite` / `rename`,
`-y` (skip overwrite confirm), `--resume`.

### Share management

```bash
diskcli share save "https://pan.quark.cn/s/abc123 提取码:xxxx" /saved/
diskcli share save "https://pan.quark.cn/s/abc123" /saved/ --passcode 1234
diskcli share create /file.txt --need-passcode --passcode 1234 --expire 3
diskcli share list --page 1 --page-size 50 -o json
diskcli share delete <share-id> -y
```

`--expire`: 1=permanent, 2=1 day, 3=7 days, 4=30 days. A permanent share
shows as `permanent` in table output.

### Cloud extraction

```bash
diskcli unarchive /backup/data.zip                 # default conflict: rename
diskcli unarchive /backup/data.zip --conflict overwrite
```

Supports zip / rar / 7z / tar / gz. Extracted files land in the
"夸克云解压" folder at the drive root.

## Agent guidance

1. **Parse with `-o json`.** `ls`, `search`, `info`, `share list`, `version`
   support `-o json|yaml`. Prefer JSON over table output when you need to
   extract fields; table output is for humans.
2. **Run non-interactively.** Commands that mutate state (`rm`, `share delete`,
   overwrite) prompt for confirmation. Always pass `-y` (or `--yes`) in
   non-interactive sessions, or the command will hang.
3. **Prefer `--dry-run` first.** For `brename`, preview changes with
   `--dry-run` before applying, especially with `--regex`.
4. **Prefer `--undo` for renames.** `rename --undo` and `brename --undo`
   record the previous state to a temp file and offer rollback — safer for
   batch operations.
5. **Destructive ops need user consent.** `rm` deletes cloud files
   permanently. Confirm with the user before running `rm` or
   `share delete`, even with `-y` available.
6. **Check auth first.** If a command fails with an auth/cookie error, run
   `diskcli info` to confirm, then ask the user for a fresh cookie
   (must contain `__pus` and `__puus`).
7. **Rate limits.** Keep `--file-threads` at the default 1 for folder
   uploads; raising it can trigger provider rate limiting.
8. **Trailing slash semantics.** A remote path ending in `/` means "into the
   directory" (keeps the source filename); a full path means "upload/copy as
   this exact name".

## Common workflows

```bash
# Health check + explore
diskcli info && diskcli ls / -o json

# Find and download a file
diskcli search "report" / -o json
diskcli download /reports/report_2026.pdf ./

# Upload a project backup (skip existing)
diskcli upload ./project /backup/project --on-conflict skip -y

# Save a shared link, then extract the archive
diskcli share save "https://pan.quark.cn/s/abc123 提取码:xxxx" /saved/ -y
diskcli unarchive /saved/data.zip --conflict rename
```

## Troubleshooting

| Symptom | Likely cause / fix |
|---------|--------------------|
| Auth error on any command | Cookie expired — get a fresh one from the browser (needs `__pus` + `__puus`) |
| Upload succeeds but download of it fails | Cookie missing `__puus` (callback auth breaks) |
| Upload/download hangs | Interactive confirmation waiting — add `-y` |
| `同名冲突` warnings during folder upload | Normal: the target directory already exists; upload proceeds |
| 4xx rate-limit errors | Lower `--threads` / keep `--file-threads 1` |
