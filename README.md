# diskcli

A multi-cloud-disk command-line tool written in Go. Currently only **Quark pan**
is supported; baidu / aliyun / onedrive / google-drive / lanzou / tianyi are
planned via the provider interface.

> Project site: https://phpgao.github.io/diskcli/

## Features

- **Multi-provider architecture** — `provider.Provider` interface abstracts the
  cloud disk; adding a new one only requires implementing the interface.
- **File & folder upload/download** — chunked concurrent upload (configurable
  threads), resumable transfer, streaming download.
- **Conflict policy** — `skip` / `overwrite` / `rename` on name collision.
- **Directory ops** — `ls` / `mkdir` / `mv` / `cp` / `rm` / `rename`.
- **Search** — full-text search by file name across the drive or within a
  directory.
- **Batch rename** — literal / regex / case transforms with dry-run preview
  and undoable confirmation.
- **Undoable rename** — `rename --undo` records the change to a temp file and
  prompts for keep/rollback.
- **Share management** — save, create, list, delete shares.
- **kubectl-style output** — `-o json|yaml|table|wide`.
- **Multiple cookie sources** — `--qk-cookie` flag > `QK_COOKIE` env >
  `~/.diskcli/config`.
- **Custom browser headers** — `--qk-user-agent` / `--qk-referer` /
  `--qk-origin` flags, `QK_USER_AGENT` / `QK_REFERER` / `QK_ORIGIN` env vars,
  or `qk_user_agent` / `qk_referer` / `qk_origin` config keys.
- **Structured logging** — `-v` enables `slog` debug logs on stderr.
- **Decoupled protocol & tool** — `Doer` interface abstracts HTTP for unit tests.

## Install

```bash
go install github.com/jimmy/diskcli/cmd/diskcli@latest
```

Build from source:

```bash
git clone <repo>
cd diskcli
go build -o diskcli ./cmd/diskcli
```

## Configuration

Config file: `~/.diskcli/config` (TOML format).

### Priority (high → low)

1. `--qk-cookie` CLI flag — always wins, bypasses account selection
2. `QK_COOKIE` env var — overrides the active account's cookie
3. Config file — `[account.<name>]` section selected by `DISK_ACCOUNT` env, or top-level keys when `DISK_ACCOUNT` is unset

### TOML format

```toml
# Top-level keys = [default] account
qk_cookie = "__pus=xxx; __puus=yyy"
upload_threads = 4

# Optional custom browser headers
qk_user_agent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36"

# Named accounts selected via DISK_ACCOUNT env var
[account.work]
qk_cookie = "work-cookie-value"

[account.personal]
qk_cookie = "personal-cookie-value"
```

### Multi-account

```bash
# Use default account
diskcli info

# Switch to [account.work]
DISK_ACCOUNT=work diskcli info

# Override with --qk-cookie (ignores DISK_ACCOUNT)
diskcli --qk-cookie "xxx" share save "https://pan.quark.cn/s/abc" /dst/
```

> The Quark cookie must contain both `__pus` and `__puus`; missing `__puus`
> breaks upload/download callback auth.
```

## Usage

### User info

```bash
diskcli info                     # nickname / member type / capacity
diskcli -v info                  # with debug logs
diskcli info -o json             # JSON output
diskcli info -o yaml             # YAML output
```

### Directory listing

```bash
diskcli ls /                     # list root (names only)
diskcli ls /backup -o wide       # long table (type/size/time/fid/name)
diskcli ls / -o json             # JSON (pipe-friendly)
diskcli ls / -o yaml             # YAML
```

### Directory management

```bash
diskcli tree                          # print the directory tree from root
diskcli tree /backup                  # from a given path
diskcli tree /backup -L 2             # max depth
diskcli tree /backup -d               # directories only
diskcli tree /backup -h               # show file sizes
diskcli tree /backup -C 10            # traverse 10 directories in parallel (default 5)
diskcli mkdir /backup/photos
diskcli mv /file.txt /backup/           # move into existing dir
diskcli mv /file.txt /backup/new.txt    # rename via move (same dir)
diskcli cp /file.txt /backup/           # copy into existing dir
diskcli cp /file.txt /backup/copy.txt   # copy with rename (same dir)
diskcli rm /old.txt -y                  # skip confirm
diskcli rename /old.txt new.txt
```

### Search

```bash
diskcli search "report"                # whole drive
diskcli search "invoice" /work         # within a directory
diskcli search "backup" / -o json      # JSON output
```

### Rename with undo

```bash
# rename and record to a temp file; confirm or rollback
diskcli rename /file.txt new.txt --undo
#   → renamed: /file.txt -> /new.txt
#   → confirm to keep? (y=keep, n=rollback)
# y / n

# auto-confirm (keep)
diskcli rename /file.txt new.txt --undo -y
```

### Batch rename

```bash
# literal replace
diskcli brename /photos --match ".jpeg" --to ".jpg" -y

# regex with capture groups (single-quote --to to avoid shell expansion)
diskcli brename /photos --match 'photo_(\d+)' --to 'img_$1' --regex -y

# case transforms
diskcli brename /docs --upper -y
diskcli brename /docs --lower --dry-run

# undoable batch (rollback on reject)
diskcli brename /docs --match "draft" --to "final" --undo
```

Modes: `--match`+`--to` (literal), `--regex` (regex with `$1` capture groups),
`--upper`, `--lower`. Flags: `--dry-run` (preview only), `--undo` (record +
confirm/rollback), `-y` (skip prompts).

### Upload

```bash
# single file
diskcli upload ./file.txt /backup/file.txt

# upload into a directory (uses local filename)
diskcli upload ./file.txt /backup/

# folder (recursive)
diskcli upload ./folder /backup/folder --threads 8

# overwrite on conflict
diskcli upload ./file.txt /backup/ --on-conflict overwrite -y

# resumable
diskcli upload ./big.iso /backup/ --resume
```

Flags:

- `--threads` — chunk concurrency (0 = config or server-suggested, default 3)
- `--file-threads` — per-file concurrency inside a folder (default 1, avoid rate limit)
- `--on-conflict` — `skip` (default) / `overwrite` / `rename`
- `--yes` / `-y` — skip confirm before overwrite
- `--resume` — resumable upload

### Download

```bash
diskcli download /backup/file.txt ./file.txt
diskcli download /backup/folder ./folder --resume
diskcli download /backup/file.txt ./ --on-conflict rename
```

### Share

```bash
# save (transfer) a shared link to local dir
diskcli share save "https://pan.quark.cn/s/abc123 提取码:xxxx" /saved/

# create a share
diskcli share create /backup/file.txt
diskcli share create /file.txt --need-passcode --passcode 1234 --expire 3

# list my shares (default page 1, page size 50)
diskcli share list

# list with explicit pagination
diskcli share list --page 2 --page-size 100

# delete a share
diskcli share delete <share-id> -y
```

> `share list` fetches one page at a time. The table prints the expiry as a
> timestamp; a permanent share is shown as `permanent`. Use `-o json`/`-o yaml`
> for the full `expires_at`/`created_at` fields.

## Architecture

```
internal/
├── provider/   generic cloud-disk interface (Provider/Sharer + shared types)
├── quark/      Quark protocol layer (Doer/CookieStore/5-step upload/share) + Adapter
├── transfer/   orchestration (conflict policy, folder recursion)
├── config/     cookie source merging
├── cli/        cobra command layer
└── pkg/logx/   slog wrapper
```

### Adding a new provider

1. Create `internal/<name>/` and implement the protocol layer.
2. Create `internal/<name>/adapter.go` implementing `provider.Provider`
   (and `provider.Sharer` if the provider supports sharing).
3. Register it in `internal/cli/provider.go` `newProvider`.

The `transfer` and `cli` layers require no changes.

## License

MIT
