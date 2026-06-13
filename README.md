# ssh-tunnel-daemon

> 一个用于启动、停止、重启和监控 SSH 隧道的守护进程 CLI 工具。
>
> 支持 `local`（`-L`）和 `remote`（`-R`）两种隧道模式，通过 YAML 配置文件管理隧道，并为每个隧道生成独立的日志文件。

---

## 解决了什么问题？

在日常开发和运维中，经常需要建立多个 SSH 隧道来转发端口，例如：

- 本地调试时把远程数据库端口映射到本地
- 把本地服务暴露到远程跳板机

手动维护这些 `ssh -L` / `ssh -R` 命令既容易出错，也难以查看哪些隧道正在运行。`ssh-tunnel-daemon` 帮你把隧道配置持久化到 YAML 文件，通过一条命令即可启动、停止、重启和查看状态，并为每个隧道维护独立的 PID 与日志。

## 安装指南

### 通过 Homebrew 安装

```bash
brew tap northwang-lucky/tap
brew install ssh-tunnel-daemon
```

安装后可直接使用 `ssh-tunnel-daemon`、`sshtnl`、`s17n` 三个命令。

### 从源码构建（需要 mise 与 Go）

```bash
git clone https://github.com/northwang-lucky/ssh-tunnel-daemon.git
cd ssh-tunnel-daemon
mise install
mise run build
```

构建产物位于 `bin/ssh-tunnel-daemon`。你可以将其加入 `PATH`，或使用别名 `sshtnl` / `s17n`。

## 快速上手

以下是一个**从零到运行隧道**的完整演示：

```bash
# 1. 构建
mise run build

# 2. 启动并保存一条 local 隧道
./bin/ssh-tunnel-daemon start web -t user@example.com -p 8080,9090 -m local --save
# 输出：
# Started tunnel "web" (PID: 12345, log: ~/.local/state/ssh-tunnel-daemon/logs/web_20260101_120000.log)

# 3. 查看状态
./bin/ssh-tunnel-daemon status
# 输出：
# NAME         STATUS     PID      MODE         PORTS
# web          running    12345    local        8080,9090

# 4. 查看配置
./bin/ssh-tunnel-daemon config show

# 5. 停止隧道
./bin/ssh-tunnel-daemon stop web

# 6. 重启隧道
./bin/ssh-tunnel-daemon restart web
```

### 配置文件示例

`ssh-tunnel-daemon` 会在第一次需要时创建默认配置文件。你也可以手动编辑：

```yaml
tunnels:
  - name: web
    target: user@example.com
    mode: local
    ports:
      - 8080
      - 9090
  - name: db
    target: user@db.example.com
    mode: remote
    ports:
      - 3306
```

模式说明：

- `local`：对应 `ssh -L`，把远程端口转发到本地。
- `remote`：对应 `ssh -R`，把本地端口转发到远程。

## CLI 文档

### 全局说明

- 所有命令均可通过 `ssh-tunnel-daemon <command> --help` 查看帮助。
- 同时支持 `sshtnl` 与 `s17n` 作为快捷命令（例如 `sshtnl status`）。
- 配置文件默认保存在 `$XDG_CONFIG_HOME/ssh-tunnel-daemon/config.yaml`（回退到 `~/.config/ssh-tunnel-daemon/config.yaml`）。
- 状态文件（PID）保存在 `$XDG_STATE_HOME/ssh-tunnel-daemon/*.pid`（回退到 `~/.local/state/ssh-tunnel-daemon/*.pid`）。
- 日志文件保存在 `$XDG_STATE_HOME/ssh-tunnel-daemon/logs/*.log`。
- 日志默认保留 3 天，每次命令调用时自动懒清除过期日志。

### `ssh-tunnel-daemon start` — 启动隧道

启动一个 SSH 隧道守护进程。如果未指定隧道名称且终端为交互式，会弹出选择/创建界面。

**用法：**

```bash
ssh-tunnel-daemon start [tunnel_name] [flags]
```

**标志：**

| 标志 | 简写 | 说明 |
|------|------|------|
| `--target` | `-t` | SSH 目标（例如 `user@host`） |
| `--ports` | `-p` | 逗号分隔的端口号列表 |
| `--mode` | `-m` | 隧道模式：`local` 或 `remote`，默认为 `local` |
| `--save` | - | 将隧道定义持久化到配置文件 |

**示例：**

```bash
# 交互式选择或创建隧道
ssh-tunnel-daemon start

# 从配置文件启动已有隧道
ssh-tunnel-daemon start web

# 显式创建并保存一条 local 隧道
ssh-tunnel-daemon start web -t user@example.com -p 8080,9090 -m local --save

# 创建一条 remote 隧道
ssh-tunnel-daemon start api -t user@example.com -p 3000 -m remote --save
```

---

### `ssh-tunnel-daemon stop` — 停止隧道

停止一个或多个正在运行的 SSH 隧道。如果未指定名称，会弹出交互式多选界面。

**用法：**

```bash
ssh-tunnel-daemon stop [tunnel_name...]
```

**示例：**

```bash
# 停止单个隧道
ssh-tunnel-daemon stop web

# 停止多个隧道
ssh-tunnel-daemon stop web api

# 交互式选择
ssh-tunnel-daemon stop
```

---

### `ssh-tunnel-daemon restart` — 重启隧道

重启一个或多个 SSH 隧道。隧道定义必须从配置文件中存在。如果未指定名称，会弹出交互式多选界面。

**用法：**

```bash
ssh-tunnel-daemon restart [tunnel_name...]
```

**示例：**

```bash
# 重启单个隧道
ssh-tunnel-daemon restart web

# 交互式选择
ssh-tunnel-daemon restart
```

---

### `ssh-tunnel-daemon status` — 查看隧道状态

显示所有已配置隧道或单个指定隧道的运行状态。

**用法：**

```bash
ssh-tunnel-daemon status [tunnel_name]
```

**示例：**

```bash
# 查看所有隧道
ssh-tunnel-daemon status

# 查看单个隧道
ssh-tunnel-daemon status web
```

---

### `ssh-tunnel-daemon config show` — 显示配置

显示当前配置文件内容。如果配置文件不存在，会提示尚未创建。

**用法：**

```bash
ssh-tunnel-daemon config show
```

---

### `ssh-tunnel-daemon config edit` — 编辑配置

使用 `$EDITOR` 打开配置文件进行编辑。如果未设置 `EDITOR`，则默认使用 `vi`。

**用法：**

```bash
ssh-tunnel-daemon config edit
```

---

### `ssh-tunnel-daemon version` — 显示版本

打印当前版本信息。

**用法：**

```bash
ssh-tunnel-daemon version
```

## 路径说明

- 配置文件：`$XDG_CONFIG_HOME/ssh-tunnel-daemon/config.yaml`
- 状态文件（pid）：`$XDG_STATE_HOME/ssh-tunnel-daemon/*.pid`
- 日志文件：`$XDG_STATE_HOME/ssh-tunnel-daemon/logs/*.log`

## 开发

```bash
mise run build   # 构建二进制文件到 bin/ssh-tunnel-daemon
mise run test    # 运行全部测试
mise run clean   # 清理构建产物
```

[MIT](./LICENSE)
