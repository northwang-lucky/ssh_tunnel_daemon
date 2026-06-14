# Changelog

## [2.0.0](https://github.com/northwang-lucky/ssh_tunnel_daemon/compare/v1.2.0...v2.0.0) (2026-06-14)


### ⚠ BREAKING CHANGES

* remove --no-supervisor and route all starts through the supervisor.

### Features

* add session-scoped rotating logs ([13ea545](https://github.com/northwang-lucky/ssh_tunnel_daemon/commit/13ea5456ad6845c4da4eadd6869bc6830950f2fd))
* **daemon:** start 命令输出隧道 PID，新增 WaitForTunnelPID ([80e26c8](https://github.com/northwang-lucky/ssh_tunnel_daemon/commit/80e26c8102b913908bc1cb9c55e7442939b27b64))

## [1.2.0](https://github.com/northwang-lucky/ssh_tunnel_daemon/compare/v1.1.0...v1.2.0) (2026-06-14)


### Features

* **cli:** 新增 list 子命令列出所有已保存的隧道 ([6a74aae](https://github.com/northwang-lucky/ssh_tunnel_daemon/commit/6a74aaeb966f335eac14b2c10f4447d255be8d2e))


### Bug Fixes

* **daemon:** ListRunning 不再把 supervisor PID 文件当作隧道列出来 ([278a9d9](https://github.com/northwang-lucky/ssh_tunnel_daemon/commit/278a9d9d4c0e114674ac41bad85d0f699c3cda12))
* **prompt:** 修复 SelectTunnel 选择已有隧道时错误返回 create=true ([df37f0d](https://github.com/northwang-lucky/ssh_tunnel_daemon/commit/df37f0d9290e5a21960a043c8ae5c77259fcf2fd))

## [1.1.0](https://github.com/northwang-lucky/ssh_tunnel_daemon/compare/v1.0.1...v1.1.0) (2026-06-14)


### Features

* **daemon:** release supervisor auto-reconnect (minor bump) ([e07f6e9](https://github.com/northwang-lucky/ssh_tunnel_daemon/commit/e07f6e940187f5cbde0a2bebb8ca6aba42152459))
* **daemon:** 新增断线自动重连 supervisor 机制 ([0e1cf62](https://github.com/northwang-lucky/ssh_tunnel_daemon/commit/0e1cf62aee8b836b832af9d0162e07730fbae641))


### Bug Fixes

* **cli:** 禁用 Cobra 默认生成的 completion 子命令 ([2460a05](https://github.com/northwang-lucky/ssh_tunnel_daemon/commit/2460a05abb5b69434bc33405ba8e3fde86df778f))
* **daemon:** 修复 supervisor 被 SIGPIPE/SIGHUP 意外终止的问题 ([a5b5749](https://github.com/northwang-lucky/ssh_tunnel_daemon/commit/a5b57490b3f94822d0f89df4a8a060f60bf34806))

## [Unreleased]

### Features

* **supervisor:** 新增断线自动重连 supervisor，start 命令默认启用 watchdog 看守 ssh 子进程
  - ssh 退出时按指数退避自动重试，最多 10 次（2s 基数，60s 封顶）
  - 新增 `--no-supervisor` 标志可回退到旧行为
  - `stop` 命令会同时停止 supervisor 和 ssh 进程
  - supervisor 使用独立的 PID 文件和日志

### Refactoring

* **cli:** 移除 restart 子命令

## [1.0.1](https://github.com/northwang-lucky/ssh_tunnel_daemon/compare/v1.0.0...v1.0.1) (2026-06-13)


### Bug Fixes

* **config:** 添加 refactor 到 changelog-sections ([412b99d](https://github.com/northwang-lucky/ssh_tunnel_daemon/commit/412b99d7f35bef6d407a4e3ffeee8efab3f1e6e2))
* **config:** 添加 refactor 到 changelog-sections ([15ea94e](https://github.com/northwang-lucky/ssh_tunnel_daemon/commit/15ea94e14871a544841b065edb6cd1846f093f26))

## 1.0.0 (2026-06-13)


### Features

* **cli:** 实现 ssh-tunnel-daemon 完整 CLI 与日志系统 ([3663fc3](https://github.com/northwang-lucky/ssh_tunnel_daemon/commit/3663fc344235b20f167c5d64da66c20d2d3fc510))
* **init:** 初始化 ssh-tunnel-daemon Go 项目开发环境 ([b76cfe9](https://github.com/northwang-lucky/ssh_tunnel_daemon/commit/b76cfe9f3b9081d5212adea1eab267d217c269e4))
* **release:** 建立 Release Please + GoReleaser + Homebrew tap 自动化发布流水线 ([a5f7737](https://github.com/northwang-lucky/ssh_tunnel_daemon/commit/a5f7737ffb0350aa560ae0174ff9a3ea95920330))
* **skill:** 新增 publish-binary 发布流程技能 ([111b833](https://github.com/northwang-lucky/ssh_tunnel_daemon/commit/111b8337b86837fb77698c03d6b0908b0dfbdbb7))
