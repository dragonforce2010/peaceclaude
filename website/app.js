(() => {
  const $ = (id) => document.getElementById(id);

  const I18N = {
    en: {
      nav_arch: 'Cockpit',
      nav_compare: 'Zero-VM',
      nav_surfaces: 'Shields',
      nav_preview: 'Showcase',
      nav_download: 'Install',
      cta_download_short: 'Get v0.2.0',
      hero_eyebrow: 'macOS Universal (arm64 + x86_64) · ~18 MB Zero-VM Architecture',
      hero_title: 'Native macOS Speed.<br/>Deterministic Static-IP Safety.',
      hero_lead: 'Ditch the 8 GB Linux VM without risking naked proxy leaks. PeaceClaude wraps Claude Desktop, Claude Code CLI, and your native Arc & Chrome browsers in a Fail-Closed static-IP gate, synced US timezone/locale, and verified 2/100 anti-fingerprint shield.',
      hero_cta_primary: 'Download Universal DMG',
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
      cmp_kicker: 'ZERO-VM ENGINEERING',
      cmp_title: 'Why carry an 8 GB Linux VM just to lock an IP and timezone?',
      cmp_desc: 'PeaceClaude enforces deterministic network fail-closing and browser/CLI fingerprint alignment at the OS user-space + pf kernel boundary — keeping macOS instant, silent, and battery-friendly.',
      b_ram_title: 'Resident Memory',
      b_boot_title: 'Ready-to-Code Startup',
      b_disk_title: 'Disk Footprint',
      b_vpn_title: 'Corporate VPN (Feilian / Cisco)',
      lbl_vm: 'Linux Desktop VM',
      lbl_tun: 'System TUN / VM Bridge',
      val_no_tun: 'No TUN · Coexists',
      val_conflict: 'Route Clash',
      bento_kicker: 'FULL-SURFACE ARCHITECTURE',
      bento_title: 'Six engineered defense layers in one cohesive app.',
      bento_desc: 'From kernel packet anchors to browser MV3 Content Scripts, every surface stays aligned with your active profile.',
      f1_t: 'Fail-Closed Egress Gate + pf',
      f1_d: '4-source parallel IP probe verifies your static node. On any IP drift or manual lock, the gateway returns hard 503, severs active streams, and drops direct IPv4/IPv6/UDP via pfctl.',
      f2_t: 'Native Arc & Chrome MV3 Shield',
      f2_d: 'Select Arc, Chrome, Brave, or Edge in the console: pairs Claude-only PAC routing with a domain-scoped MV3 extension achieving 2/100 LOW RISK without touching Slack, Lark, or work tabs.',
      f3_t: 'Isolated Multi-Account Cockpit',
      f3_d: 'Separate config, Desktop user-data-dir, cookies, and UUID per account. Detects Free vs Pro/Max automatically, routes claude:// OAuth callbacks strictly to the active window, and guards native Claude.app launches.',
      f4_t: '11-Point Doctor & 403 Triage',
      f4_d: 'Verifies remote SOCKS5 DNS, --disable-quic, WebRTC UDP suppression, and host socket scans. Smart 3-way diagnose separates transient network drops from real auth expiration before you touch Sign Out.',
      f5_t: 'LAN & Tailscale Mobile Remote',
      f5_d: 'Issue 120-second ISO-18004 SVG QR pairing passes backed by SHA-256 Bearer tokens to check quotas, swap active profiles, or lock egress from your phone with zero cloud relay servers.',
      f6_t: '30-Day Redacted Observability',
      f6_d: 'Local packet capture and Split-IP leak logs strip Authorization headers, Cookies, and OAuth tokens before hitting 0600 disk files, auto-pruned on a rolling 30-day schedule.',
      show_kicker: 'LIVE WORKBENCH TOUR',
      show_title: 'Crafted as a quiet precision instrument.',
      show_desc: 'Built with zero external frontend frameworks. Inspect real sanitized captures from PeaceClaude v0.2.0 below.',
      bnd_kicker: 'HONEST ENGINEERING',
      bnd_title: "Transparent about what Zero-VM covers — and doesn't.",
      bnd_1: '<strong>Network &amp; Fingerprint Guarantee:</strong> Fail-Closed static-IP routing, kernel <code>pf</code> IPv4/IPv6/UDP killswitch, isolated profile paths, host env scrubbing, and domain-scoped timezone/locale/CJK-font alignment.',
      bnd_2: '<strong>Same macOS UID Boundary:</strong> Unlike a hardware hypervisor VM, processes run under your macOS user account and share normal workspace file permissions — pair with OS permissions or containers if executing untrusted code.',
      bnd_3: '<strong>Native Browser Opt-In:</strong> Modern macOS Chromium/Arc blocks silent unsigned extension injection; PeaceClaude generates the auditable MV3 folder locally and guides a 10-second developer-mode load.',
      ins_kicker: '30-SECOND INSTALL · v0.2.0',
      ins_title: 'Ready for Apple Silicon & Intel Macs.',
      ins_copy: 'Copy Steps',
      ins_docs: 'Read Full Docs',
      foot_note: 'Zero-VM Static-IP Security Cockpit for macOS',
      tab_acc: 'Multi-Account',
      tab_sec: 'Security Doctor',
      tab_nb: 'Native Browser Shield',
      tab_app: 'Appearance Studio',
      tab_rem: 'Mobile Pairing',
      tab_cli: 'CLI Guide',
    },
    zh: {
      nav_arch: '安全透视',
      nav_compare: '零虚拟机对比',
      nav_surfaces: '六层防御',
      nav_preview: '界面画廊',
      nav_download: '安装下载',
      cta_download_short: '获取 v0.2.0',
      hero_eyebrow: 'macOS Universal 双架构通用包 · ~18 MB 零虚拟机架构',
      hero_title: 'macOS 原生速度。<br/>确定性的静态 IP 安全边界。',
      hero_lead: '无需背负 8 GB Linux 虚拟机，也告别裸代理泄漏与串号风险。PeaceClaude 以零外部依赖 Go 门禁将 Claude Desktop、CLI 与原生 Arc/Chrome 统一锁定至静态美区出口、同步城市时区语言，并达成 2/100 LOW RISK 防护水准。',
      hero_cta_primary: '下载 Universal DMG 安装包',
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
      cmp_kicker: '零虚拟机系统工程',
      cmp_title: '为什么只为锁住 IP 与时区，就要拖着 8 GB 虚拟机？',
      cmp_desc: 'PeaceClaude 在 macOS 用户态与内核 pf 防火墙协同完成 Fail-Closed 熔断和全入口指纹对齐，保持笔记本静音、省电与原生流畅手感。',
      b_ram_title: '常驻内存开销',
      b_boot_title: '即开即用速度',
      b_disk_title: '磁盘占用空间',
      b_vpn_title: '企业 VPN（飞连 / 内网）',
      lbl_vm: '传统 Linux 桌面虚拟机',
      lbl_tun: '全局 TUN / 桥接网卡',
      val_no_tun: '绝不开 TUN · 零冲突',
      val_conflict: '路由冲突断网',
      bento_kicker: '全入口统一架构',
      bento_title: '六层精密安全防线，收口于一处工作台。',
      bento_desc: '从内核级丢包锚点到浏览器定向 MV3 扩展，每个客户端始终严格跟随当前激活档案。',
      f1_t: 'Fail-Closed 门禁 + pf 内核兜底',
      f1_d: '四源并发探测静态出口，IP 漂移或手动锁屏秒返 503、主动掐断活跃流，并经 pfctl 静默丢弃直连 IPv4/IPv6/UDP 包。',
      f2_t: '原生 Arc / Chrome 多选护航',
      f2_d: '一键多选本机浏览器：叠加仅分流 Claude 的 PAC 与域名定向 MV3 扩展，在原生浏览器达成 2/100 LOW RISK 且不扰动飞书等工作标签页。',
      f3_t: '多账号物理隔离与单活切号',
      f3_d: '独立配置、Desktop 目录与设备 UUID；自动识别 Free/Pro/Max 套餐，claude:// OAuth 深链严格路由至当前活跃窗口。',
      f4_t: '11 项安全体检与 403 三分诊',
      f4_d: '实测 SOCKS5 远端 DNS、禁 QUIC、WebRTC UDP 阻断与直连 Socket 审计；遇到 403 智能区分 network/auth 避免误退登。',
      f5_t: '局域网 / Tailscale 手机扫码遥控',
      f5_d: '签发 120 秒一次性 SVG 二维码与 SHA-256 限权令牌，手机随时查看余量、切号或触发紧急锁网，绝不经公网第三方中继。',
      f6_t: '30 天全脱敏可审计观测',
      f6_d: '请求抓包与 Split-IP 审计自动遮蔽 Authorization、Cookie 与 OAuth Code，以 0600 权限落盘并按 30 天滚动清理。',
      show_kicker: '实景界面画廊',
      show_title: '精密仪器般克制的工作台体验。',
      show_desc: '零外部前端构建依赖内嵌渲染，点击下方标签切换查看 PeaceClaude v0.2.0 真实脱敏截图。',
      bnd_kicker: '诚实工程边界',
      bnd_title: '明确零虚拟机能保什么，以及边界在哪里。',
      bnd_1: '<strong>网络与指纹层保障：</strong>Fail-Closed 静态 IP 路由、内核 <code>pf</code> 防直连、独立档案目录、宿主中转变量剥离，以及 Claude 域定向时区/语言/中文字体对齐。',
      bnd_2: '<strong>同 macOS UID 边界：</strong>进程与您处于同一 macOS 用户空间下；若运行不可信第三方脚本需文件系统强隔离，建议额外叠加系统权限或容器边界。',
      bnd_3: '<strong>原生浏览器扩展机制：</strong>macOS Chromium/Arc 禁止静默植入未上架商店扩展；PeaceClaude 在本地生成可审计扩展源码并引导您 10 秒开启加载，随后自动实时检测生效态。',
      ins_kicker: '30 秒极速部署 · v0.2.0',
      ins_title: '原生态支持 Apple Silicon 与 Intel Mac。',
      ins_copy: '复制安装命令',
      ins_docs: '阅读完整文档',
      foot_note: 'macOS 零虚拟机静态 IP Claude 全入口安全套件',
      tab_acc: '多账号工作台',
      tab_sec: '11 项安全体检',
      tab_nb: '原生浏览器护航',
      tab_app: '外观工坊',
      tab_rem: '手机扫码配对',
      tab_cli: 'CLI 速查',
    },
  };

  let curLang = 'en';
  let switchSafe = true;
  let installMode = 'dmg';
  let curShotTab = document.querySelector('.sc-tab.active');

  const INSTALL_SNIPPETS = {
    dmg: `# 1. Download PeaceClaude-v0.2.0-macos-universal.dmg from GitHub Releases
# 2. Drag PeaceClaude.app into Applications & run Install-PeaceClaude-CLI.command
# 3. Clear ad-hoc quarantine if prompted on first launch:
xattr -dr com.apple.quarantine /Applications/PeaceClaude.app`,
    tar: `tar -xzf peaceclaude-v0.2.0-macos-universal.tar.gz
cd peaceclaude-v0.2.0-macos-universal
./install.sh
peaceclaude --version`,
    src: `git clone https://github.com/dragonforce2010/peaceclaude.git
cd peaceclaude
./install.sh
peaceclaude status`,
  };

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
      $('dv-kernel').textContent = switchSafe ? '503 硬拦截 + pfctl 锚点激活' : '无 Fail-Closed · IPv6/UDP 可直连';
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
      $('dv-kernel').textContent = switchSafe ? 'HTTP 503 Guard + pfctl Anchor Armed' : 'No Fail-Closed · Raw IPv6/UDP Open';
      $('sc-cli-note').textContent = switchSafe ? 'TZ + Env Stripped' : 'Host TZ Exposed';
      $('sc-desktop-note').textContent = switchSafe ? 'Auto-Peace & QUIC Off' : 'Host Locale / QUIC';
      $('sc-browser-note').textContent = switchSafe ? 'PAC + MV3 Shield' : 'Direct / Fingerprint Leak';
    }
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
    installMode = btn.dataset.mode || 'dmg';
    $('install-code').innerHTML = '<code>' + INSTALL_SNIPPETS[installMode].replace(/&/g, '&amp;').replace(/</g, '&lt;') + '</code>';
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

  $('btn-copy-install')?.addEventListener('click', async () => {
    try { await navigator.clipboard.writeText(INSTALL_SNIPPETS[installMode]); } catch {}
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
