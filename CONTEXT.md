# PeaceClaude — Project Context & Domain Glossary

## Purpose
`peaceclaude` 是一套面向 macOS 的**零虚拟机、轻量级、Fail-Closed 静态 IP 隔离与全链路安全工具套件**，用于安全、稳定、无感地使用官方 **Claude Desktop** 与 **Claude Code CLI**。

它融合了：
- **ReClaude (`recode.cat`)** 的轻量单二进制体验：统一托管 CLI 与 Desktop、共用本地代理守护进程、原生 macOS `.app` 启动器、直通域名泄漏审计（`leaks`）、脱敏本地抓包（`capture`）、多账号 Profile 隔离、全局规则共享（`rules`）、macOS Cowork `vm-proxy` 与医生自检（`doctor`）。
- **Claude-Cage (`Lee-xeo/Claude-Cage`)** 的核心防风控安全理念：Fail-Closed 永不直连回退、多源出口 IP 强验证、时区/语言随出口 IP 自动同步、环境与指纹体检（含 FuckClaude 浏览器侧检查项）、多环境隔离与可恢复指纹清理、现代化可视化控制台。
- **macOS 原生界面入口 (`/Applications/PeaceClaude.app`)**: 双击打开基于 Swift + WKWebView 的原生管理窗口（`http://127.0.0.1:18899/#accounts`），严格按 **Unslop** (`@heydamir`, CC-BY-4.0) 极简精密仪器规范为 PeaceClaude 原创设计，采用单一分组固定侧栏、单行顶部遥测轨与非对称工作流：
  - **多账号中心**：顶部「当前使用」账号宽幅焦点仪表卡（随当前主题主色 `--accent` 边框高亮 + 实心胶囊标签）+ 下方列对齐待命账号清单（准确区分 `已激活` vs `未登录` 与独立 `Desktop PID`、自动识别仅 Desktop 登录的 `Free` 档案及无付费配额明确提示、彩色套餐胶囊、每账号直接显示可跳转的 0-100 安全得分与二次确认删除，长名称自动截断并悬停显示全名）；
  - **CLI 使用指南页**：左侧导航独立分区，汇总全 22 组 CLI 命令分类卡片与一键复制，并醒目提示 CLI 唤起系统浏览器（Arc 等）时须保持系统 PAC 开启或改用隔离浏览器；
  - **7:5 外观工坊**：左侧实时微缩 PeaceClaude 仪表画布（实时预览亮/暗、三态安全语义条与终端等宽字体）+ 右侧原创色板矩阵（`paper`/`linen`/`frost`/`stone`/`chalk` 与 `obsidian`/`midnight`/`slate`/`amber`/`moss`，向后兼容历史预设别名）与快照标签；
  - **7:5 远程配对台**：左侧三节点连接拓扑与授权设备管理 + 右侧四角定位配对通行证（纯 Go ISO-18004 SVG 二维码 + 120s 实时倒计时进度条，驱动限权 `/mobile` 控制台）；
  - **左下角一体式身份控制托盘**：呼出带内联浅/深/跟随速切与 2×2 快捷入口的控制面板，支持 `⌘L` 锁定控制台（代理持续守护不中断）。
- **网络韧性与 PAC/Policy 单一源**：`egress` 探针采用多源并行竞速 + 瞬态超时 1 次宽容/快速复检（IP/国家违规仍 0 容忍秒锁）；`upstream` / `proxy` 开启 `TCP_NODELAY`、瞬断重试与主备无缝切换、`tunnelBidirectional` 半关闭防泄漏；`pac.go` 与 `policy.go` 共享完整 Google OAuth 资源域（含 `gstatic.com`/`googleapis.com`/`googleusercontent.com`），并保持 `www.google.com` 走 `DIRECT`。
- **本机存储与防关联边界**: 账号数据与隔离环境均存于 `~/.peaceclaude/` 下的 `0700`/`0600` 目录与 JSON 文件（非 SQLite），可消除本机文件/Cookie/DeviceUUID 串号，但不能替代多 IP（多号共享同一静态 IP 或相同硬件指纹仍存在服务端关联风险）。
- **官方个人版 (`1p`) 主端与三方模型边界**: 官方 `Claude.app` 个人版聊天使用私有 Web 会话协议，不支持在保持 `1p` 原生交互体验的同时挂接三方模型 API；三方模型应仅在宿主机独立 CLI 通道使用，绝不强行破解桌面主端。

## Domain Glossary (Canonical Terms)
- **Egress Gate (出口门禁)**: `peaceclaude` 守护网关内置的 Fail-Closed 状态机。未验证或出口 IP 偏离预期时处于 `LOCKED`，拒绝一切 Claude/Anthropic 流量，永不回退 DIRECT。_Avoid_: `fallback`、`auto-direct`。
- **Upstream (上游链路)**: 出口流量实际发往的代理目标，支持 `direct-socks5` / `direct-http`（直连 Webshare 静态 IP，零 Split-IP）与 `local-proxy`（接入本机 Clash Verge `127.0.0.1:7897`）。
- **Profile (隔离档案)**: 位于 `~/.peaceclaude/profiles/<name>/` 的独立身份沙箱，包含独立 `CLAUDE_CONFIG_DIR`、Desktop `--user-data-dir`、隔离登录浏览器目录与模拟指纹元数据。_Avoid_: `org`（无第三方拼车组织语义）、`default slot`。
- **Shared Rules (共享规则)**: 通过 `peaceclaude rules` 在所有 Profile 之间共享的全局 `CLAUDE.md`、`skills/`、`agents/`、`hooks/`，保持身份隔离而工作流一致。
- **Telemetry Shield (遥测阻断盾)**: 网关层与环境变量层联合阻断 `sentry.io`、`statsigapi.net` 等遥测域名与错误上报。
- **Leak Audit (直通泄漏审计)**: 记录所有经过网关的非白名单/直通域名决策，以及后台 `lsof` 扫描到的宿主机直连 Anthropic socket。
- **Sanitized Capture (脱敏抓包)**: 网关层可选开启的请求元数据审计，严格剥离 `Authorization`、`Cookie`、`x-api-key`、`Proxy-Authorization` 等一切凭据。
- **Split-IP Risk (分流双出口风险)**: 使用分流型本地代理时，Claude 主域名走静态 IP 而页面辅助域名走宿主机/企业 VPN 出口的现象。

## Hard Security Boundaries
1. **Fail-Closed Only**: Egress Gate 未通过时，所有 Claude/Anthropic/OAuth 域名必须硬拒绝（503 / 拒连），绝不回退直连。
2. **No Secret Leakage**: 上游代理密码、API Key、OAuth Token、Cookie 严禁写入日志、抓包、诊断报告、Web UI 或 CLI stdout。
3. **Never Enable TUN**: 绝不修改 Clash Verge 开启 TUN 模式，绝不破坏宿主机企业 VPN（如飞连）与内网路由。
4. **Loopback-Only Control Plane + CSRF Guard**: Control API/Web UI 仅绑定 `127.0.0.1`，所有状态变更端点校验 `Host` 与同源 `Origin`。
5. **Non-Destructive Coexistence**: 不覆盖宿主机原生 `claude`、现有 `reclaude`、现有 `claude-egress-guard` 或原生 `Claude.app` 数据。
