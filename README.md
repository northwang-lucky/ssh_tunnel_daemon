# ssh-tunnel-daemon

SSH 隧道守护进程。

## 开发环境

本项目使用 [mise](https://mise.jdx.dev/) 管理 Go 工具链。

```bash
mise install
```

## 常用命令

```bash
mise run build   # 构建二进制文件到 bin/ssh-tunnel-daemon
mise run test    # 运行全部测试
mise run clean   # 清理构建产物
```

## 功能

- 启动、停止、重启 SSH 隧道守护进程。
- 支持 `local`（`-L`）与 `remote`（`-R`）两种隧道模式。
- 基于 YAML 的配置文件，默认位于 `~/.config/ssh-tunnel-daemon/config.yaml`。
- 交互式选择/创建隧道（基于终端 UI）。
- 每个隧道启动时生成独立的日志文件，日志按 `tunnel_name_YYYYMMDD_HHMMSS.log` 命名。
- 日志保留 3 天，每次命令调用时自动懒清除过期日志。

## 使用示例

```bash
# 启动并保存配置
ssh-tunnel-daemon start web -t user@example.com -p 8080,9090 -m local --save

# 从配置文件启动
ssh-tunnel-daemon start web

# 查看状态
ssh-tunnel-daemon status

# 停止/重启
ssh-tunnel-daemon stop web
ssh-tunnel-daemon restart web

# 配置管理
ssh-tunnel-daemon config show
ssh-tunnel-daemon config edit

# 版本
ssh-tunnel-daemon version
```

命令同时支持别名 `sshtnl` 与 `s17n`。

## 路径说明

- 配置文件：`$XDG_CONFIG_HOME/ssh-tunnel-daemon/config.yaml`
- 状态文件（pid）：`$XDG_STATE_HOME/ssh-tunnel-daemon/*.pid`
- 日志文件：`$XDG_STATE_HOME/ssh-tunnel-daemon/logs/*.log`
