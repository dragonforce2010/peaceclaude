# PeaceClaude

**省心、放心的 macOS 零虚拟机静态 IP Claude 全入口安全套件（Desktop / CLI / 浏览器）**

`peaceclaude` 融合了 **[Claude-Cage](https://github.com/Lee-xeo/Claude-Cage)** 的 Fail-Closed 安全隔离理念与 **[ReClaude](https://www.recode.cat/)** 的轻量单二进制本地代理体验，专为在 macOS 上拥有 **Webshare 等静态 IP 链路**、想省心放心地使用官方 Claude（且不想承受虚拟机内存/磁盘与画面延迟开销）的用户打造。

---

## 核心能力一览

| 模块 | 解决什么问题 | 实现机制 |
|---|---|---|
| **Fail-Closed 出口门禁** | 代理断开/规则被清空/IP 漂移时直连 Anthropic 触发风控 | 本地 Go 守护网关（HTTP `:18897` / SOCKS5 `:18898`），4 源并行竞速探活 + IP 漂移 0 容忍秒锁，瞬态抖动快速复检不误杀；启用 `TCP_NODELAY`、瞬断重试、备用上游切换与半关闭连接回收，**绝不回退直连** |
| **双模式静态 IP 上游** | 既想开箱复用本机 Clash Verge，又想彻底消除 Split-IP 双出口 | 支持 `local-proxy`（无缝接管 `127.0.0.1:7897` 并自动检测 Split-IP 预警）与 `direct-socks5` / `direct-http`（直连 Webshare SOCKS5，远端解析 DNS，解决 `platform.claude.com` OAuth HTTP CONNECT RST 问题） |
| **时区与语言指纹同步** | macOS 本机 `Asia/Shanghai` + `zh-CN` 撞上美国静态 IP | 根据出口 IP 精准查询 IANA 时区（如自动对齐 `America/Denver` / `America/New_York`），启动 CLI / Desktop / 登录浏览器时自动注入 `TZ`、`LANG=en_US.UTF-8`、`--lang=en-US` |
| **统一托管 CLI & Desktop + Auto-Peace 防误点** | 容易误点原生图标或被宿主机 `ANTHROPIC_BASE_URL` 中转污染 | `peaceclaude cli` 自动定位最新版原生二进制并剥离宿主机污染变量；`/Applications/PeaceClaude.app` 强制独立 `--user-data-dir`、`--proxy-server`、`--disable-quic`（兼容官方 Claude.app v6+ 启动校验）并内置启动探活与窗口置顶；后台实时守护，误点原生 `Claude.app` 自动接管重开 |
| **多浏览器全入口覆盖 (PAC + 隔离浏览器)** | 普通浏览器或 Desktop 唤起 Google OAuth 时直连 `claude.ai` 触发 403 | `pac.go` 与 `policy.go` 共享完整 OAuth/Captcha 资源域（覆盖 `accounts.google.com`/`gstatic.com`/`googleapis.com`/`googleusercontent.com`），`www.google.com` 与飞连内网保持 `DIRECT`；多账号页在未开 PAC 时自动提示一键开启 |
| **多账号管理、套餐与实时用量中心** | 多账号容易串号或网页登录回调「打开 Claude」发偏无响应 | 采用 **Hero 默认焦点仪表卡 + 待命账号列对齐清单** 原创布局：清晰区分「已激活」vs「未登录」及 `Desktop PID`，内置 **单活切换模式** 平滑关闭干扰实例并透传 `claude://` 深链 |
| **Unslop 原创外观工坊与一体式指令托盘** | 默认设置界面单一，缺实时画布与快捷锁屏控制 | 按 `unslop` 精密仪器美学原创设计：统一单层侧栏、单行遥测顶轨、7:5 实时微缩画布外观工坊（5 套浅色 / 5 套深色原创色板 + 三原色微调 + JSON 导入导出）、左下角集成快捷面板支持内联主题速切与 `⌘L` 一键锁屏 |
| **手机扫码远程配对台与移动控制台** | 需要手机端快速查看余额、切换默认号或唤醒 Desktop | 7:5 连接拓扑与 120s 实时倒计时「配对通行证」，内置纯 Go ISO-18004 SVG 二维码（`/mobile` 支持浅/深自适应），支持局域网 / Tailscale 限权 Bearer 直连与随时撤销授权设备 |
| **遥测屏蔽盾** | 客户端埋点泄露宿主机异常与设备指纹 | 网关层阻断 `sentry.io`、`statsigapi.net`、`segment.io` 等遥测域名 + CLI 注入 `DISABLE_TELEMETRY=1` |
| **直通泄漏审计 & 30 天滚动存储** | 日志长期累计占磁盘，403 不知道怎么查 | `leaks` 与脱敏 `capture`（自动脱敏 Authorization/Cookie/OAuth Code）均默认保留最近 **30 天**并每 6 小时自动滚动清理（辅以 10MB 单文本日志上限）；所有配置存于 `~/.peaceclaude/` (`0700`/`0600`) 本地 JSON 目录 |
| **403 三分诊 & 可恢复指纹清理** | 看到 403 误点 Sign Out 伤 Token；换号清理怕误删 skills | `diagnose` 自动区分 `network` / `auth` / `unknown`；`clean` 盘点+自动备份+保留 `CLAUDE.md/skills/settings.json` |
| **macOS pf 护盾 & Cowork `vm-proxy`** | 兜底任何绕过环境变量直连或 Cowork VM 泄漏 | 自动生成经 `pfctl -n` 校验的 `pf` Kill-Switch 与 Cowork 子网规则（绝不开 TUN，绝不冲突企业飞连） |
| **零依赖内嵌 Web 控制台** | 命令行之外一眼看清安全状态 | `http://127.0.0.1:18899` 提供安全评分环、浏览器本地指纹自检、档案管理、抓包与一键启动 |

---

## 快速安装

```bash
chmod +x ./install.sh
./install.sh
```

安装完成后会就绪：
- 全局命令：`~/.local/bin/peaceclaude` 与短别名 `~/.local/bin/pclaude`（不依赖 nvm，新终端直接可用）
- 桌面入口：`/Applications/PeaceClaude.app`（带专属锁盾图标，可直接从 Spotlight / Launchpad / Dock 启动）
- 后台服务：macOS LaunchAgent `com.peaceclaude.daemon`（开机/登录自启，仅监听 `127.0.0.1`）
- 配置与数据：`~/.peaceclaude/`（严格 `0700`/`0600` 权限，绝不覆写原生 `~/.claude` 与 `Claude.app` 数据）

---

## 日常正确使用指南

### 1. 日常使用 Claude Code CLI
```bash
peaceclaude cli
# 或使用短别名
pclaude cli
# 透传任意原生参数（如 --continue、-p）
pclaude cli --continue
```
> **不要**直接用未隔离的原生 `claude` 访问官方个人订阅账号；通过 `pclaude cli` 启动会自动完成 Egress Gate 预检、注入静态 IP 代理、同步美区时区/语言、并剥离宿主机中转 `ANTHROPIC_BASE_URL`。

### 2. 日常使用 Claude Desktop 客户端
- **推荐方式**：直接从 Spotlight / `/Applications` 打开 **`PeaceClaude.app`**（可拖到 Dock），或在终端运行：
  ```bash
  peaceclaude app
  ```
- 如果误点了原生 `Claude.app`：
  - 默认开启 **Auto-Peace 自动接管**（`auto_peace_desktop=true`）：后台在 2 秒内自动关闭新开的未隔离原生窗口，校验静态 IP 门禁后自动以安全隔离模式重新拉起 Claude Desktop；
  - 若想改为“只弹原生警告框，点击按钮再切换”：`peaceclaude config set auto_peace_desktop false && peaceclaude service restart`；
  - 对之前已经开着的旧原生窗口，可运行 `peaceclaude app takeover` 或在 Web 控制台点 **「一键接管并隔离重开」**。
- 如果出口异常或被手动熔断，启动器会直接弹出 macOS 红色告警框阻止启动，绝不会放行直连。

### 3. 浏览器访问（隔离浏览器 & 普通浏览器 PAC）
- **最推荐（完整隔离：IP + 指纹 + 时区 + Cookie）**：使用内置隔离浏览器：
  ```bash
  peaceclaude login-browser
  # 或传入 CLI 打印出来的 OAuth URL：
  peaceclaude login-browser "https://platform.claude.com/oauth/authorize?..."
  ```
  该浏览器使用独立的 `browser-profile`，强制走 `peaceclaude` 静态 IP 网关与美区时区/语言；通过强写 `Preferences`/`Local State` 覆盖语言与 `DNT`/`Geolocation` 禁权，并在 Chromium 浏览器按需加载本地回环 `Identity Shield` MV3 扩展统一 `navigator.languages`、`Intl` 默认区域、GPC 与 Canvas 中文字体指纹枚举，且禁用 QUIC 与 WebRTC 非代理 UDP。
- **普通浏览器（Chrome / Safari / Arc / Edge）网络层自动保护**：
  ```bash
  peaceclaude browser-pac on     # 开启系统 PAC（仅 Claude 域名走 PeaceClaude，不影响飞连与日常网页）
  peaceclaude browser-pac status # 检查 PAC 状态
  peaceclaude browser-pac off    # 随时一键还原
  ```
  > 注：PAC 让所有普通浏览器访问 `claude.ai` 自动享受静态 IP + Fail-Closed 熔断，但无法改写普通浏览器自带的宿主机时区/语言/历史 Cookie；深度使用网页版或首次登录仍推荐 `peaceclaude login-browser`。

### 4. 升级为“零 Split-IP”直连 Webshare SOCKS5 模式（强烈推荐）
默认初始化会无缝复用本机 Clash Verge (`127.0.0.1:7897`)。如果你有可直达的 Webshare SOCKS5 静态 IP，强烈建议切换到直连模式，让所有页面附属请求（Stripe / hCaptcha / Cloudflare / 第三方脚本）100% 与 Claude 走同一个静态 IP，彻底消除 Split-IP 风险：
```bash
# 方式 A：直接配置 Webshare SOCKS5
peaceclaude config set-upstream \
  --mode direct-socks5 \
  --addr <WEBSHARE_IP>:<PORT> \
  --user <USERNAME> \
  --pass <PASSWORD> \
  --expected-ip <WEBSHARE_IP>
peaceclaude service restart
peaceclaude config gateway   # 测速并验证出口与 OAuth TLS

# 方式 B：如果节点已在 Clash 配置中，可先安全预览再决定是否导入（绝不打印密码）
peaceclaude config import-clash --node Claude-US-1
# 注意：若你的节点依赖 Clash 前置跳板 (dialer-proxy，如 Xray-LA-1)，请继续保持 local-proxy 模式，
# 并可按需开启严格域名隔离以阻断非 Claude 域名的 Split-IP：
peaceclaude config set strict_split true
peaceclaude service restart
```

### 5. 多账号隔离与全局规则共享
```bash
# 创建第二个隔离档案
peaceclaude profile create backup-us --note "备用 Webshare 账号"
# 切换默认档案
peaceclaude profile use backup-us
# 开启全局规则共享（CLAUDE.md / skills / agents / hooks 跨档案同步）
peaceclaude rules on
peaceclaude rules status
```

### 6. 遇到 403 / 断流 / 换号时的标准操作
```bash
# ① 看到 403 千万别立刻点 Sign Out！先运行三分诊：
peaceclaude diagnose

# ② 运行全链路防风控安全体检（0-100 评分）：
peaceclaude doctor

# ③ 查看近 24 小时直通与分流泄漏审计：
peaceclaude leaks --since 24h

# ④ 需要深度排查请求时，按需开启脱敏抓包（查完记得关闭）：
peaceclaude capture on
peaceclaude capture list
peaceclaude capture off

# ⑤ 换号或清理旧指纹（先 dry-run 盘点，确认后加 --yes，自动备份且保护 skills/CLAUDE.md）：
peaceclaude clean --profile default --dry-run
peaceclaude clean --profile default --yes

# ⑥ 打开可视化 Web 控制台：
peaceclaude ui
```

### 7. 可选：激活 macOS 内核级 `pf` 防直连 Kill-Switch（含 Cowork VM 防护）
如果你希望连宿主机上任何企图绕过代理直连 Anthropic CIDR 的进程都被内核直接丢弃（如果本机已有 `/etc/pf.anchors/claude.egress` 护盾，`peaceclaude doctor` 会自动识别并显示绿灯）：
```bash
peaceclaude vm-proxy status
# 如需启用 Cowork VM 子网白名单并安装 PeaceClaude pf anchor：
peaceclaude vm-proxy on
peaceclaude vm-proxy install   # 按提示执行 sudo 命令，不清空企业飞连规则
```

---

## CLI 与控制台详细使用指南（Private 仓库脱敏文档）

> 完整 22 组 CLI 子命令参数详解、CLI 唤起系统浏览器（Arc 等）PAC 安全前置条件、`Privacy -> Location metadata` 防泄露实证说明与全套工作台界面详解，请参阅：[docs/CLI_GUIDE.md](docs/CLI_GUIDE.md)。

- **内嵌 CLI 使用指南**：Web 控制台左侧 **「系统与连接 → CLI 使用指南」** 直接列出全部常用 CLI 指令，支持点击一键复制；
- **多账号中心与安全检测脱敏预览**（来自 RFC 5737 脱敏沙盒演示数据）：

![多账号中心界面](docs/screenshots/accounts-overview.png)

![安全检测与分诊界面](docs/screenshots/security-doctor.png)

---

## 开发与测试

```bash
make build          # 编译二进制到 ./bin/peaceclaude
make test-race      # 运行全量 Go 竞态与单元测试
make regression     # 运行端到端沙箱回归测试门禁
```
