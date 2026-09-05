# diskcli

基于 Golang 实现的多网盘命令行工具。目前仅支持**夸克网盘**，百度/阿里/OneDrive/Google Drive/蓝奏/天翼等已通过 provider 接口预留，未来按需扩展。

> 项目主页：https://phpgao.github.io/diskcli/

## 特性

- **多网盘架构** — 通过 `provider.Provider` 接口抽象网盘能力，新增网盘只需实现接口
- **文件/文件夹上传下载** — 分片并发上传（可配线程数）、断点续传、流式下载
- **冲突策略** — 同名冲突支持 `skip` / `overwrite` / `rename`
- **目录管理** — `ls` / `mkdir` / `mv` / `cp` / `rm` / `rename`
- **搜索** — 按文件名全文搜索，可限定目录范围
- **批量重命名** — 字面替换 / 正则捕获 / 大小写转换，支持 dry-run 预览与 undo 回滚
- **可撤销改名** — `rename --undo` 把改动写入临时文件，确认后保留或回滚
- **分享管理** — 转存、创建、列出、删除
- **kubectl 风格输出** — `-o json|yaml|table|wide`
- **Cookie 多来源** — `--qk-cookie` flag > `QK_COOKIE` 环境变量 > `~/.diskcli/config`
- **自定义浏览器头** — `--qk-user-agent` / `--qk-referer` / `--qk-origin` flag，
  `QK_USER_AGENT` / `QK_REFERER` / `QK_ORIGIN` 环境变量，
  或 `qk_user_agent` / `qk_referer` / `qk_origin` 配置项
- **结构化日志** — `-v` 开启 slog debug 日志（输出到 stderr）
- **协议库与工具解耦** — `Doer` interface 抽象 HTTP 传输，便于单测

## 安装

```bash
go install github.com/phpgao/diskcli/cmd/diskcli@latest
```

从源码构建：

```bash
git clone <repo>
cd diskcli
go build -o diskcli ./cmd/diskcli
```

## 配置

配置文件：`~/.diskcli/config`（TOML 格式）。

### 优先级（高 → 低）

1. `--qk-cookie` CLI flag — 直接指定，绕过所有账号选择
2. `QK_COOKIE` 环境变量 — 覆盖当前选中账号的 cookie
3. 配置文件 — `DISK_ACCOUNT` 环境变量选中 `[account.<name>]` 段落，未设置时用顶层 key（default 账号）

### TOML 格式

```toml
# 顶层 key = [default] 账号
qk_cookie = "__pus=xxx; __puus=yyy"
upload_threads = 4

# 可选：自定义浏览器头
qk_user_agent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36"

# 通过 DISK_ACCOUNT 环境变量选中的命名账号
[account.work]
qk_cookie = "工作号-cookie"

[account.personal]
qk_cookie = "个人号-cookie"
```

### 多账号

```bash
# 使用默认账号
diskcli info

# 切换到 [account.work]
DISK_ACCOUNT=work diskcli info

# --qk-cookie 直接覆盖（忽略 DISK_ACCOUNT）
diskcli --qk-cookie "xxx" share save "https://pan.quark.cn/s/abc" /dst/
```

> 夸克 cookie 必须包含 `__pus` 与 `__puus`，仅 `__pus` 会导致上传/下载 callback 鉴权失败。

## 使用

### 用户信息

```bash
diskcli info                     # 昵称/会员类型/容量
diskcli -v info                  # 开启 debug 日志
diskcli info -o json             # JSON 输出
diskcli info -o yaml             # YAML 输出
```

### 列目录

```bash
diskcli ls /                     # 列根目录（仅文件名）
diskcli ls /backup -o wide       # 长格式表格（类型/大小/时间/fid/名称）
diskcli ls / -o json             # JSON 输出（管道友好）
diskcli ls / -o yaml             # YAML 输出
```

### 目录管理

```bash
diskcli tree                          # 从根目录打印目录树
diskcli tree /backup                  # 从指定路径
diskcli tree /backup -L 2             # 限制最大深度
diskcli tree /backup -d               # 仅目录
diskcli tree /backup -h               # 显示文件大小
diskcli tree /backup -C 10            # 并发遍历 10 个目录（默认 5）
diskcli mkdir /backup/photos
diskcli mv /file.txt /backup/           # 移到已存在的目录
diskcli mv /file.txt /backup/new.txt    # 同目录改名（mv 语义）
diskcli cp /file.txt /backup/           # 复制到已存在的目录
diskcli cp /file.txt /backup/copy.txt   # 同目录复制并改名
diskcli rm /old.txt -y                  # 跳过确认
diskcli rename /old.txt new.txt
```

### 搜索

```bash
diskcli search "report"                # 全盘搜索
diskcli search "invoice" /work         # 在指定目录内搜索
diskcli search "backup" / -o json      # JSON 输出
```

### 改名 + 撤销

```bash
# rename 并记录到临时文件；之后确认或回滚
diskcli rename /file.txt new.txt --undo
#   → renamed: /file.txt -> /new.txt
#   → confirm to keep? (y=keep, n=rollback)
# y / n

# 自动确认（保留）
diskcli rename /file.txt new.txt --undo -y
```

### 批量改名

```bash
# 字面替换
diskcli brename /photos --match ".jpeg" --to ".jpg" -y

# 正则 + 捕获组（--to 用单引号避免 shell 展开）
diskcli brename /photos --match 'photo_(\d+)' --to 'img_$1' --regex -y

# 大小写转换
diskcli brename /docs --upper -y
diskcli brename /docs --lower --dry-run

# 可撤销批量（拒绝时全部回滚）
diskcli brename /docs --match "draft" --to "final" --undo
```

模式：`--match`+`--to`（字面替换）、`--regex`（正则，支持 `$1` 捕获组）、
`--upper`、`--lower`。Flag：`--dry-run`（仅预览）、`--undo`（记录 + 确认/回滚）、
`-y`（跳过交互）。

### 上传

```bash
# 单文件
diskcli upload ./file.txt /backup/file.txt

# 上传到目录（自动用本地文件名）
diskcli upload ./file.txt /backup/

# 文件夹（递归）
diskcli upload ./folder /backup/folder --threads 8

# 覆盖同名
diskcli upload ./file.txt /backup/ --on-conflict overwrite -y

# 断点续传
diskcli upload ./big.iso /backup/ --resume
```

Flag：

- `--threads` — 分片并发度（0 用配置或服务端建议，默认 3）
- `--file-threads` — 文件夹内文件并发（默认 1，避免风控）
- `--on-conflict` — `skip`（默认）/ `overwrite` / `rename`
- `--yes` / `-y` — 覆盖前跳过确认
- `--resume` — 断点续传

### 下载

```bash
diskcli download /backup/file.txt ./file.txt
diskcli download /backup/folder ./folder --resume
diskcli download /backup/file.txt ./ --on-conflict rename
```

### 分享

```bash
# 转存分享
diskcli share save "https://pan.quark.cn/s/abc123 提取码:xxxx" /saved/

# 创建分享
diskcli share create /backup/file.txt
diskcli share create /file.txt --need-passcode --passcode 1234 --expire 3

# 列出我的分享（默认第 1 页，每页 50 条）
diskcli share list

# 显式翻页
diskcli share list --page 2 --page-size 100

# 删除分享
diskcli share delete <share-id> -y
```

> `share list` 每次只取一页。表格中有效期显示为时间戳；永久分享显示为
> `permanent`。加 `-o json`/`-o yaml` 可查看完整的 `expires_at`/`created_at` 字段。

## 架构

```
internal/
├── provider/   通用网盘接口（Provider/Sharer + 通用类型）
├── quark/      夸克协议层（Doer/CookieStore/五步上传/分享）+ Adapter
├── transfer/   业务编排（冲突策略、文件夹递归）
├── config/     Cookie 三来源合并
├── cli/        cobra 命令层
└── pkg/logx/   slog 封装
```

### 新增网盘

1. 创建 `internal/<name>/` 包，实现协议层
2. 创建 `internal/<name>/adapter.go`，实现 `provider.Provider`（如支持分享则实现 `provider.Sharer`）
3. 在 `internal/cli/provider.go` 的 `newProvider` 注册

`transfer` 与 `cli` 层零改动。

## License

MIT
