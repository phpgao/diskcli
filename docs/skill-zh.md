# diskcli Skill（中文版）

---
name: diskcli-zh
description: 云盘操作 CLI（当前仅支持夸克网盘）——非交互式地列出、上传、下载、搜索、重命名、分享、解压文件。任务涉及云盘文件时使用本 skill。
version: 1.0.0
---

> 英文版见 [skill.md](skill.md)。

## 概述

`diskcli` 是一个 Go 编写的多云盘命令行工具。当前仅实现**夸克网盘** provider；
百度 / 阿里云 / onedrive / google-drive / 蓝奏 / 天翼均规划在同一接口之下。

## 安装与配置

### 安装

推荐从 GitHub Releases 下载预编译二进制（无需 Go 工具链）。产物命名为
`diskcli-<os>-<arch>[.exe]`，覆盖 linux/amd64、linux/arm64、darwin/amd64、
darwin/arm64、windows/amd64：

```bash
# 获取最新 release tag（需要 curl + jq）
VERSION=$(curl -fsSL https://api.github.com/repos/phpgao/diskcli/releases/latest | jq -r .tag_name)

# macOS / Linux（按需调整 OS/ARCH）
curl -fsSL -o diskcli \
  "https://github.com/phpgao/diskcli/releases/download/${VERSION}/diskcli-darwin-arm64"
chmod +x diskcli && sudo mv diskcli /usr/local/bin/   # 或任意 PATH 目录

# Windows（PowerShell）
curl.exe -fsSL -o diskcli.exe `
  "https://github.com/phpgao/diskcli/releases/download/${VERSION}/diskcli-windows-amd64.exe"
```

备选方式：Go 安装或源码构建：

```bash
go install github.com/phpgao/diskcli/cmd/diskcli@latest
# 或源码构建
go build -o diskcli ./cmd/diskcli
```

用 `diskcli version` 验证安装。

### 认证（夸克 cookie）

夸克 cookie 必须同时包含 **`__pus` 和 `__puus`**——缺少 `__puus` 会静默破坏
上传/下载的回调认证。来源优先级（高 → 低）：

1. `--qk-cookie` CLI flag——总是最高优先级，绕过账号选择
2. `QK_COOKIE` 环境变量——覆盖当前账号的 cookie
3. 配置文件 `~/.diskcli/config`（TOML）——通过 `DISK_ACCOUNT` 环境变量选择
   `[account.<name>]` 段；未设置时使用顶层 key

#### 如何获取夸克 cookie

cookie 来自已登录的夸克网页会话。请让用户提供——agent 不能（也绝对不要
尝试）自行登录或提取凭据。

1. 在桌面浏览器（Chrome / Edge / Safari）打开 <https://pan.quark.cn> 并登录。
2. 打开开发者工具：按 `F12`（macOS Safari：`Cmd+Option+I`）。
3. 切到 **Network（网络）** 标签，刷新页面（`F5`）以捕获请求。
4. 点击任意一条发往 `pan.quark.cn` 的请求（如文档加载或 `/api/...` XHR）。
5. 在 **Request Headers（请求标头）**（Safari：请求详情下方）找到
   `cookie:` 标头，复制**完整值**。
6. 使用前确认同时包含 `__pus=` 和 `__puus=`。

备选方式：开发者工具 → **Application（应用）** 标签 → **Cookies** →
`https://pan.quark.cn` → 复制 `__pus` 和 `__puus` 的值，
拼成 `__pus=<value>; __puus=<value>`。

然后配置：

```bash
# 一次性（flag）
diskcli --qk-cookie "__pus=xxx; __puus=yyy" info

# 会话级（环境变量）
export QK_COOKIE="__pus=xxx; __puus=yyy"
diskcli info
```

```toml
# ~/.diskcli/config —— 持久化
qk_cookie = "__pus=xxx; __puus=yyy"   # 顶层 key = 默认账号
upload_threads = 4

[account.work]                          # 切换方式：DISK_ACCOUNT=work diskcli ...
qk_cookie = "work-cookie-value"
```

**把 cookie 当作机密凭据。** 不要写日志、输出到共享内容或提交进版本库。

**cookie 会过期。** 视夸克会话策略，cookie 通常存活数天到数周；
`diskcli info` 报认证错误时，按上述步骤向用户索取新 cookie。

**批量操作前先验证认证：** `diskcli info` 返回昵称 / 会员类型 / 容量，
cookie 过期时快速失败。

## 全局 flag

| Flag | 含义 |
|------|------|
| `-v` | Debug 日志（slog，输出到 stderr） |
| `--log-json` | 日志以 JSON 输出 |
| `--qk-cookie <string>` | 夸克 cookie（最高优先级） |
| `--config <path>` | 配置文件路径（默认 `~/.diskcli/config`） |
| `--provider <name>` | 云盘 provider（当前仅 `quark`） |
| `--qk-user-agent` / `--qk-referer` / `--qk-origin` | 自定义浏览器标头 |

## 命令参考

### 只读命令

```bash
diskcli info                              # 账号信息（昵称/容量）
diskcli ls /                              # 列目录（仅名称）
diskcli ls /backup -o wide                # 表格：type/size/time/fid/name
diskcli ls / -o json                      # 机器可读输出
diskcli tree /backup -L 2 -d -h           # 树：最大深度 / 仅目录 / 显示大小
diskcli search "report"                   # 全盘搜索
diskcli search "invoice" /work -o json    # 目录内搜索
diskcli version -o json                   # 版本/commit/日期/go
```

### 目录操作

```bash
diskcli mkdir /backup/photos
diskcli mv /file.txt /backup/             # 移入已有目录
diskcli mv /file.txt /backup/new.txt      # 通过 move 改名
diskcli cp /file.txt /backup/
diskcli rm /old.txt -y                    # -y 跳过确认
diskcli rename /old.txt new.txt
```

### 可撤销重命名

```bash
diskcli rename /file.txt new.txt --undo -y   # 记录 + 自动保留
diskcli brename /photos --match ".jpeg" --to ".jpg" --dry-run   # 仅预览
diskcli brename /photos --match 'photo_(\d+)' --to 'img_$1' --regex -y
diskcli brename /docs --lower -y
diskcli brename /docs --match "draft" --to "final" --undo       # 拒绝时回滚
```

模式：`--match`+`--to`（字面量）、`--regex`（支持 `$1` 捕获组，
`--to` 用单引号包裹以防 shell 展开）、`--upper`、`--lower`。
Flag：`--dry-run`（预览）、`--undo`（记录 + 确认/回滚）、`-y`。

### 传输

```bash
# 上传
diskcli upload ./file.txt /backup/file.txt
diskcli upload ./folder /backup/folder --threads 8          # 递归
diskcli upload ./file.txt /backup/ --on-conflict overwrite -y
diskcli upload ./big.iso /backup/ --resume

# 下载
diskcli download /backup/file.txt ./file.txt
diskcli download /backup/folder ./folder --resume
diskcli download /backup/file.txt ./ --on-conflict rename
```

传输 flag：`--threads`（分片并发，0 = 配置值/服务端建议值，默认 3）、
`--file-threads`（文件夹内单文件并发，默认 1 以避免限流）、
`--on-conflict` `skip`（默认）/ `overwrite` / `rename`、
`-y`（跳过覆盖确认）、`--resume`。

### 分享管理

```bash
diskcli share save "https://pan.quark.cn/s/abc123 提取码:xxxx" /saved/
diskcli share save "https://pan.quark.cn/s/abc123" /saved/ --passcode 1234
diskcli share create /file.txt --need-passcode --passcode 1234 --expire 3
diskcli share list --page 1 --page-size 50 -o json
diskcli share delete <share-id> -y
```

`--expire`：1=永久、2=1 天、3=7 天、4=30 天。永久分享在表格输出中
显示为 `permanent`。

### 云解压

```bash
diskcli unarchive /backup/data.zip                 # 默认冲突策略：rename
diskcli unarchive /backup/data.zip --conflict overwrite
```

支持 zip / rar / 7z / tar / gz。解压产物落在网盘根目录的
「夸克云解压」文件夹。

## Agent 守则

1. **用 `-o json` 解析。** `ls`、`search`、`info`、`share list`、`version`
   支持 `-o json|yaml`。需要提取字段时优先 JSON；表格输出是给人看的。
2. **非交互运行。** 会改状态的命令（`rm`、`share delete`、覆盖）会弹确认。
   非交互会话必须传 `-y`（或 `--yes`），否则命令会挂起。
3. **先 `--dry-run`。** `brename` 尤其是配合 `--regex` 时，先预览再应用。
4. **重命名优先 `--undo`。** `rename --undo` 和 `brename --undo` 会把先前
   状态记录到临时文件并支持回滚——批量操作更安全。
5. **破坏性操作需用户同意。** `rm` 会永久删除云盘文件。即使有 `-y`，
   执行 `rm` 或 `share delete` 前也要先征得用户确认。
6. **先查认证。** 命令报认证/cookie 错误时，先跑 `diskcli info` 确认，
   再按上述步骤向用户索取新 cookie（必须含 `__pus` 和 `__puus`）。
7. **限流。** 文件夹上传保持 `--file-threads` 为默认 1；调高可能触发
   provider 限流。
8. **尾斜杠语义。** 远端路径以 `/` 结尾表示"移入该目录"（保留源文件名）；
   完整路径表示"上传/复制为该确切名称"。

## 常见工作流

```bash
# 健康检查 + 浏览
diskcli info && diskcli ls / -o json

# 搜索并下载文件
diskcli search "report" / -o json
diskcli download /reports/report_2026.pdf ./

# 上传项目备份（跳过已存在）
diskcli upload ./project /backup/project --on-conflict skip -y

# 转存分享链接，再解压
diskcli share save "https://pan.quark.cn/s/abc123 提取码:xxxx" /saved/ -y
diskcli unarchive /saved/data.zip --conflict rename
```

## 故障排查

| 症状 | 可能原因 / 处理 |
|------|----------------|
| 任意命令报认证错误 | cookie 过期——从浏览器重新获取（需 `__pus` + `__puus`） |
| 上传成功但下载失败 | cookie 缺 `__puus`（回调认证失效） |
| 上传/下载挂起 | 等待交互确认——加 `-y` |
| 文件夹上传时出现 `同名冲突` 警告 | 正常：目标目录已存在，上传会继续 |
| 4xx 限流错误 | 降低 `--threads` / 保持 `--file-threads 1` |
