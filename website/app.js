(() => {
  const $ = (id) => document.getElementById(id);

  const I18N = {
    en: {
      nav_arch: 'Cockpit',
      nav_guide: 'Quickstart',
      nav_ecosystem: '3-Platform',
      nav_surfaces: 'Shields',
      nav_preview: 'Showcase',
      nav_faq: 'FAQ',
      nav_download: 'Install',
      cta_download_short: 'Quickstart',
      hero_eyebrow: 'macOS · Windows · Ubuntu · iOS PeaceSwitch · ~18 MB Zero-VM',
      hero_title: 'Native OS Speed.<br/>Deterministic Static-IP Safety.',
      hero_lead: 'Ditch the 8 GB Linux VM without risking naked proxy leaks. PeaceClaude wraps Claude Desktop, Claude Code CLI, and your native Arc & Chrome browsers in a Fail-Closed static-IP gate across macOS, Windows, and Ubuntu — paired with our native PeaceSwitch iOS companion app.',
      hero_cta_primary: 'Read 4-Platform Guide',
      copy_label: 'Copy',
      metric_ram: 'Daemon RAM',
      metric_boot: 'Cold Launch',
      metric_risk: 'Scanner Risk Score',
      metric_deps: 'Pure Go Stdlib',
      deck_title: 'Peace Switch Telemetry',
      deck_sub: 'Toggle to compare naked host vs PeaceClaude posture',
      deck_score_label: 'Risk Score',
      dk_egress: 'Egress Route',
      dk_tz: 'Runtime Timezone',
      dk_locale: 'Locale & Fonts',
      dk_kernel: 'Kernel / Drop Safety',
      guide_kicker: 'INTERACTIVE QUICKSTART PLAYBOOK',
      guide_title: 'Up and guarded across 4 platforms in under 2 minutes.',
      guide_desc: 'Select your platform below for step-by-step setup: install daemon, bind your static residential egress, shield your desktop/CLI/browser, and pair PeaceSwitch on iPhone.',
      eco_kicker: '3-PLATFORM ECOSYSTEM',
      eco_title: 'One static-IP brain. Every screen in sync.',
      eco_desc: 'Manage accounts and fail-closed locks wherever you are — native SwiftUI on iOS, local browser console on desktop, and cross-platform Go CLI/daemon across macOS, Windows, and Ubuntu.',
      eco_pc_t: 'macOS · Windows · Ubuntu Linux',
      eco_pc_d: 'Unified single-binary Go daemon (<code>peaceclaude</code>), OS-native autostart (LaunchAgent / Registry Run / systemd --user), scoped browser PAC, and macOS physical interface VPN bypass (<code>IP_BOUND_IF</code>).',
      eco_ios_t: 'PeaceSwitch for iPhone (Pure SwiftUI)',
      eco_ios_d: '100% native SwiftUI with AVFoundation QR pairing, iOS Keychain tokens, haptic Fail-Closed Kill Switch, live Pro/Max vs Free quota readout, and zero-server Interactive Demo Mode.',
      eco_web_t: 'Embedded Local Workbench (:18899)',
      eco_web_d: 'Runs seamlessly offline inside your loopback interface with 11-point Security Doctor, Appearance Studio themes, 403 network/auth triage, and one-click MV3 native browser setup.',
      ios_mock_title: 'PeaceSwitch Cockpit',
      ios_mock_ip_lbl: 'Verified Static Egress',
      ios_mock_acc_lbl: 'Isolated Accounts (Tap to Switch)',
      ios_mock_q1: '5h 38% · Weekly 54%',
      ios_mock_q2: 'Free tier · no 5h/Weekly bucket',
      ios_mock_lock_btn: 'Trigger Emergency Lock',
      ios_mock_active: 'ACTIVE',
      ios_mock_standby: 'STANDBY',
      ios_tab_cockpit: 'Cockpit',
      ios_tab_accounts: 'Accounts',
      ios_tab_security: 'Security',
      cmp_kicker: 'ZERO-VM ENGINEERING',
      cmp_title: 'Why carry an 8 GB Linux VM just to lock an IP and timezone?',
      cmp_desc: 'PeaceClaude enforces deterministic network fail-closing and browser/CLI fingerprint alignment at the OS user-space + kernel boundary — keeping your workstation instant, silent, and battery-friendly.',
      b_ram_title: 'Resident Memory',
      b_boot_title: 'Ready-to-Code Startup',
      b_disk_title: 'Disk Footprint',
      b_vpn_title: 'Corporate VPN Coexistence',
      lbl_vm: 'Linux Desktop VM',
      lbl_tun: 'System TUN / VM Bridge',
      val_no_tun: 'No TUN · Coexists',
      val_conflict: 'Route Clash',
      bento_kicker: 'FULL-SURFACE ARCHITECTURE',
      bento_title: 'Six engineered defense layers in one cohesive app.',
      bento_desc: 'From kernel packet anchors to browser MV3 Content Scripts, every surface stays aligned with your active profile.',
      f1_t: 'Fail-Closed Egress Gate + Firewall',
      f1_d: '4-source parallel IP probe verifies your static node. On any IP drift or manual lock, the gateway returns hard 503, severs active streams, and drops direct leaks.',
      f2_t: 'Native Arc & Chrome MV3 Shield',
      f2_d: 'Select Arc, Chrome, Brave, or Edge in the console: pairs Claude-only PAC routing with a domain-scoped MV3 extension achieving 2/100 LOW RISK without touching Slack, Lark, or work tabs.',
      f3_t: 'Isolated Multi-Account Cockpit',
      f3_d: 'Separate config, Desktop user-data-dir, cookies, and UUID per account. Detects Free vs Pro/Max automatically, routes claude:// OAuth callbacks strictly to the active window.',
      f4_t: '11-Point Doctor & 403 Triage',
      f4_d: 'Verifies remote SOCKS5 DNS, --disable-quic, WebRTC UDP suppression, and host socket scans. Smart 3-way diagnose separates transient network drops from real auth expiration.',
      f5_t: 'LAN & Tailscale PeaceSwitch iOS',
      f5_d: 'Issue 120-second ISO-18004 SVG QR pairing passes backed by SHA-256 Bearer tokens to check quotas, swap active profiles, or lock egress from PeaceSwitch on iPhone.',
      f6_t: '30-Day Redacted Observability',
      f6_d: 'Local packet capture and Split-IP leak logs strip Authorization headers, Cookies, and OAuth tokens before hitting 0600 disk files, auto-pruned on a rolling 30-day schedule.',
      show_kicker: 'LIVE WORKBENCH TOUR',
      show_title: 'Crafted as a quiet precision instrument.',
      show_desc: 'Built with zero external frontend frameworks. Inspect real sanitized captures from PeaceClaude v0.2.0 below.',
      faq_kicker: 'FREQUENTLY ASKED QUESTIONS',
      faq_title: 'Everything engineers ask before switching.',
      faq_q1: 'Will PeaceClaude slow down Slack, Lark, or normal browsing?',
      faq_a1: 'No. Our PAC file and domain-scoped MV3 Shield match only Claude/Anthropic hostnames (<code>claude.ai</code>, <code>claude.com</code>, <code>anthropic.com</code>, <code>claudeusercontent.com</code>, and <code>fuck-claude.app</code>). Corporate intranets, video calls, and general tabs bypass the proxy directly and retain your native timezone.',
      faq_q2: 'Can I run PeaceClaude alongside a corporate VPN?',
      faq_a2: 'Yes. Unlike TUN-based proxy apps that hijack the global routing table, PeaceClaude uses targeted loopback ports plus macOS <code>IP_BOUND_IF</code> physical interface binding so static US upstream probes and Claude sessions bypass corporate VPN interference cleanly.',
      faq_q3: 'What operating systems and browsers are supported?',
      faq_a3: 'Desktop/CLI runs natively on macOS Universal (<code>arm64</code> + <code>x86_64</code>), Windows 10/11 (<code>amd64</code> + <code>arm64</code>), and Ubuntu/Debian Linux (<code>.deb</code> &amp; <code>tar.gz</code>). Native browser shielding works with Arc, Google Chrome, Brave, and Microsoft Edge.',
      faq_q4: 'How can I test the PeaceSwitch iOS app before setting up a server?',
      faq_a4: 'Launch PeaceSwitch on iPhone and tap "Launch Interactive Demo Mode" on the pairing screen. You can test profile switching, Pro/Max vs Free quota display, emergency fail-closed gate locks, and PAC toggles completely offline with zero server setup.',
      faq_q5: 'How do I host this website on peace-claude.app while keeping my repo private?',
      faq_a5: 'The <code>website/</code> folder has zero build dependencies. Deploy it directly via Vercel or Cloudflare Pages with Root Directory set to <code>website</code> and bind your <code>peace-claude.app</code> DNS records—your GitHub source repo can stay Private permanently. GitHub Pages workflow <code>.github/workflows/pages.yml</code> is also included.',
      bnd_kicker: 'HONEST ENGINEERING',
      bnd_title: "Transparent about what Zero-VM covers — and doesn't.",
      bnd_1: '<strong>Network &amp; Fingerprint Guarantee:</strong> Fail-Closed static-IP routing, kernel <code>pf</code> IPv4/IPv6/UDP killswitch, isolated profile paths, host env scrubbing, and domain-scoped timezone/locale/CJK-font alignment.',
      bnd_2: '<strong>Same OS UID Boundary:</strong> Unlike a hardware hypervisor VM, processes run under your user account and share normal workspace file permissions — pair with OS permissions or containers if executing untrusted code.',
      bnd_3: '<strong>Native Browser Opt-In:</strong> Modern Chromium/Arc blocks silent unsigned extension injection; PeaceClaude generates the auditable MV3 folder locally and guides a 10-second developer-mode load.',
      ins_kicker: '4-PLATFORM RELEASES · v0.2.0',
      ins_title: 'Deploy everywhere your Claude workflow runs.',
      ins_copy: 'Copy Steps',
      ins_docs: 'GitHub Repository',
      foot_note: 'Cross-Platform Zero-VM Static-IP Security Cockpit',
      tab_acc: 'Multi-Account',
      tab_sec: 'Security Doctor',
      tab_nb: 'Native Browser Shield',
      tab_app: 'Appearance Studio',
      tab_rem: 'Mobile Pairing',
      tab_cli: 'CLI Guide',
    },
    zh: {
      nav_arch: '安全透视',
      nav_guide: '使用教程',
      nav_ecosystem: '三端生态',
      nav_surfaces: '六层防御',
      nav_preview: '界面画廊',
      nav_faq: '常见问答',
      nav_download: '多端下载',
      cta_download_short: '快速上手',
      hero_eyebrow: 'macOS · Windows · Ubuntu · iOS PeaceSwitch · ~18 MB 零虚拟机',
      hero_title: '宿主机原生速度。<br/>确定性的静态 IP 安全边界。',
      hero_lead: '无需背负 8 GB 虚拟机，告别裸代理泄漏与串号风险。PeaceClaude 现已覆盖 macOS、Windows 与 Ubuntu Linux 全端网关，并配备纯正 SwiftUI 原生 iOS 伴侣应用 PeaceSwitch，随时随地一键切号与紧急熔断。',
      hero_cta_primary: '查看四端上手指南',
      copy_label: '复制',
      metric_ram: '网关内存占用',
      metric_boot: '冷启动速度',
      metric_risk: '指纹检测得分',
      metric_deps: 'Go 纯标准库依赖',
      deck_title: 'Peace Switch 链路透视舱',
      deck_sub: '点击开关实时对比「裸奔状态」与「PeaceClaude 守护态」',
      deck_score_label: '风险评估',
      dk_egress: '出口路由',
      dk_tz: '运行时区',
      dk_locale: '语言与字体',
      dk_kernel: '内核熔断防线',
      guide_kicker: '交互式四端上手指南',
      guide_title: '选择您的设备，2 分钟跑通静态 IP 隔离闭环。',
      guide_desc: '点击下方任意系统标签，按 4 步即可完成网关安装、静态节点绑定、原生客户端防护与手机扫码配对。',
      eco_kicker: 'PC · iOS · WEB 三端协同架构',
      eco_title: '同一套本地安全核心，三端状态实时同步。',
      eco_desc: '电脑前用命令行或 Web 工作台高效编码；起身离开电脑后，打开 iPhone 上的 PeaceSwitch 即可查看配额、切换账号或瞬间掐断出口门禁。',
      eco_pc_t: 'PC 桌面与命令行（macOS / Windows / Ubuntu）',
      eco_pc_d: '单文件零依赖 Go 网关守护，支持 LaunchAgent / Windows 注册表 / Linux systemd --user 自启动、系统级域内 PAC 以及 macOS 物理网卡企业 VPN 旁路直连。',
      eco_ios_t: 'PeaceSwitch 原生 iOS 应用（Pure SwiftUI）',
      eco_ios_d: '零 WebView 封装、零第三方追踪 SDK：支持 AVFoundation 扫码 120s 配对、系统 Keychain 存储令牌、震动反馈紧急熔断、多账号实时余量及无服务端互动演示模式。',
      eco_web_t: '零依赖本机 Web 控制台（127.0.0.1:18899）',
      eco_web_d: '内置于网关进程之中：提供 11 项全链路 Doctor 体检、外观工坊主题系统、403 三分诊定位与 Arc/Chrome MV3 扩展多选引导。',
      ios_mock_title: 'PeaceSwitch 工作台',
      ios_mock_ip_lbl: '已验证静态出口 IP',
      ios_mock_acc_lbl: '隔离多账号（点击体验切换）',
      ios_mock_q1: '5h 已用 38% · 本周 54%',
      ios_mock_q2: '免费账号 · 无 5h / Weekly 配额',
      ios_mock_lock_btn: '触发紧急熔断',
      ios_mock_active: '当前使用',
      ios_mock_standby: '待命',
      ios_tab_cockpit: '工作台',
      ios_tab_accounts: '多账号',
      ios_tab_security: '安全设置',
      cmp_kicker: '零虚拟机系统工程',
      cmp_title: '为什么只为锁住 IP 与时区，就要拖着 8 GB 虚拟机？',
      cmp_desc: 'PeaceClaude 在用户态与系统边界协同完成 Fail-Closed 熔断与全入口指纹对齐，保持设备静音、省电与原生流畅手感。',
      b_ram_title: '常驻内存开销',
      b_boot_title: '即开即用速度',
      b_disk_title: '磁盘占用空间',
      b_vpn_title: '企业 VPN 共存兼容',
      lbl_vm: '传统 Linux 桌面虚拟机',
      lbl_tun: '全局 TUN / 桥接网卡',
      val_no_tun: '绝不开 TUN · 零冲突',
      val_conflict: '路由冲突断网',
      bento_kicker: '全入口统一架构',
      bento_title: '六层精密安全防线，收口于一处工作台。',
      bento_desc: '从防泄漏网关到底层浏览器 MV3 扩展，每个客户端始终严格跟随当前激活档案。',
      f1_t: 'Fail-Closed 门禁 + 防直连护盾',
      f1_d: '四源并发探测静态出口，IP 漂移或手动锁屏秒返 503、主动掐断活跃长连接并拦截非代理直连泄漏。',
      f2_t: '原生 Arc / Chrome 多选护航',
      f2_d: '一键多选本机浏览器：叠加仅分流 Claude 的 PAC 与域名定向 MV3 扩展，达成 2/100 LOW RISK 且不扰动日常网页。',
      f3_t: '多账号物理隔离与单活切号',
      f3_d: '独立配置、Desktop 目录与设备 UUID；自动区分 Free 与 Pro/Max 套餐，深度链接精准路由到活跃窗口。',
      f4_t: '11 项安全体检与 403 三分诊',
      f4_d: '实测 SOCKS5 远端 DNS、禁 QUIC、WebRTC UDP 阻断与直连审计；智能区分网络瞬断与真实鉴权失效。',
      f5_t: 'PeaceSwitch iOS 扫码随身控',
      f5_d: '签发 120 秒一次性 QR 二维码与 SHA-256 限权令牌，手机上直接看用量、切换激活账号或一键熔断锁网。',
      f6_t: '30 天全脱敏可审计观测',
      f6_d: '请求抓包与 Split-IP 审计自动遮蔽 Authorization、Cookie 与 OAuth Code，以 0600 权限落盘滚动清理。',
      show_kicker: '实景界面画廊',
      show_title: '精密仪器般克制的工作台体验。',
      show_desc: '零外部前端构建依赖内嵌渲染，点击下方标签切换查看 PeaceClaude v0.2.0 真实脱敏截图。',
      faq_kicker: '常见问题与架构答疑',
      faq_title: '迁移到零虚拟机前，工程师最关心的 5 个问题。',
      faq_q1: '开启后会不会导致飞书、Slack 或公司内网变慢？',
      faq_a1: '完全不会。系统 PAC 与 MV3 护航扩展通过精确域名白名单仅匹配 <code>claude.ai</code>、<code>claude.com</code>、<code>anthropic.com</code>、<code>claudeusercontent.com</code> 与 <code>fuck-claude.app</code>。飞书、GitHub 与日常网页一律直连，且保留您本机原生时区与语言。',
      faq_q2: '公司强制开着飞连等企业 VPN，还能稳用静态美国 IP 吗？',
      faq_a2: '可以。PeaceClaude 绝不创建全局 TUN 虚拟网卡；在 macOS 上更支持物理网卡直连绑定（<code>IP_BOUND_IF</code>，如直出 <code>en0</code>），上游探测与静态节点请求直接绕开企业 VPN 路由干扰，日常办公与安全分流互不抢路。',
      faq_q3: '目前支持哪些桌面操作系统和浏览器？',
      faq_a3: '桌面服务端支持 macOS Universal（Apple Silicon + Intel）、Windows 10/11（amd64 + arm64）以及 Ubuntu / Debian Linux（提供 <code>.deb</code> 安装包与 systemd --user 自启服务）。原生浏览器护航兼容 Arc、Chrome、Brave 与 Microsoft Edge。',
      faq_q4: '没有配置桌面网关之前，iOS 端 PeaceSwitch 能否直接试用？',
      faq_a4: '可以。首次打开 PeaceSwitch 点击底部的「进入互动演示模式 / Launch Interactive Demo Mode」，即可在离线拟真沙盒中完整操作多账号切换、Free/Pro 配额查看、Fail-Closed 紧急锁网与 PAC 开关。',
      faq_q5: '如何在保持主源码仓库 Private 私有的前提下部署独立官网（如 peace-claude.app）？',
      faq_a5: '<code>website/</code> 目录为纯静态网页零构建步骤。推荐直接在 Vercel 或 Cloudflare Pages 导入仓库（Root Directory 填 <code>website</code>）并绑定 <code>peace-claude.app</code> 自定义域名，这样您的 GitHub 主代码仓可永久保持 Private 私有。仓内也已备妥 <code>.github/workflows/pages.yml</code> 与详细部署文档。',
      bnd_kicker: '诚实工程边界',
      bnd_title: '明确零虚拟机能保什么，以及边界在哪里。',
      bnd_1: '<strong>网络与指纹层保障：</strong>Fail-Closed 静态 IP 路由、防火墙防直连、独立档案目录、宿主中转变量剥离，以及 Claude 域定向时区/语言/中文字体对齐。',
      bnd_2: '<strong>同操作系统用户权限边界：</strong>进程与您处于同一操作系统 UID 空间下；若运行不可信第三方脚本需文件系统强隔离，建议额外叠加系统权限或容器边界。',
      bnd_3: '<strong>原生浏览器扩展机制：</strong>现代 Chromium/Arc 禁止静默植入未上架商店扩展；PeaceClaude 在本地生成可审计扩展源码并引导您 10 秒开启加载。',
      ins_kicker: '四端同步发布 · v0.2.0',
      ins_title: '覆盖您的每一台主力电脑与随身 iPhone。',
      ins_copy: '复制步骤',
      ins_docs: '查看 GitHub 仓库',
      foot_note: '跨平台零虚拟机静态 IP Claude 全入口安全套件',
      tab_acc: '多账号工作台',
      tab_sec: '11 项安全体检',
      tab_nb: '原生浏览器护航',
      tab_app: '外观工坊',
      tab_rem: '手机扫码配对',
      tab_cli: 'CLI 速查',
    },
  };

  const GUIDE_DATA = {
    en: {
      macos: {
        cmdLabel: 'macOS Quick Setup',
        cmd: `chmod +x ./install.sh && ./install.sh
peaceclaude config set-upstream --mode direct-socks5 \\
  --addr <HOST>:<PORT> --user <USER> --pass <PASS> --expected-ip <STATIC_IP>
peaceclaude browser-pac on && peaceclaude doctor --fix
peaceclaude cli`,
        steps: [
          { num: 'STEP 01', title: 'Install Universal Package', desc: 'Open DMG or run <code>./install.sh</code>. Installs <code>~/.local/bin/peaceclaude</code>, LaunchAgent autostart, and PeaceClaude.app.' },
          { num: 'STEP 02', title: 'Bind Static IP & Bypass VPN', desc: 'Set your SOCKS5/HTTP static residential IP. macOS automatically binds physical interface <code>en0</code> to bypass corporate VPN clashes.' },
          { num: 'STEP 03', title: 'Launch Guarded Surfaces', desc: 'Run <code>peaceclaude cli</code>, launch isolated Desktop, or toggle PAC + MV3 Shield for Arc/Chrome (achieves 2/100 LOW RISK).' },
          { num: 'STEP 04', title: 'Pair PeaceSwitch on iPhone', desc: 'Open Console -> Remote Pairing for a 120s QR ticket. Scan via PeaceSwitch on iPhone over LAN or Tailscale.' },
        ],
      },
      windows: {
        cmdLabel: 'Windows PowerShell Setup',
        cmd: `Expand-Archive peaceclaude-v0.2.0-windows-amd64.zip -DestinationPath $HOME\\peaceclaude
$env:Path += ";$HOME\\peaceclaude"
peaceclaude.exe service install
peaceclaude.exe config set-upstream --mode direct-socks5 --addr <HOST>:<PORT> --expected-ip <STATIC_IP>
peaceclaude.exe status --check`,
        steps: [
          { num: 'STEP 01', title: 'Extract Release Zip', desc: 'Download <code>peaceclaude-v0.2.0-windows-amd64.zip</code> (or <code>arm64</code>) and place <code>peaceclaude.exe</code> on your PATH.' },
          { num: 'STEP 02', title: 'Register HKCU Run Autostart', desc: 'Run <code>peaceclaude.exe service install</code> to write the HKCU Run entry cleanly without UAC admin elevation.' },
          { num: 'STEP 03', title: 'Configure System Scoped PAC', desc: 'Toggle Browser PAC in console or CLI: updates Windows <code>Internet Settings\\AutoConfigURL</code> strictly for Claude domains.' },
          { num: 'STEP 04', title: 'Verify Gate & Run CLI / App', desc: 'Confirm <code>OPEN</code> static IP with <code>peaceclaude.exe status --check</code>, then launch isolated Claude CLI or Desktop.' },
        ],
      },
      ubuntu: {
        cmdLabel: 'Ubuntu / Debian (.deb + systemd --user)',
        cmd: `sudo dpkg -i peaceclaude_0.2.0_amd64.deb
peaceclaude service install
peaceclaude config set-upstream --mode direct-socks5 \\
  --addr <HOST>:<PORT> --user <USER> --pass <PASS> --expected-ip <STATIC_IP>
peaceclaude cli --profile work`,
        steps: [
          { num: 'STEP 01', title: 'Install Debian Package', desc: 'Install <code>peaceclaude_0.2.0_amd64.deb</code> (or extract Linux <code>.tar.gz</code>). Drops binary to <code>/usr/local/bin</code>.' },
          { num: 'STEP 02', title: 'Enable systemd --user Unit', desc: 'Run <code>peaceclaude service install</code> to enable and start <code>~/.config/systemd/user/peaceclaude.service</code>.' },
          { num: 'STEP 03', title: 'GNOME PAC & MV3 Shield', desc: 'Desktop environments apply <code>gsettings org.gnome.system.proxy autoconfig-url</code> automatically alongside Chromium MV3.' },
          { num: 'STEP 04', title: 'Open Web Workbench', desc: 'Visit <code>http://127.0.0.1:18899</code> in your Linux browser to inspect 11-point Doctor scores and pair your phone.' },
        ],
      },
      ios: {
        cmdLabel: 'Desktop Side Pairing Prep for PeaceSwitch iOS',
        cmd: `# Enable mobile companion listener (LAN / Tailscale)
peaceclaude remote enable --mode lan
# Or open desktop Web Console -> Remote Pairing -> Generate 120s QR`,
        steps: [
          { num: 'STEP 01', title: 'Try Interactive Demo First', desc: 'Download <strong>PeaceSwitch</strong> (or build via Xcode in <code>ios/PeaceSwitch</code>) and tap <em>Launch Interactive Demo Mode</em> to test zero-server.' },
          { num: 'STEP 02', title: 'Generate 120s QR on PC', desc: 'Ensure your iPhone is on the same Wi-Fi or Tailscale network, then open Remote Pairing on your Mac/PC console.' },
          { num: 'STEP 03', title: 'Scan & Store Token in Keychain', desc: 'Tap <em>Scan QR Code</em> in PeaceSwitch. The redeemable single-use code issues a SHA-256 backed bearer token into iOS Keychain.' },
          { num: 'STEP 04', title: 'One-Tap Lock & Quota Check', desc: 'Check Pro/Max 5h/Weekly limits, switch isolated accounts, toggle Browser PAC, or engage Fail-Closed Emergency Lock anytime.' },
        ],
      },
    },
    zh: {
      macos: {
        cmdLabel: 'macOS 一键安装与启动命令',
        cmd: `chmod +x ./install.sh && ./install.sh
peaceclaude config set-upstream --mode direct-socks5 \\
  --addr <HOST>:<PORT> --user <USER> --pass <PASS> --expected-ip <STATIC_IP>
peaceclaude browser-pac on && peaceclaude doctor --fix
peaceclaude cli`,
        steps: [
          { num: 'STEP 01', title: '安装 Universal 双架构套件', desc: '挂载 DMG 或执行 <code>./install.sh</code>，自动部署 <code>peaceclaude</code> CLI、开机 LaunchAgent 守护进程与原生工作台应用。' },
          { num: 'STEP 02', title: '绑定静态 IP 并绕过企业 VPN', desc: '填入你的静态住宅 SOCKS5/HTTP 出口。macOS 自动绑定物理网卡 <code>en0</code>（<code>IP_BOUND_IF</code>）避开飞连等 VPN 抢路。' },
          { num: 'STEP 03', title: '启动全入口护航', desc: '直接运行 <code>peaceclaude cli</code>、隔离 Desktop，或在工作台多选 Arc/Chrome 加载仅护航 Claude 的 MV3 扩展。' },
          { num: 'STEP 04', title: '连接 PeaceSwitch 手机端', desc: '打开工作台「远程配对」签发 120 秒一次性二维码，用 iPhone PeaceSwitch 扫码接入。' },
        ],
      },
      windows: {
        cmdLabel: 'Windows PowerShell 快速部署',
        cmd: `Expand-Archive peaceclaude-v0.2.0-windows-amd64.zip -DestinationPath $HOME\\peaceclaude
$env:Path += ";$HOME\\peaceclaude"
peaceclaude.exe service install
peaceclaude.exe config set-upstream --mode direct-socks5 --addr <HOST>:<PORT> --expected-ip <STATIC_IP>
peaceclaude.exe status --check`,
        steps: [
          { num: 'STEP 01', title: '解压 Windows 绿色发行包', desc: '下载 <code>peaceclaude-v0.2.0-windows-amd64.zip</code>（或 <code>arm64</code>）解压并加入系统 PATH。' },
          { num: 'STEP 02', title: '免管理员权限开机自启动', desc: '运行 <code>peaceclaude.exe service install</code> 自动写入当前用户注册表 <code>HKCU\\...\\Run</code> 静默自启项。' },
          { num: 'STEP 03', title: '自动切换域内系统 PAC', desc: '开启 PAC 护航后自动写入 Windows <code>Internet Settings\\AutoConfigURL</code>，严格只接管 Claude 相关域名。' },
          { num: 'STEP 04', title: '门禁复检并启动 CLI / 桌面端', desc: '运行 <code>peaceclaude.exe status --check</code> 确认显示 <code>OPEN</code> 后，打开隔离版 CLI 或浏览器工作台。' },
        ],
      },
      ubuntu: {
        cmdLabel: 'Ubuntu / Debian (.deb 与 systemd --user)',
        cmd: `sudo dpkg -i peaceclaude_0.2.0_amd64.deb
peaceclaude service install
peaceclaude config set-upstream --mode direct-socks5 \\
  --addr <HOST>:<PORT> --user <USER> --pass <PASS> --expected-ip <STATIC_IP>
peaceclaude cli --profile work`,
        steps: [
          { num: 'STEP 01', title: '安装 .deb 安装包', desc: '执行 <code>dpkg -i peaceclaude_0.2.0_amd64.deb</code>（或解压 <code>.tar.gz</code>），一键安装至 <code>/usr/local/bin</code>。' },
          { num: 'STEP 02', title: '注册 systemd --user 常驻服务', desc: '执行 <code>peaceclaude service install</code> 自动配置并启动 <code>~/.config/systemd/user/peaceclaude.service</code>。' },
          { num: 'STEP 03', title: 'GNOME PAC 与 Chrome 扩展协同', desc: '通过 <code>gsettings</code> 自动下发系统 PAC 地址，并结合 Chrome/Brave 开发者扩展完成域内指纹护盾。' },
          { num: 'STEP 04', title: '浏览器打开本地控制台', desc: '访问 <code>http://127.0.0.1:18899</code> 查看 11 项体检分数并向 PeaceSwitch iPhone 端配对。' },
        ],
      },
      ios: {
        cmdLabel: '电脑端配对准备（供 iPhone PeaceSwitch 扫码）',
        cmd: `# 开启局域网 / Tailscale 手机伴侣监听
peaceclaude remote enable --mode lan
# 随后在电脑浏览器控制台进入「远程配对」生成 120 秒二维码`,
        steps: [
          { num: 'STEP 01', title: '支持零服务端互动演示', desc: '安装并打开 <strong>PeaceSwitch</strong>（可在 <code>ios/PeaceSwitch</code> 用 Xcode 直接真机构建），点「进入互动演示模式」即刻体验全部控制逻辑。' },
          { num: 'STEP 02', title: 'PC 控制台生成 120 秒二维码', desc: '确认电脑和 iPhone 位于同一局域网 Wi-Fi 或已连通 Tailscale，在桌面工作台生成一次性配对二维码。' },
          { num: 'STEP 03', title: '扫码安全入库 iOS Keychain', desc: '用 PeaceSwitch 点击「立即扫码配对」，一次性验证码换取可随时撤回的 SHA-256 令牌并加密存入 Keychain。' },
          { num: 'STEP 04', title: '掌上监控、切号与一键熔断', desc: '随时查看 Pro/Max 5h 与 Weekly 余量进度、唤醒电脑端账号，或在离桌时触发紧急 Fail-Closed 熔断。' },
        ],
      },
    },
  };

  let curLang = 'en';
  let switchSafe = true;
  let guidePlatform = 'macos';
  let installMode = 'macos';
  let iosLocked = false;
  let iosActiveAccount = 'alpha';
  let curShotTab = document.querySelector('.sc-tab.active');

  const INSTALL_SNIPPETS = {
    macos: `# macOS Universal (Apple Silicon + Intel)
tar -xzf peaceclaude-v0.2.0-darwin-universal.tar.gz
cd peaceclaude-v0.2.0-darwin-universal
./install.sh
peaceclaude status --check`,
    windows: `# Windows 10 / 11 (amd64 / arm64) PowerShell
Expand-Archive peaceclaude-v0.2.0-windows-amd64.zip -DestinationPath $HOME\\peaceclaude
$env:Path += ";$HOME\\peaceclaude"
peaceclaude.exe service install
peaceclaude.exe status --check`,
    ubuntu: `# Ubuntu / Debian Linux (amd64 / arm64)
sudo dpkg -i peaceclaude_0.2.0_amd64.deb
peaceclaude service install
peaceclaude status --check`,
    ios: `# iOS Companion App (PeaceSwitch · Pure SwiftUI)
git clone https://github.com/dragonforce2010/peaceclaude.git
cd peaceclaude/ios/PeaceSwitch
xcodegen generate && open PeaceSwitch.xcodeproj`,
  };

  function escapeHTML(str) {
    return String(str).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }

  function renderTelemetryDeck() {
    const deck = $('telemetry-deck');
    if (!deck) return;
    deck.dataset.state = switchSafe ? 'safe' : 'risk';
    const btn = $('peace-switch-btn');
    if (btn) btn.setAttribute('aria-checked', String(switchSafe));

    if (curLang === 'zh') {
      $('switch-state-text').textContent = switchSafe ? '护航开启' : '裸奔暴露';
      $('deck-verdict-badge').textContent = switchSafe ? 'OPEN · FAIL-CLOSED' : 'ALERT · 暴露直连风险';
      $('deck-verdict-headline').textContent = switchSafe ? '静态美国出口已锁 · 零泄漏' : '检测到中国区指纹 + Split-IP 偏差';
      $('deck-risk-score').textContent = switchSafe ? '2 / 100' : '78 / 100';
      $('dv-egress').textContent = switchSafe ? '203.0.113.42 · 美国夏延 (全链路同 IP)' : '代理闪断回退 / Split-IP 双出口';
      $('dv-tz').textContent = switchSafe ? 'America/Denver (UTC-7)' : 'Asia/Shanghai (UTC+8 · 高权重命中)';
      $('dv-locale').textContent = switchSafe ? 'en-US · Canvas 中文字体护盾已启' : 'zh-CN · 暴露简体中文字体列表';
      $('dv-kernel').textContent = switchSafe ? '503 硬拦截 + 防直连护盾激活' : '无 Fail-Closed · IPv6/UDP 可直连';
      $('sc-cli-note').textContent = switchSafe ? 'TZ 对齐 + 剥离宿主变量' : '沿用 Asia/Shanghai';
      $('sc-desktop-note').textContent = switchSafe ? 'Auto-Peace + 禁 QUIC' : '暴露系统 locale / UDP';
      $('sc-browser-note').textContent = switchSafe ? 'PAC + 定向 MV3 扩展' : '漏出国区时区与语言';
    } else {
      $('switch-state-text').textContent = switchSafe ? 'GUARDED' : 'EXPOSED';
      $('deck-verdict-badge').textContent = switchSafe ? 'OPEN · FAIL-CLOSED' : 'ALERT · LEAK RISK';
      $('deck-verdict-headline').textContent = switchSafe ? 'Static US IP Verified · Zero Host Leaks' : 'China Fingerprint + Split-IP Drift';
      $('deck-risk-score').textContent = switchSafe ? '2 / 100' : '78 / 100';
      $('dv-egress').textContent = switchSafe ? '203.0.113.42 · Cheyenne, US (Single-IP)' : 'Direct Fallback / Split-IP Egress';
      $('dv-tz').textContent = switchSafe ? 'America/Denver (UTC-7)' : 'Asia/Shanghai (UTC+8 · Flagged)';
      $('dv-locale').textContent = switchSafe ? 'en-US · Canvas CJK Scrubber ON' : 'zh-CN · Chinese Font List Exposed';
      $('dv-kernel').textContent = switchSafe ? 'HTTP 503 Guard + Kernel Anchor Armed' : 'No Fail-Closed · Raw IPv6/UDP Open';
      $('sc-cli-note').textContent = switchSafe ? 'TZ + Env Stripped' : 'Host TZ Exposed';
      $('sc-desktop-note').textContent = switchSafe ? 'Auto-Peace & QUIC Off' : 'Host Locale / QUIC';
      $('sc-browser-note').textContent = switchSafe ? 'PAC + MV3 Shield' : 'Direct / Fingerprint Leak';
    }
  }

  function renderGuide() {
    const grid = $('guide-steps-grid');
    if (!grid) return;
    const data = (GUIDE_DATA[curLang] && GUIDE_DATA[curLang][guidePlatform]) || GUIDE_DATA.en.macos;
    grid.innerHTML = data.steps.map((s) => `
      <article class="guide-step-card">
        <span class="step-num">${escapeHTML(s.num)}</span>
        <h3>${escapeHTML(s.title)}</h3>
        <p>${s.desc}</p>
      </article>
    `).join('');
    if ($('guide-cmd-label')) $('guide-cmd-label').textContent = data.cmdLabel;
    if ($('guide-cmd-code')) $('guide-cmd-code').innerHTML = '<code>' + escapeHTML(data.cmd) + '</code>';
  }

  function renderIOSMockup() {
    const chassis = $('ios-mockup');
    if (!chassis) return;
    chassis.dataset.locked = String(iosLocked);
    const pill = $('ios-gate-pill');
    const meta = $('ios-egress-meta');
    const lockBtn = $('btn-ios-lock');
    if (curLang === 'zh') {
      if (pill) pill.textContent = iosLocked ? '已熔断' : '已放行';
      if (meta) meta.textContent = iosLocked ? 'Fail-Closed 硬截断 · 等待解锁' : 'Cheyenne, US · en0 物理直连 · 84 ms';
      if (lockBtn) lockBtn.textContent = iosLocked ? '恢复静默放行' : '触发紧急熔断';
    } else {
      if (pill) pill.textContent = iosLocked ? 'LOCKED' : 'OPEN';
      if (meta) meta.textContent = iosLocked ? 'Fail-Closed Armed · Traffic Halted' : 'Cheyenne, US · en0 Bypass · 84 ms';
      if (lockBtn) lockBtn.textContent = iosLocked ? 'Unlock Egress Gate' : 'Trigger Emergency Lock';
    }

    document.querySelectorAll('.ios-acc-row').forEach((row) => {
      const isAct = row.dataset.iosAcc === iosActiveAccount;
      row.classList.toggle('active', isAct);
      const tag = row.querySelector('.ios-active-tag');
      if (tag) {
        tag.classList.toggle('muted', !isAct);
        tag.textContent = curLang === 'zh'
          ? (isAct ? '当前使用' : '待命')
          : (isAct ? 'ACTIVE' : 'STANDBY');
      }
    });
  }

  function applyLang(lang) {
    curLang = lang === 'zh' ? 'zh' : 'en';
    document.documentElement.lang = curLang === 'zh' ? 'zh-CN' : 'en';
    const dict = I18N[curLang];
    document.querySelectorAll('[data-i18n]').forEach((el) => {
      const k = el.dataset.i18n;
      if (dict[k]) el.innerHTML = dict[k];
    });
    if ($('lang-label')) $('lang-label').textContent = curLang === 'zh' ? 'EN' : '中文';
    if (curShotTab) {
      $('showcase-caption').textContent = curLang === 'zh' ? curShotTab.dataset.captionZh : curShotTab.dataset.captionEn;
    }
    renderTelemetryDeck();
    renderGuide();
    renderIOSMockup();
    try { localStorage.setItem('peaceclaude_site_lang', curLang); } catch {}
  }

  function applyTheme(theme) {
    const t = theme === 'dark' ? 'dark' : 'light';
    document.documentElement.setAttribute('data-theme', t);
    if ($('theme-icon')) $('theme-icon').textContent = t === 'dark' ? '☀' : '◐';
    try { localStorage.setItem('peaceclaude_site_theme', t); } catch {}
  }

  $('peace-switch-btn')?.addEventListener('click', () => {
    switchSafe = !switchSafe;
    renderTelemetryDeck();
  });

  $('guide-tabs')?.addEventListener('click', (ev) => {
    const btn = ev.target.closest('.guide-tab');
    if (!btn) return;
    document.querySelectorAll('.guide-tab').forEach((b) => b.classList.remove('active'));
    btn.classList.add('active');
    guidePlatform = btn.dataset.platform || 'macos';
    renderGuide();
  });

  $('btn-ios-lock')?.addEventListener('click', () => {
    iosLocked = !iosLocked;
    renderIOSMockup();
  });

  document.querySelectorAll('.ios-acc-row').forEach((row) => {
    row.addEventListener('click', () => {
      iosActiveAccount = row.dataset.iosAcc || 'alpha';
      renderIOSMockup();
    });
  });

  $('btn-lang-toggle')?.addEventListener('click', () => {
    applyLang(curLang === 'en' ? 'zh' : 'en');
  });

  $('btn-theme-toggle')?.addEventListener('click', () => {
    const cur = document.documentElement.getAttribute('data-theme') || 'light';
    applyTheme(cur === 'light' ? 'dark' : 'light');
  });

  $('showcase-tabs')?.addEventListener('click', (ev) => {
    const btn = ev.target.closest('.sc-tab');
    if (!btn) return;
    document.querySelectorAll('.sc-tab').forEach((b) => b.classList.remove('active'));
    btn.classList.add('active');
    curShotTab = btn;
    $('showcase-img').src = 'assets/' + btn.dataset.shot;
    $('showcase-caption').textContent = curLang === 'zh' ? btn.dataset.captionZh : btn.dataset.captionEn;
  });

  $('install-tabs')?.addEventListener('click', (ev) => {
    const btn = ev.target.closest('.ins-tab');
    if (!btn) return;
    document.querySelectorAll('.ins-tab').forEach((b) => b.classList.remove('active'));
    btn.classList.add('active');
    installMode = btn.dataset.mode || 'macos';
    $('install-code').innerHTML = '<code>' + escapeHTML(INSTALL_SNIPPETS[installMode] || '') + '</code>';
  });

  $('btn-hero-copy')?.addEventListener('click', async () => {
    const cmd = ($('hero-copy-cmd')?.textContent || './install.sh').trim();
    try { await navigator.clipboard.writeText(cmd); } catch {}
    const tag = $('hero-copy-tag');
    if (tag) {
      tag.textContent = curLang === 'zh' ? '已复制' : 'Copied!';
      setTimeout(() => { tag.textContent = I18N[curLang].copy_label; }, 1600);
    }
  });

  $('btn-copy-guide')?.addEventListener('click', async () => {
    const data = (GUIDE_DATA[curLang] && GUIDE_DATA[curLang][guidePlatform]) || GUIDE_DATA.en.macos;
    try { await navigator.clipboard.writeText(data.cmd); } catch {}
    const btn = $('btn-copy-guide');
    if (btn) {
      btn.textContent = curLang === 'zh' ? '已复制!' : 'Copied!';
      setTimeout(() => { btn.textContent = I18N[curLang].ins_copy; }, 1600);
    }
  });

  $('btn-copy-install')?.addEventListener('click', async () => {
    try { await navigator.clipboard.writeText(INSTALL_SNIPPETS[installMode] || ''); } catch {}
    const btn = $('btn-copy-install');
    if (btn) {
      btn.textContent = curLang === 'zh' ? '已复制!' : 'Copied!';
      setTimeout(() => { btn.textContent = I18N[curLang].ins_copy; }, 1600);
    }
  });

  // Boot
  let savedTheme = 'light';
  let savedLang = 'en';
  try {
    savedTheme = localStorage.getItem('peaceclaude_site_theme') || 'light';
    savedLang = localStorage.getItem('peaceclaude_site_lang') || 'en';
  } catch {}
  applyTheme(savedTheme);
  applyLang(savedLang);
})();
