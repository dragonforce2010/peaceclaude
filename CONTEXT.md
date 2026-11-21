# PeaceClaude — Project Context & Domain Glossary

## Purpose
`peaceclaude` 是一套面向 macOS 的**零虚拟机、轻量级、Fail-Closed 静态 IP 隔离与全链路安全工具套件**，用于安全、稳定、无感地使用官方 **Claude Desktop** 与 **Claude Code CLI**。

它融合了：
- **ReClaude (`recode.cat`)** 的轻量单二进制体验：统一托管 CLI 与 Desktop、共用本地代理守护进程、原生 macOS `.app` 启动器、直通域名泄漏审计（`leaks`）、脱敏本地抓包（`capture`）、多账号 Profile 隔离、全局规则共享（`rules`）、macOS Cowork `vm-proxy` 与医生自检（`doctor`）。
- **Claude-Cage (`Lee-xeo/Claude-Cage`)** 的核心防风控安全理念：Fail-Closed 永不直连回退、多源出口 IP 强验证、时区/语言随出口 IP 自动同步、环境与指纹体检（含 FuckClaude 浏览器侧检查项）、多环境隔离与可恢复指纹清理、现代化可视化控制台。
- **本机实战防御沉淀**：兼容 Webshare 静态 IP（SOCKS5 远端解析，规避 `platform.claude.com` HTTP CONNECT RST）、兼容 Clash Verge 新旧 Unix Socket、绝不开启 TUN（避免与企业飞连冲突）、SSE 活跃长流探测避让、macOS `pf` 防直连 Kill-Switch、403/断流三分类分诊。

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
