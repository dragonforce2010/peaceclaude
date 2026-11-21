# PeaceClaude 架构设计与实施计划

- **日期**: 2026-11-21
- **交付二进制**: `~/.local/bin/peaceclaude`（及别名 `~/.local/bin/pclaude`）
- **交付桌面应用**: `/Applications/PeaceClaude.app`
- **数据目录**: `~/.peaceclaude/`（权限 `0700`）

## 1. 设计目标与取舍

| 维度 | Claude-Cage (VM) | ReClaude (闭源托管) | PeaceClaude (本方案) |
|---|---|---|---|
| 运行开销 | Debian 13 Lima VM (2-4GB RAM, 15GB+ 磁盘) | 轻量本地代理 (~30MB) | **零 VM 单 Go 二进制 (~20MB RAM)** |
| 账号与自主可控 | 自有账号 | 需登录第三方拼车平台 | **100% 本地开源自控，自有账号 + 自有 Webshare 静态 IP** |
| 防直连 Fail-Closed | VM 独立网络栈 | 依赖远端网关 | **三层纵深：进程隔离 + 本地 Egress Gate 熔断 + macOS pf Kill-Switch** |
| Split-IP 防护 | VM 全流量代理 | 仅代理特定域名 | **支持直连 Webshare SOCKS5 全量代理 + 兼容 Clash 7897 分流预警** |
| 时区与语言对齐 | VM 内系统设置 | 无自动对齐 | **自动探测出口 GeoIP 时区，启动 CLI/Desktop/登录浏览器同步注入 TZ/LANG/--lang** |
| 多环境 & 规则共享 | VM 内多实例 | `org` + `rules` | **`profile` 强隔离 + `rules` 全局共享 CLAUDE.md/skills/agents/hooks** |
| 审计与排障 | 网络检查面板 | `leaks` + `capture` + `doctor` | **统一 `leaks` + 脱敏 `capture` + `doctor` + 403 三分诊 `diagnose` + 可恢复 `clean`** |
| Cowork VM 保护 | 暂不支持 | `vm-proxy` | **`vm-proxy` 规则生成、校验与状态检测** |
| 可视化体验 | 原生 WebView 工作台 | 纯 CLI | **内嵌轻量现代 Web 控制台 (`http://127.0.0.1:18899`) + 原生 `.app` 启动器** |

## 2. 端口与文件布局

- HTTP/HTTPS CONNECT Proxy: `127.0.0.1:18897`（默认，可配置）
- SOCKS5 Proxy: `127.0.0.1:18898`（默认，可配置）
- Control API + Web UI + PAC: `127.0.0.1:18899`（默认，可配置；`/peaceclaude.pac`）
- 主配置: `~/.peaceclaude/config.json` (`0600`)
- 共享规则源: `~/.peaceclaude/shared-rules/`
- 隔离档案: `~/.peaceclaude/profiles/<name>/{profile.json,claude-config/,desktop-data/,browser-profile/}`
- 审计与日志: `~/.peaceclaude/logs/{daemon.log,leaks.jsonl,capture.jsonl,launcher-app.log}`
- 指纹清理备份: `~/.peaceclaude/backups/<timestamp>/`
- LaunchAgent: `~/Library/LaunchAgents/com.peaceclaude.daemon.plist`

## 3. 实施清单 (Tasks)

- [x] 调研 `Lee-xeo/Claude-Cage`、`recode.cat/install.sh` (`reclaude`) 与本机现有网络/Webshare/Clash/pf/指纹清理设施
- [x] Phase 1: 初始化 Go 模块、配置系统 (`internal/config`)、域名与路由策略引擎 (`internal/policy`)
- [x] Phase 2: 实现上游拨号器 (`internal/upstream`: SOCKS5 RFC1928/1929 远端 DNS + HTTP CONNECT 认证)
- [x] Phase 3: 实现出口探测与 Fail-Closed Egress Gate 状态机 (`internal/egress`) + 审计与脱敏抓包存储 (`internal/audit`)
- [x] Phase 4: 实现核心本地代理网关 (`internal/proxy`: HTTP CONNECT + SOCKS5 + PAC + SSE 活跃流追踪 + 遥测阻断)
- [x] Phase 5: 实现多 Profile 隔离、模拟指纹、全局 `rules` 同步与可恢复指纹清理 (`internal/profile`)
- [x] Phase 6: 实现宿主机感知模块 (`internal/host`: Clash Verge 新旧 socket、`lsof` 直连泄漏扫描、`pf`/Cowork `vm-proxy`、`diagnose` 403 三分诊、`doctor` 11 项全链路体检与运行中 Desktop 审计)
- [x] Phase 7: 实现启动器与安装器 (`internal/launcher`: CLI 隔离启动器、Desktop 隔离启动器 + `/Applications/PeaceClaude.app` 图标打包、隔离 OAuth 浏览器、LaunchAgent 服务管理)
- [x] Phase 8: 实现守护进程 Control API (`internal/daemon`) 与内嵌高质感 Web 控制台 (`internal/ui`，含 11/11 关键检查面板、暂停外网并重检、复制报告、Desktop 实时审计、FuckClaude 浏览器侧自检)
- [x] Phase 9: 实现完整 CLI (`cmd/peaceclaude`)、`install.sh`、`Makefile`、`README.md` 与端到端回归测试门禁 (`scripts/regression-test.sh`)
- [x] Phase 10: 全量单元测试 (`go test -race ./...`)、安装到本地 `~/.local/bin/peaceclaude` 与 `/Applications/PeaceClaude.app`、启动真实守护服务验证 Webshare 静态 IP 链路、运行 `regression-test.sh` 全绿 (14/14)
- [x] Phase 11: 使用 `ego-browser` 真实打开 `http://127.0.0.1:18899` 做前端可视化端到端验收与截图取证，并做全量实现后 Code Review

## 4. 验证门禁 (Verification Gates)

1. `go test -race ./...` 全部通过（覆盖 policy、upstream SOCKS5/HTTP、egress gate 熔断/恢复、HTTP/SOCKS5 代理、遥测拦截、脱敏抓包不泄密、profile 隔离与 rules 同步、CSRF 防护、doctor/diagnose 逻辑）。
2. `./scripts/regression-test.sh` 全绿（覆盖真实二进制 `--version`、配置自动初始化、daemon 启停、真实代理出口验证、Fail-Closed 强制熔断拦截反例、遥测拦截、leaks/capture 脱敏验证、profile 创建/切换/rules 同步、pf/vm-proxy 语法 `pfctl -n` 校验、`.app` 启动器结构完整、零密码泄漏）。
3. `ego-browser` 打开本地 Web UI 验证核心页面渲染、零控制台错误、交互响应正常并留存验收截图。
4. 新开干净 shell（无 nvm 预加载）可直接运行 `peaceclaude --version`、`peaceclaude status`、`peaceclaude doctor`。
