(() => {
  const $ = (id) => document.getElementById(id);
  let state = null;
  let lastDoctor = null;
  let captureMap = new Map();

  function toast(msg) {
    const el = $('toast');
    el.textContent = msg;
    el.classList.remove('hidden');
    clearTimeout(toast._t);
    toast._t = setTimeout(() => el.classList.add('hidden'), 3200);
  }

  async function api(path, opts = {}) {
    const res = await fetch(path, {
      headers: { 'Content-Type': 'application/json' },
      ...opts,
    });
    const text = await res.text();
    let data = {};
    try { data = text ? JSON.parse(text) : {}; } catch { data = { error: text }; }
    if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
    return data;
  }

  function esc(str) {
    return String(str ?? '').replace(/[&<>"']/g, (m) => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
    }[m]));
  }

  function fmtTime(iso) {
    if (!iso) return '—';
    try { return new Date(iso).toLocaleTimeString('zh-CN', { hour12: false }); } catch { return iso; }
  }

  function renderStatus(s) {
    state = s;
    $('version-badge').textContent = `${s.version || 'v0.1.0'} · PID ${s.pid}`;
    $('sb-http-addr').textContent = `${s.config.listen.bind_addr}:${s.config.listen.http_port}`;
    $('sb-socks-addr').textContent = `${s.config.listen.bind_addr}:${s.config.listen.socks5_port}`;
    $('sb-active-profile').textContent = s.active_profile || 'default';

    const gate = s.gate || {};
    const probe = gate.last_probe || {};
    const good = gate.last_good || {};
    const banner = $('gate-banner');
    const pillText = $('gate-pill-text');
    const alertBox = $('gate-alert');
    const lockBtn = $('btn-lock-toggle');

    if (gate.open) {
      banner.dataset.state = 'open';
      pillText.textContent = '安全放行 · Fail-Closed 门禁就绪';
      lockBtn.textContent = '紧急熔断';
    } else {
      banner.dataset.state = 'locked';
      pillText.textContent = gate.manual_lock ? '已手动熔断 · 拒绝一切出站' : '门禁已锁定 · 已阻断直连泄漏';
      lockBtn.textContent = '解除熔断并重检';
    }

    const ip = probe.egress_ip || good.egress_ip || '未探测';
    const expected = gate.expected_ip ? ` (锁定 ${gate.expected_ip})` : '';
    $('hero-ip').textContent = ip + expected;
    const geoParts = [probe.country || good.country, probe.city || good.city, (probe.asn || good.asn) ? `AS${probe.asn || good.asn}` : ''].filter(Boolean);
    $('hero-geo').textContent = geoParts.length ? geoParts.join(' · ') : '—';
    $('hero-tz').textContent = `${gate.effective_timezone || '—'} / ${gate.effective_language || 'en-US'}`;
    $('hero-streams').textContent = String(gate.active_streams || 0);

    if (!gate.open && (gate.lock_reason || probe.failure_reason)) {
      alertBox.textContent = `熔断原因：${gate.lock_reason || probe.failure_reason}`;
      alertBox.classList.remove('hidden');
    } else if (probe.split_ip_detected && !s.config.egress.strict_domain_isolation) {
      alertBox.textContent = `Split-IP 提醒：Claude 域名走 ${probe.egress_ip}，但泛公网走 ${probe.public_split_ip}。建议在“上游与安全策略”切到直连 Webshare SOCKS5 或开启严格域名隔离。`;
      alertBox.classList.remove('hidden');
    } else {
      alertBox.classList.add('hidden');
    }

    const st = s.stats || {};
    $('kpi-proxied').textContent = String(st.proxied_requests || 0);
    $('kpi-blocked-fc').textContent = String(st.blocked_failclosed || 0);
    $('kpi-blocked-tel').textContent = String(st.blocked_telemetry || 0);
    const dl = $('kpi-direct-leaks');
    dl.textContent = String(s.direct_leaks_count || 0);
    dl.className = 'kpi-value ' + ((s.direct_leaks_count || 0) > 0 ? 'accent-red' : 'accent-green');

    $('nav-leak-count').textContent = String(st.leak_count || 0);
    $('nav-cap-count').textContent = String(st.capture_count || 0);
    $('toggle-capture').checked = !!st.capture_enabled;

    renderVMProxy(s.vm_proxy);
    renderRules(s.rules);
    populateSettings(s.config);
  }

  function renderDoctor(doc) {
    lastDoctor = doc;
    const score = doc.score ?? 0;
    $('doctor-score').textContent = String(score);
    const critEl = $('doctor-crit-count');
    if (critEl) critEl.textContent = `${doc.critical_passed ?? 0} / ${doc.critical_total ?? 11}`;
    const fg = $('doctor-ring-fg');
    const circumference = 264;
    fg.style.strokeDashoffset = String(circumference - (circumference * score) / 100);
    fg.style.stroke = score >= 85 ? 'var(--green)' : score >= 60 ? 'var(--amber)' : 'var(--red)';

    const gradeEl = $('doctor-grade');
    gradeEl.className = 'grade-badge ' + (doc.grade || '');
    gradeEl.textContent = doc.grade === 'safe'
      ? `外网已放行 · 关键检查 ${doc.critical_passed}/${doc.critical_total} 通过`
      : doc.grade === 'caution'
      ? `可用但有提醒 · 关键检查 ${doc.critical_passed}/${doc.critical_total} 通过`
      : `存在风险项 · 关键检查 ${doc.critical_passed}/${doc.critical_total} 通过`;
    $('doctor-desc').textContent = `宿主机时区 ${doc.host_timezone} → 隔离注入 ${doc.peace_timezone} / ${doc.peace_language}`;

    if (doc.desktop_audit) {
      const box = $('desktop-audit-box');
      const tkBtn = $('btn-takeover-desktop');
      if (doc.desktop_audit.exposed_count > 0) {
        box.textContent = 'Desktop 检测告警：' + doc.desktop_audit.summary;
        box.classList.remove('hidden');
        if (tkBtn) tkBtn.classList.remove('hidden');
      } else {
        box.classList.add('hidden');
        if (tkBtn) tkBtn.classList.add('hidden');
      }
    }

    $('doctor-checks').innerHTML = (doc.checks || []).map((c) => `
      <div class="check-row">
        <span class="status-tag ${esc(c.status)}">${esc(c.status)}</span>
        <div>
          <div class="check-title">${esc(c.title)}</div>
          <div class="check-detail">${esc(c.detail)}</div>
          ${c.status !== 'pass' && c.fix_hint ? `<div class="check-fix">建议：${esc(c.fix_hint)}</div>` : ''}
        </div>
      </div>
    `).join('');
  }

  function renderBrowserFingerprint() {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || 'unknown';
    const lang = navigator.language || 'unknown';
    const langs = (navigator.languages || []).join(', ');
    const offset = new Date().getTimezoneOffset();
    const isCNTZ = /Shanghai|Chongqing|Urumqi|Hong_Kong|Taipei/i.test(tz) || offset === -480;
    const isCNLang = /^zh/i.test(lang);
    const items = [
      { k: '当前浏览器时区 (Intl)', v: tz, ok: !isCNTZ },
      { k: 'UTC 偏移 (分钟)', v: `${offset} (UTC${offset <= 0 ? '+' : '-'}${Math.abs(offset / 60)})`, ok: !isCNTZ },
      { k: '浏览器主语言', v: lang, ok: !isCNLang },
      { k: 'Accept-Languages', v: langs || '—', ok: !isCNLang },
      { k: '隔离启动建议', v: isCNTZ || isCNLang ? '请点“打开隔离登录浏览器”完成 OAuth' : '当前浏览器指纹与英文区一致', ok: !(isCNTZ || isCNLang) },
      { k: '宿主机零污染', v: '本地运行，零外部指纹上报', ok: true },
    ];
    $('browser-fp-grid').innerHTML = items.map((it) => `
      <div class="fp-item ${it.ok ? 'ok' : 'warn'}">
        <div class="fp-k">${esc(it.k)}</div>
        <div class="fp-v">${esc(it.v)}</div>
      </div>
    `).join('');
  }

  function renderProfiles(data) {
    const list = data.profiles || [];
    $('profile-list').innerHTML = list.map((p) => `
      <div class="profile-card ${p.active ? 'active' : ''}">
        <div>
          <div><strong>${esc(p.name)}</strong> ${p.active ? '<span class="status-tag pass">ACTIVE</span>' : ''} ${p.note ? `<span class="muted">· ${esc(p.note)}</span>` : ''}</div>
          <div class="profile-meta">UUID: ${esc(p.fingerprint?.device_uuid || '—')} · CLI登录:${p.has_cli_creds ? '有' : '空'} · Desktop数据:${p.has_desktop_data ? '有' : '空'}</div>
        </div>
        <div class="btn-row">
          ${!p.active ? `<button class="btn btn-secondary btn-sm" data-use="${esc(p.name)}">设为默认</button>` : ''}
          <button class="btn btn-ghost btn-sm" data-clean-dry="${esc(p.name)}">盘点清理</button>
          <button class="btn btn-danger btn-sm" data-clean-real="${esc(p.name)}">清理指纹</button>
        </div>
      </div>
    `).join('');
  }

  function renderRules(rules) {
    if (!rules) return;
    $('toggle-shared-rules').checked = !!rules.enabled;
    const rows = [
      ['共享目录', rules.shared_dir || '—'],
      ['同步项', (rules.items || []).join(', ')],
      ...Object.entries(rules.profiles || {}).map(([k, v]) => [`档案 ${k}`, v]),
    ];
    $('rules-status-box').innerHTML = rows.map(([k, v]) => `
      <div class="kv-row"><span>${esc(k)}</span><code>${esc(v)}</code></div>
    `).join('');
  }

  function renderLeaks(data) {
    const sockets = data.host_direct_sockets || [];
    const banner = $('host-sockets-banner');
    if (sockets.length === 0) {
      banner.className = 'host-sockets-banner';
      banner.textContent = '宿主机 lsof 实时扫描正常：未发现任何绕过代理直连 Anthropic CIDR 的活跃 TCP/UDP 连接。';
    } else {
      banner.className = 'host-sockets-banner danger';
      banner.textContent = `警告：发现 ${sockets.length} 个宿主机直连 Anthropic Socket：` + sockets.map((x) => `${x.process}(${x.pid})->${x.dest_ip}:${x.dest_port}`).join(', ');
    }
    const events = data.events || [];
    $('leaks-tbody').innerHTML = events.length ? events.map((ev) => `
      <tr>
        <td class="mono">${esc(fmtTime(ev.timestamp))}</td>
        <td><span class="status-tag ${ev.kind.includes('failclosed') ? 'fail' : ev.kind.includes('split') ? 'warn' : 'pass'}">${esc(ev.kind)}</span></td>
        <td class="mono">${esc(ev.host)}${ev.port ? ':' + esc(ev.port) : ''}</td>
        <td class="mono">${esc(ev.action)}</td>
        <td>${esc(ev.reason)}</td>
      </tr>
    `).join('') : '<tr><td colspan="5" class="muted">暂无泄漏或策略拦截记录</td></tr>';
  }

  function renderCaptures(data) {
    $('toggle-capture').checked = !!data.enabled;
    captureMap.clear();
    const rows = data.records || [];
    rows.forEach((r) => captureMap.set(r.id, r));
    $('capture-tbody').innerHTML = rows.length ? rows.map((r) => `
      <tr class="clickable" data-cap-id="${esc(r.id)}">
        <td class="mono">${esc(fmtTime(r.timestamp))}</td>
        <td class="mono">${esc(r.protocol)}</td>
        <td class="mono">${esc(r.host)}${r.path ? esc(r.path) : ''}</td>
        <td>${esc(r.category)}</td>
        <td class="mono">${esc(r.action)}</td>
        <td class="mono">${esc(r.status_code || '—')}</td>
        <td class="mono">${esc(r.egress_ip || '—')}</td>
        <td class="mono">${esc(r.duration_ms)}ms</td>
      </tr>
    `).join('') : '<tr><td colspan="8" class="muted">暂无抓包记录（开启右上角开关即可开始脱敏记录）</td></tr>';
  }

  function renderVMProxy(vp) {
    if (!vp) return;
    const rows = [
      ['护盾状态', vp.active_pf_note],
      ['pf 规则语法校验 (pfctl -n)', vp.syntax_valid ? '通过 (' + vp.syntax_detail + ')' : '失败: ' + vp.syntax_detail],
      ['Cowork VM 代理接入', vp.config_enabled ? '已开启 (' + (vp.subnets || []).join(', ') + ')' : '未开启'],
      ['本机 Cowork 网桥检测', vp.cowork_bridge_active ? '活跃 (' + vp.cowork_bridge_ip + ')' : '未检测到活跃 Cowork VM'],
      ['规则文件路径', vp.rendered_anchor_path || '—'],
    ];
    $('vmproxy-box').innerHTML = rows.map(([k, v]) => `
      <div class="kv-row"><span>${esc(k)}</span><code>${esc(v)}</code></div>
    `).join('');
    if (vp.install_script_path) {
      $('pf-install-cmd').textContent = `sudo bash "${vp.install_script_path}"`;
    }
  }

  function populateSettings(cfg) {
    if (!cfg || populateSettings._dirty) return;
    $('cfg-upstream-mode').value = cfg.upstream.mode || 'local-proxy';
    $('cfg-upstream-addr').value = cfg.upstream.address || '';
    $('cfg-expected-ip').value = cfg.egress.expected_ip || '';
    $('cfg-allowed-countries').value = (cfg.egress.allowed_countries || []).join(',');
    $('cfg-language').value = cfg.identity.language || 'en-US';
    $('cfg-timezone').value = cfg.identity.timezone || '';
    $('cfg-fail-closed').checked = !!cfg.egress.fail_closed;
    $('cfg-sync-tz').checked = !!cfg.identity.sync_timezone;
    $('cfg-telemetry-shield').checked = !!cfg.shield.telemetry_shield;
    $('cfg-strict-split').checked = !!cfg.egress.strict_domain_isolation;
    $('cfg-skip-sse').checked = !!cfg.egress.skip_probe_during_sse;
    $('cfg-strip-host-env').checked = !!cfg.identity.strip_host_anthropic_env;
    $('cfg-corp-direct').checked = !!cfg.shield.allow_corporate_direct;
    $('cfg-vm-proxy').checked = !!cfg.listen.vm_proxy_enabled;
    $('cfg-auto-peace').checked = !!cfg.identity.auto_peace_desktop;
    $('cfg-cred-hint').textContent = cfg.upstream.has_credentials ? '当前上游已保存加密凭据（界面不回显）' : '当前上游未设置账密';
  }

  async function refreshAll() {
    try {
      const s = await api('/api/status');
      renderStatus(s);
    } catch (e) {
      toast('连接守护进程失败: ' + e.message);
    }
  }

  async function loadDoctor() {
    try {
      const doc = await api('/api/doctor');
      renderDoctor(doc);
    } catch (e) {
      toast('体检失败: ' + e.message);
    }
  }

  async function loadProfiles() {
    const data = await api('/api/profiles');
    renderProfiles(data);
  }

  async function loadLeaks() {
    const data = await api('/api/leaks?since=24h&limit=200');
    renderLeaks(data);
  }

  async function loadCaptures() {
    const data = await api('/api/capture?limit=200');
    renderCaptures(data);
  }

  // Tab navigation
  document.querySelectorAll('.nav-item').forEach((btn) => {
    btn.addEventListener('click', () => {
      document.querySelectorAll('.nav-item').forEach((b) => b.classList.remove('active'));
      document.querySelectorAll('.tab-panel').forEach((p) => p.classList.remove('active'));
      btn.classList.add('active');
      const tab = btn.dataset.tab;
      $('tab-' + tab).classList.add('active');
      if (tab === 'profiles') loadProfiles();
      if (tab === 'leaks') loadLeaks();
      if (tab === 'capture') loadCaptures();
    });
  });

  // Top action buttons
  $('btn-recheck').addEventListener('click', async () => {
    toast('正在重新探测静态 IP 出口…');
    try {
      await api('/api/check', { method: 'POST' });
      await refreshAll();
      await loadDoctor();
      toast('出口探测已刷新');
    } catch (e) { toast(e.message); }
  });

  $('btn-lock-toggle').addEventListener('click', async () => {
    const currentlyLocked = state && state.gate && !state.gate.open;
    try {
      await api('/api/lock', {
        method: 'POST',
        body: JSON.stringify({ locked: !currentlyLocked, reason: currentlyLocked ? '' : '用户在控制台手动触发紧急熔断' }),
      });
      await refreshAll();
      toast(currentlyLocked ? '已解除手动熔断并重检出口' : '已触发紧急熔断并切断受保护连接');
    } catch (e) { toast(e.message); }
  });

  $('btn-launch-desktop').addEventListener('click', async () => {
    try {
      const r = await api('/api/launch/desktop', { method: 'POST', body: '{}' });
      toast(`已启动隔离版 Claude Desktop (档案 ${r.profile}, PID ${r.pid})`);
    } catch (e) { toast('启动被拦截: ' + e.message); }
  });

  $('btn-launch-browser').addEventListener('click', async () => {
    try {
      const r = await api('/api/launch/browser', { method: 'POST', body: JSON.stringify({ url: 'https://claude.ai/login' }) });
      toast(`已打开隔离登录浏览器 (${r.browser})`);
    } catch (e) { toast('启动失败: ' + e.message); }
  });

  $('btn-copy-cli').addEventListener('click', async () => {
    const cmd = 'peaceclaude cli';
    try { await navigator.clipboard.writeText(cmd); toast('已复制命令: ' + cmd); }
    catch { toast('请在终端运行: ' + cmd); }
  });

  $('btn-run-doctor').addEventListener('click', async () => {
    toast('正在执行 11 项关键隔离与安全体检…');
    await loadDoctor();
    toast('体检完成');
  });

  $('btn-pause-recheck').addEventListener('click', async () => {
    toast('已暂停外网并切断活跃连接，正在执行全量 11 项重检…');
    try {
      const res = await api('/api/pause-and-recheck', { method: 'POST', body: '{}' });
      if (res.doctor) renderDoctor(res.doctor);
      await refreshAll();
      toast(`重检完成：关键检查 ${res.doctor?.critical_passed}/${res.doctor?.critical_total} 通过`);
    } catch (e) { toast('重检失败: ' + e.message); }
  });

  $('btn-copy-report').addEventListener('click', async () => {
    if (!lastDoctor) { toast('请先运行一次体检'); return; }
    const lines = [
      `# PeaceClaude 网络与隔离检查报告 (${lastDoctor.critical_passed}/${lastDoctor.critical_total} 通过 · 评分 ${lastDoctor.score}/100)`,
      `时间: ${lastDoctor.generated_at}`,
      `宿主机时区: ${lastDoctor.host_timezone} -> 隔离环境: ${lastDoctor.peace_timezone} / ${lastDoctor.peace_language}`,
      '',
      ...(lastDoctor.checks || []).map((c) => `- [${c.status.toUpperCase()}] ${c.title}: ${c.detail}`),
    ];
    try { await navigator.clipboard.writeText(lines.join('\n')); toast('已复制脱敏检查报告到剪贴板'); }
    catch { toast('复制失败，请手动复制页面内容'); }
  });

  $('btn-inspect-desktop').addEventListener('click', async () => {
    try {
      const rep = await api('/api/inspect/desktop');
      const box = $('desktop-audit-box');
      const tkBtn = $('btn-takeover-desktop');
      const extra = (rep.processes || []).map((p) =>
        `PID ${p.pid}: ${p.isolated ? '已隔离' : '未隔离(缺 ' + (p.flags_missing || []).join(',') + ')'}`
      ).join(' | ');
      box.textContent = rep.summary + (extra ? ' — ' + extra : '');
      box.classList.remove('hidden');
      if (tkBtn) tkBtn.classList.toggle('hidden', !(rep.exposed_count > 0));
      toast('已完成 Claude Desktop 运行实例隔离检测');
    } catch (e) { toast(e.message); }
  });

  $('btn-takeover-desktop').addEventListener('click', async () => {
    toast('正在关闭未隔离的原生 Claude.app 并以安全隔离模式重新拉起…');
    try {
      const r = await api('/api/desktop/takeover', { method: 'POST', body: '{}' });
      toast(`接管成功：已关闭 ${r.terminated} 个原生实例，新隔离 Desktop PID=${r.relaunched_pid}`);
      await loadDoctor();
      await refreshAll();
    } catch (e) { toast('接管拦截/失败: ' + e.message); }
  });

  $('btn-inspect-browser').addEventListener('click', () => {
    renderBrowserFingerprint();
    $('browser-fp-grid').scrollIntoView({ behavior: 'smooth', block: 'center' });
    toast('已刷新浏览器本地指纹自检（建议在“隔离登录浏览器”内打开本页复核）');
  });

  $('form-create-profile').addEventListener('submit', async (ev) => {
    ev.preventDefault();
    const name = $('new-profile-name').value.trim();
    const note = $('new-profile-note').value.trim();
    if (!name) return;
    try {
      await api('/api/profiles/create', { method: 'POST', body: JSON.stringify({ name, note }) });
      $('new-profile-name').value = '';
      $('new-profile-note').value = '';
      await loadProfiles();
      await refreshAll();
      toast('已创建隔离档案: ' + name);
    } catch (e) { toast(e.message); }
  });

  $('profile-list').addEventListener('click', async (ev) => {
    const useBtn = ev.target.closest('[data-use]');
    const dryBtn = ev.target.closest('[data-clean-dry]');
    const realBtn = ev.target.closest('[data-clean-real]');
    try {
      if (useBtn) {
        const name = useBtn.dataset.use;
        await api('/api/profiles/use', { method: 'POST', body: JSON.stringify({ name }) });
        await loadProfiles();
        await refreshAll();
        toast('已切换默认档案为: ' + name);
      } else if (dryBtn) {
        const name = dryBtn.dataset.cleanDry;
        const rep = await api('/api/profiles/clean', { method: 'POST', body: JSON.stringify({ name, dry_run: true }) });
        $('clean-output').textContent = JSON.stringify(rep, null, 2);
        toast(`已完成 ${name} 清理盘点 (Dry-Run)`);
      } else if (realBtn) {
        const name = realBtn.dataset.cleanReal;
        const rep = await api('/api/profiles/clean', { method: 'POST', body: JSON.stringify({ name, dry_run: false }) });
        $('clean-output').textContent = JSON.stringify(rep, null, 2);
        await loadProfiles();
        toast(`已完成 ${name} 指纹清理并备份`);
      }
    } catch (e) { toast(e.message); }
  });

  $('toggle-shared-rules').addEventListener('change', async (ev) => {
    try {
      const st = await api('/api/rules/toggle', { method: 'POST', body: JSON.stringify({ enabled: ev.target.checked }) });
      renderRules(st);
      toast(ev.target.checked ? '已开启跨档案全局规则共享' : '已关闭全局规则共享（各档案已保留本地副本）');
    } catch (e) { toast(e.message); }
  });

  $('btn-refresh-leaks').addEventListener('click', loadLeaks);
  $('btn-clear-leaks').addEventListener('click', async () => {
    await api('/api/leaks/clear', { method: 'POST' });
    await loadLeaks();
    await refreshAll();
    toast('已清空泄漏审计记录');
  });

  $('toggle-capture').addEventListener('change', async (ev) => {
    await api('/api/capture/toggle', { method: 'POST', body: JSON.stringify({ enabled: ev.target.checked }) });
    await refreshAll();
    toast(ev.target.checked ? '已开启脱敏抓包' : '已关闭抓包');
  });
  $('btn-refresh-capture').addEventListener('click', loadCaptures);
  $('btn-clear-capture').addEventListener('click', async () => {
    await api('/api/capture/clear', { method: 'POST' });
    await loadCaptures();
    await refreshAll();
    toast('已清空抓包记录');
  });

  $('capture-tbody').addEventListener('click', (ev) => {
    const tr = ev.target.closest('tr[data-cap-id]');
    if (!tr) return;
    const rec = captureMap.get(tr.dataset.capId);
    if (!rec) return;
    $('capture-detail-json').textContent = JSON.stringify(rec, null, 2);
    $('capture-detail').classList.remove('hidden');
  });
  $('btn-close-detail').addEventListener('click', () => $('capture-detail').classList.add('hidden'));

  $('btn-run-diagnose').addEventListener('click', async () => {
    $('diagnose-result').innerHTML = '<p class="muted">正在实时探测三大端点并扫描日志…</p>';
    try {
      const d = await api('/api/diagnose');
      const clsTag = d.classification === 'healthy' ? 'pass' : d.classification === 'network' ? 'warn' : 'fail';
      $('diagnose-result').innerHTML = `
        <div><span class="status-tag ${clsTag}">${esc(d.classification)}</span> <strong>${esc(d.summary)}</strong></div>
        <div class="kv-row"><span>推荐动作</span><span>${esc(d.recommendation)}</span></div>
        <div class="small-label">三大核心端点连通性</div>
        ${(d.endpoints || []).map((ep) => `
          <div class="kv-row">
            <code>${esc(ep.url)}</code>
            <code>${ep.error ? 'ERR: ' + esc(ep.error) : 'HTTP ' + ep.status_code + ' · ' + ep.latency_ms + 'ms'}</code>
          </div>
        `).join('')}
        ${(d.evidence || []).length ? `<div class="small-label">真实日志证据</div><pre class="code-box">${esc(d.evidence.join('\n'))}</pre>` : ''}
      `;
    } catch (e) {
      $('diagnose-result').innerHTML = `<p class="accent-red">分诊失败: ${esc(e.message)}</p>`;
    }
  });

  $('form-settings').addEventListener('input', () => { populateSettings._dirty = true; });
  $('form-settings').addEventListener('submit', async (ev) => {
    ev.preventDefault();
    const payload = {
      upstream_mode: $('cfg-upstream-mode').value,
      upstream_address: $('cfg-upstream-addr').value.trim(),
      upstream_username: $('cfg-upstream-user').value,
      upstream_password: $('cfg-upstream-pass').value,
      expected_ip: $('cfg-expected-ip').value.trim(),
      allowed_countries: $('cfg-allowed-countries').value.split(',').map((s) => s.trim()).filter(Boolean),
      language: $('cfg-language').value.trim(),
      timezone: $('cfg-timezone').value.trim(),
      fail_closed: $('cfg-fail-closed').checked,
      sync_timezone: $('cfg-sync-tz').checked,
      telemetry_shield: $('cfg-telemetry-shield').checked,
      strict_domain_isolation: $('cfg-strict-split').checked,
      skip_probe_during_sse: $('cfg-skip-sse').checked,
      strip_host_anthropic_env: $('cfg-strip-host-env').checked,
      allow_corporate_direct: $('cfg-corp-direct').checked,
      vm_proxy_enabled: $('cfg-vm-proxy').checked,
      auto_peace_desktop: $('cfg-auto-peace').checked,
    };
    try {
      await api('/api/config/update', { method: 'POST', body: JSON.stringify(payload) });
      populateSettings._dirty = false;
      $('cfg-upstream-pass').value = '';
      await refreshAll();
      await loadDoctor();
      toast('配置已保存并完成热重载与出口复检');
    } catch (e) { toast('保存失败: ' + e.message); }
  });

  $('theme-toggle').addEventListener('click', () => {
    const html = document.documentElement;
    const next = html.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
    html.setAttribute('data-theme', next);
    localStorage.setItem('peaceclaude-theme', next);
  });
  const savedTheme = localStorage.getItem('peaceclaude-theme');
  if (savedTheme === 'light' || savedTheme === 'dark') {
    document.documentElement.setAttribute('data-theme', savedTheme);
  }

  renderBrowserFingerprint();
  refreshAll().then(loadDoctor);
  setInterval(refreshAll, 8000);
})();
