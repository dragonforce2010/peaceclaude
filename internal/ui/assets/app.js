(() => {
  'use strict';

  const $ = (id) => document.getElementById(id);
  const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c]));

  // Unified 16px stroke-1.5 SVG icon library (zero emoji)
  const SVG_PATHS = {
    users: '<path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/>',
    shield: '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><path d="m9 12 2 2 4-4"/>',
    alert: '<path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>',
    activity: '<polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/>',
    sliders: '<line x1="4" y1="21" x2="4" y2="14"/><line x1="4" y1="10" x2="4" y2="3"/><line x1="12" y1="21" x2="12" y2="12"/><line x1="12" y1="8" x2="12" y2="3"/><line x1="20" y1="21" x2="20" y2="16"/><line x1="20" y1="12" x2="20" y2="3"/><line x1="2" y1="14" x2="6" y2="14"/><line x1="10" y1="8" x2="14" y2="8"/><line x1="18" y1="16" x2="22" y2="16"/>',
    smartphone: '<rect width="14" height="20" x="5" y="2" rx="2" ry="2"/><path d="M12 18h.01"/>',
    palette: '<circle cx="13.5" cy="6.5" r=".5" fill="currentColor"/><circle cx="17.5" cy="10.5" r=".5" fill="currentColor"/><circle cx="8.5" cy="7.5" r=".5" fill="currentColor"/><circle cx="6.5" cy="12.5" r=".5" fill="currentColor"/><path d="M12 2C6.5 2 2 6.5 2 12s4.5 10 10 10c.926 0 1.648-.746 1.648-1.688 0-.437-.18-.835-.437-1.125-.29-.289-.438-.652-.438-1.125a1.64 1.64 0 0 1 1.668-1.668h1.996c3.051 0 5.555-2.503 5.555-5.554C21.965 6.012 17.461 2 12 2z"/>',
    database: '<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v14a9 3 0 0 0 18 0V5"/><path d="M3 12a9 3 0 0 0 18 0"/>',
    lock: '<rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>',
    power: '<path d="M12 2v10"/><path d="M18.4 6.6a9 9 0 1 1-12.77.04"/>',
    'chevrons-up-down': '<path d="m7 15 5 5 5-5"/><path d="m7 9 5-5 5 5"/>',
    sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/>',
    moon: '<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>',
    monitor: '<rect width="20" height="14" x="2" y="3" rx="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/>',
    upload: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/>',
    download: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/>',
    'check-circle': '<circle cx="12" cy="12" r="10"/><path d="m9 12 2 2 4-4"/>',
    'alert-triangle': '<path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z"/><path d="M12 9v4"/><path d="M12 17h.01"/>',
    'x-circle': '<circle cx="12" cy="12" r="10"/><path d="m15 9-6 6"/><path d="m9 9 6 6"/>',
    'qr-code': '<rect width="5" height="5" x="3" y="3" rx="1"/><rect width="5" height="5" x="16" y="3" rx="1"/><rect width="5" height="5" x="3" y="16" rx="1"/><path d="M21 16h-3a2 2 0 0 0-2 2v3"/><path d="M21 21v.01"/><path d="M12 7v3a2 2 0 0 1-2 2H7"/><path d="M3 12h.01"/><path d="M12 3h.01"/><path d="M12 16v.01"/><path d="M16 12h1"/><path d="M21 12v.01"/><path d="M12 21v-1"/>',
    terminal: '<polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/>',
  };

  function hydrateIcons(root = document) {
    root.querySelectorAll('.nav-ico[data-icon]').forEach((el) => {
      const p = SVG_PATHS[el.dataset.icon];
      if (!p) return;
      el.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${p}</svg>`;
    });
  }

  let statusData = null;
  let accountsData = null;
  let remoteData = null;
  let currentTab = 'accounts';
  let currentNetSub = 'proxy';
  let currentDetailName = null;
  let pacEnabled = false;
  let pendingConfirm = {};
  let editingSide = 'dark';
  let editingSidePinned = false;
  let pairCountdownTimer = null;

  // Original PeaceClaude Color Palettes + Backward-compatible Alias Maps
  const LIGHT_PRESETS = {
    paper: { name: '工程师冷白', desc: '中性工程图纸灰底配低饱和陶土红焦点', accent: '#c85a32', bg: '#fafafa', text: '#171717' },
    linen: { name: '素麻暖纸', desc: '微暖亚麻底色减缓长时间审阅视觉疲劳', accent: '#b45309', bg: '#f7f4ee', text: '#1f1b16' },
    frost: { name: '雾银仪表', desc: '微冷灰蓝工业表面配澄明钴蓝信号', accent: '#005fb8', bg: '#f5f7fa', text: '#111827' },
    stone: { name: '浅砾灰白', desc: '哑光矿石浅灰与苍松静谧绿强调', accent: '#0f766e', bg: '#f6f8f7', text: '#14201c' },
    chalk: { name: '高对比粉白', desc: '纯净高反差白底配深墨碳黑读数', accent: '#2563eb', bg: '#ffffff', text: '#111111' },
    porcelain: { name: '青瓷微雾', desc: '淡雅天青釉底配沉稳石青点缀', accent: '#0e7490', bg: '#f3f7f8', text: '#132125' },
  };
  const DARK_PRESETS = {
    obsidian: { name: '墨曜石墨', desc: '默认拉丝石墨工作台与低饱和陶土信号橙', accent: '#e07a55', bg: '#141415', text: '#ededed' },
    midnight: { name: '深宵夜航', desc: '深邃夜航墨蓝面板配冰青网络读数', accent: '#38bdf8', bg: '#0b0d14', text: '#eef0f5' },
    slate: { name: '玄岩冷灰', desc: '中性炭黑表面与高可读冷水蓝按钮', accent: '#3b82f6', bg: '#18181b', text: '#f4f4f5' },
    amber: { name: '夜班琥珀', desc: '深蓝褐夜班台配温和琥珀指针高亮', accent: '#e6b450', bg: '#12151c', text: '#cbc7be' },
    moss: { name: '深松静谧', desc: '幽暗苔藓碳底与松石绿状态灯组合', accent: '#34d399', bg: '#0f1412', text: '#e6edea' },
    plum: { name: '曜紫极夜', desc: '深邃紫曜暗底配柔和绛紫亮标', accent: '#a78bfa', bg: '#13111c', text: '#ede9fe' },
  };
  const ANIMAL_AVATARS = [
    { id: 'tiger', label: '眼镜虎' },
    { id: 'lion', label: '金狮' },
    { id: 'eagle', label: '苍鹰' },
    { id: 'fox', label: '赤狐' },
    { id: 'panda', label: '熊猫' },
    { id: 'shiba', label: '柴犬' },
    { id: 'owl', label: '枭鹫' },
    { id: 'koala', label: '考拉' },
    { id: 'wolf', label: '银狼' },
  ];
  const ANIMAL_AVATAR_MAP = Object.fromEntries(ANIMAL_AVATARS.map((x) => [x.id, x]));
  function resolveAvatarID(a) {
    if (a && a.avatar_id && ANIMAL_AVATAR_MAP[a.avatar_id]) return a.avatar_id;
    const key = String((a && a.name) || 'default');
    if (key === 'default') return 'tiger';
    let h = 0;
    for (let i = 0; i < key.length; i += 1) h = (h * 31 + key.charCodeAt(i)) >>> 0;
    return ANIMAL_AVATARS[h % ANIMAL_AVATARS.length].id;
  }
  function avatarMediaHTML(a) {
    const id = resolveAvatarID(a);
    const label = ANIMAL_AVATAR_MAP[id]?.label || '头像';
    return `<img class="avatar-img" src="/avatars/${esc(id)}.png" alt="${esc(label)}" />`;
  }
  const LEGACY_LIGHT_MAP = {
    codex: 'paper', graphite: 'paper', warm: 'linen', solarized: 'linen', 'vscode-modern': 'frost', evergreen: 'stone',
  };
  const LEGACY_DARK_MAP = {
    graphite: 'obsidian', ayu: 'amber', codex: 'slate', ink: 'midnight', solarized: 'amber', 'vscode-modern': 'slate',
  };

  let appearanceState = {
    theme_mode: 'dark',
    preset_light: 'paper',
    preset_dark: 'obsidian',
    custom_light: null,
    custom_dark: null,
    saved_themes: [],
    active_saved_theme: '',
    ui_font: '',
    code_font: '',
    ui_font_size: 14,
    code_font_size: 12,
  };

  function toast(msg, kind = 'ok') {
    const el = $('toast');
    el.textContent = msg;
    el.className = `toast ${kind || 'ok'}`;
    clearTimeout(toast._t);
    toast._t = setTimeout(() => el.classList.add('hidden'), 4500);
  }

  async function withBtnBusy(btn, busyText, fn) {
    if (!btn || btn.disabled) return;
    const orig = btn.textContent;
    btn.disabled = true;
    if (busyText) btn.textContent = busyText;
    try {
      return await fn();
    } catch (e) {
      toast(e?.message || '操作失败', 'err');
      if (busyText) btn.textContent = orig;
    } finally {
      btn.disabled = false;
      // Preserve updated state label (e.g. from renderStatus / renderPACStatus) if fn changed it.
      if (busyText && btn.textContent === busyText) {
        btn.textContent = orig;
      }
    }
  }

  async function api(url, opts = {}) {
    const res = await fetch(url, {
      headers: { 'Content-Type': 'application/json' },
      ...opts,
    });
    const text = await res.text();
    let data = {};
    try { data = text ? JSON.parse(text) : {}; } catch { data = { raw: text }; }
    if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
    return data;
  }

  const fmtTime = (iso) => {
    if (!iso) return '—';
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? String(iso) : d.toLocaleTimeString('zh-CN', { hour12: false });
  };
  const fmtDate = (iso) => {
    if (!iso) return '—';
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? String(iso) : d.toISOString().slice(0, 10);
  };
  const fmtBytes = (n) => {
    if (!n) return '0 B';
    if (n < 1024) return n + ' B';
    if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
    return (n / (1024 * 1024)).toFixed(1) + ' MB';
  };

  // --- Color Math Helpers ---
  function hexToRgb(hex) {
    const m = /^#([0-9a-f]{6})$/i.exec((hex || '').trim());
    if (!m) return null;
    const n = parseInt(m[1], 16);
    return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
  }
  function mixHex(h1, h2, ratio) {
    const a = hexToRgb(h1), b = hexToRgb(h2);
    if (!a || !b) return h1;
    const c = a.map((v, i) => Math.round(v * (1 - ratio) + b[i] * ratio));
    return '#' + c.map((v) => v.toString(16).padStart(2, '0')).join('');
  }
  function luminance(hex) {
    const rgb = hexToRgb(hex);
    if (!rgb) return 0;
    const [r, g, b] = rgb.map((v) => {
      const s = v / 255;
      return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
    });
    return 0.2126 * r + 0.7152 * g + 0.0722 * b;
  }
  function rgbaStr(hex, alpha) {
    const rgb = hexToRgb(hex);
    return rgb ? `rgba(${rgb[0]}, ${rgb[1]}, ${rgb[2]}, ${alpha})` : `rgba(255,255,255,${alpha})`;
  }

  function normalizePresetKey(side, key) {
    if (side === 'light') {
      const mapped = LEGACY_LIGHT_MAP[key] || key;
      return LIGHT_PRESETS[mapped] ? mapped : 'paper';
    }
    const mapped = LEGACY_DARK_MAP[key] || key;
    return DARK_PRESETS[mapped] ? mapped : 'obsidian';
  }

  function resolveEffectiveThemeColors(side) {
    const isLight = side === 'light';
    const presets = isLight ? LIGHT_PRESETS : DARK_PRESETS;
    const presetKey = normalizePresetKey(side, isLight ? appearanceState.preset_light : appearanceState.preset_dark);
    const base = presets[presetKey] || (isLight ? LIGHT_PRESETS.paper : DARK_PRESETS.obsidian);
    const ov = isLight ? appearanceState.custom_light : appearanceState.custom_dark;
    return {
      presetKey,
      name: base.name,
      desc: base.desc,
      accent: (ov && ov.accent) || base.accent,
      bg: (ov && ov.bg) || base.bg,
      text: (ov && ov.text) || base.text,
      isCustom: !!(ov && (ov.accent || ov.bg || ov.text)),
    };
  }

  function resolvedMode() {
    if (appearanceState.theme_mode === 'system') {
      return window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
    }
    return appearanceState.theme_mode === 'light' ? 'light' : 'dark';
  }

  function applyAppearance(app) {
    if (app) appearanceState = { ...appearanceState, ...app };
    const mode = resolvedMode();
    if (!editingSidePinned) editingSide = mode;
    const isLight = mode === 'light';
    document.documentElement.dataset.theme = mode;

    const c = resolveEffectiveThemeColors(mode);
    const root = document.documentElement.style;
    root.setProperty('--bg', c.bg);
    root.setProperty('--text', c.text);
    root.setProperty('--accent', c.accent);
    root.setProperty('--accent-soft', rgbaStr(c.accent, isLight ? 0.11 : 0.14));
    root.setProperty('--accent-ink', luminance(c.accent) > 0.45 ? '#111111' : '#ffffff');

    if (isLight) {
      const card = mixHex(c.bg, '#ffffff', 0.65);
      const deep = mixHex(c.bg, '#000000', 0.045);
      const subtle = mixHex(c.bg, '#000000', 0.06);
      root.setProperty('--surface-deep', deep);
      root.setProperty('--bg-elev', deep);
      root.setProperty('--bg-card', card);
      root.setProperty('--bg-subtle', subtle);
      root.setProperty('--text-sec', mixHex(c.text, c.bg, 0.28));
      root.setProperty('--muted', mixHex(c.text, c.bg, 0.44));
      root.setProperty('--border', rgbaStr(c.text, 0.08));
      root.setProperty('--border-strong', rgbaStr(c.text, 0.16));
    } else {
      const deep = mixHex(c.bg, '#000000', 0.25);
      const card = mixHex(c.bg, '#ffffff', 0.035);
      const subtle = mixHex(c.bg, '#ffffff', 0.075);
      root.setProperty('--surface-deep', deep);
      root.setProperty('--bg-elev', c.bg);
      root.setProperty('--bg-card', card);
      root.setProperty('--bg-subtle', subtle);
      root.setProperty('--text-sec', mixHex(c.text, c.bg, 0.28));
      root.setProperty('--muted', mixHex(c.text, c.bg, 0.45));
      root.setProperty('--border', rgbaStr(c.text, 0.08));
      root.setProperty('--border-strong', rgbaStr(c.text, 0.16));
    }

    root.setProperty('--ui-font', appearanceState.ui_font || '-apple-system, BlinkMacSystemFont, "SF Pro Text", "PingFang SC", "Helvetica Neue", sans-serif');
    root.setProperty('--code-font', appearanceState.code_font || 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace');
    root.setProperty('--ui-size', (appearanceState.ui_font_size || 14) + 'px');
    root.setProperty('--code-size', (appearanceState.code_font_size || 12) + 'px');

    try { localStorage.setItem('peaceclaude_appearance', JSON.stringify(appearanceState)); } catch {}
    renderAppearanceUI();
  }

  let saveAppTimer = null;
  function saveAppearanceDebounced() {
    applyAppearance();
    clearTimeout(saveAppTimer);
    saveAppTimer = setTimeout(async () => {
      try {
        await api('/api/appearance/update', { method: 'POST', body: JSON.stringify(appearanceState) });
      } catch {}
    }, 220);
  }

  function renderAppearanceUI() {
    $('app-theme-mode-seg')?.querySelectorAll('button[data-mode]').forEach((b) => {
      b.classList.toggle('active', b.dataset.mode === appearanceState.theme_mode);
    });
    $('quick-theme-seg')?.querySelectorAll('button[data-qmode]').forEach((b) => {
      b.classList.toggle('active', b.dataset.qmode === appearanceState.theme_mode);
    });
    $('edit-side-seg')?.querySelectorAll('button[data-eside]').forEach((b) => {
      b.classList.toggle('active', b.dataset.eside === editingSide);
    });

    const curSide = resolvedMode();
    const isEditingActive = editingSide === curSide;
    if ($('stage-active-hint')) {
      $('stage-active-hint').textContent = isEditingActive ? '当前正激活此配色' : '编辑非当前激活方案中';
    }
    if ($('inspector-side-desc')) {
      $('inspector-side-desc').textContent = editingSide === 'light'
        ? '点击色板立即写入浅色方案'
        : '点击色板立即写入深色方案';
    }

    const c = resolveEffectiveThemeColors(editingSide);
    const isLight = editingSide === 'light';
    const sideBg = isLight ? mixHex(c.bg, '#000000', 0.04) : mixHex(c.bg, '#000000', 0.25);
    const cardBg = isLight ? mixHex(c.bg, '#ffffff', 0.70) : mixHex(c.bg, '#ffffff', 0.04);
    const trackBg = isLight ? mixHex(c.bg, '#000000', 0.08) : mixHex(c.bg, '#ffffff', 0.08);
    const lineCol = rgbaStr(c.text, 0.10);

    // Paint Live Miniature PeaceClaude Instrument Canvas
    const stage = $('live-theme-stage');
    if (stage) {
      stage.style.background = c.bg;
      stage.style.color = c.text;
      stage.style.fontFamily = appearanceState.ui_font || 'var(--ui-font)';
      stage.style.fontSize = (appearanceState.ui_font_size || 14) + 'px';
      $('stage-mini-sidebar').style.background = sideBg;
      $('stage-mini-sidebar').style.borderRightColor = lineCol;
      $('stage-brand-dot').style.background = c.accent;
      $('stage-nav-active').style.background = cardBg;
      $('stage-mini-rail').style.borderBottomColor = lineCol;
      $('stage-mini-btn').style.background = c.text;
      $('stage-mini-btn').style.color = c.bg;
      $('stage-mini-card').style.background = cardBg;
      $('stage-mini-card').style.borderColor = lineCol;
      $('stage-mini-track').style.background = trackBg;
      $('stage-mini-fill').style.background = c.accent;

      const sem = $('stage-semantic-strip');
      if (sem) {
        sem.style.background = cardBg;
        sem.style.borderColor = lineCol;
        const spans = sem.querySelectorAll(':scope > span');
        if (spans[0]) spans[0].style.color = isLight ? '#16a34a' : '#4cc38a';
        if (spans[1]) spans[1].style.color = isLight ? '#b45309' : '#f5b849';
        if (spans[2]) spans[2].style.color = isLight ? '#dc2626' : '#f87171';
      }
      const codeEl = $('stage-code-sample');
      if (codeEl) {
        codeEl.style.background = sideBg;
        codeEl.style.borderColor = lineCol;
        codeEl.style.color = mixHex(c.text, c.bg, 0.25);
        codeEl.style.fontFamily = appearanceState.code_font || 'var(--code-font)';
        codeEl.style.fontSize = (appearanceState.code_font_size || 12) + 'px';
      }
    }

    // Swatch Grid for Active Editing Side
    const grid = $('preset-swatch-grid');
    if (grid) {
      const presets = isLight ? LIGHT_PRESETS : DARK_PRESETS;
      grid.innerHTML = Object.entries(presets).map(([key, p]) => `
        <button type="button" class="swatch-card ${!c.isCustom && c.presetKey === key ? 'active' : ''}" data-preset-key="${esc(key)}">
          <div class="swatch-top">
            <span class="swatch-name">${esc(p.name)}</span>
            <span class="swatch-dots">
              <span class="swatch-dot" style="background:${p.bg}"></span>
              <span class="swatch-dot" style="background:${p.text}"></span>
              <span class="swatch-dot" style="background:${p.accent}"></span>
            </span>
          </div>
          <span class="swatch-desc">${esc(p.desc)}</span>
        </button>`).join('');
    }

    // Color Inspector Inputs
    if ($('pick-side-accent')) $('pick-side-accent').value = c.accent;
    if ($('hex-side-accent') && document.activeElement !== $('hex-side-accent')) $('hex-side-accent').value = c.accent;
    if ($('pick-side-bg')) $('pick-side-bg').value = c.bg;
    if ($('hex-side-bg') && document.activeElement !== $('hex-side-bg')) $('hex-side-bg').value = c.bg;
    if ($('pick-side-text')) $('pick-side-text').value = c.text;
    if ($('hex-side-text') && document.activeElement !== $('hex-side-text')) $('hex-side-text').value = c.text;
    if ($('side-custom-label')) {
      $('side-custom-label').textContent = c.isCustom ? `已微调自定义色值 · ${c.name}` : `预设 · ${c.name}`;
    }

    if ($('select-ui-font')) $('select-ui-font').value = appearanceState.ui_font || '';
    if ($('select-code-font')) $('select-code-font').value = appearanceState.code_font || '';
    if ($('select-ui-size')) $('select-ui-size').value = String(appearanceState.ui_font_size || 14);
    if ($('select-code-size')) $('select-code-size').value = String(appearanceState.code_font_size || 12);
  }

  // --- Navigation & Network Subtabs ---
  function switchNetSub(sub) {
    currentNetSub = sub === 'policy' ? 'policy' : 'proxy';
    $('net-subtabs')?.querySelectorAll('button[data-net-sub]').forEach((b) => {
      b.classList.toggle('active', b.dataset.netSub === currentNetSub);
    });
    $('sub-net-proxy')?.classList.toggle('hidden', currentNetSub !== 'proxy');
    $('sub-net-policy')?.classList.toggle('hidden', currentNetSub !== 'policy');
  }

  function switchTab(tab) {
    if (tab === 'profiles') tab = 'accounts';
    if (tab === 'general') {
      tab = 'network';
      switchNetSub('policy');
    } else if (tab === 'settings') {
      tab = 'network';
      switchNetSub('proxy');
    }
    const valid = ['accounts', 'security', 'leaks', 'capture', 'remote', 'appearance', 'network', 'storage', 'cli'];
    if (!valid.includes(tab)) tab = 'accounts';
    currentTab = tab;

    document.querySelectorAll('.nav-item[data-tab]').forEach((btn) => {
      btn.classList.toggle('active', btn.dataset.tab === tab);
    });
    document.querySelectorAll('.tab-panel').forEach((p) => {
      p.classList.toggle('active', p.id === `tab-${tab}`);
    });
    history.replaceState(null, '', '#' + tab);

    if (tab === 'accounts') loadAccounts(false);
    if (tab === 'security') { loadDoctor(); checkBrowserFingerprint(); }
    if (tab === 'leaks') loadLeaks();
    if (tab === 'capture') loadCaptures();
    if (tab === 'remote') loadRemote();
    if (tab === 'network') loadNativeBrowsers();
    if (tab === 'appearance') renderAppearanceUI();
  }

  // --- Status, Bottom-Left Dock Tray & Lock Overlay ---
  function renderStatus(s) {
    statusData = s;
    const g = s.gate || {};
    const banner = $('gate-banner');
    banner.dataset.state = g.open ? 'open' : 'locked';
    $('gate-dot').className = 'status-dot ' + (g.open ? 'ok' : 'bad');
    $('gate-pill-text').textContent = g.open ? '静态出口放行' : '门禁已锁定';

    const ip = (g.last_good && g.last_good.egress_ip) || (g.last_probe && g.last_probe.egress_ip) || '—';
    const geo = g.last_good && g.last_good.country ? `${g.last_good.country} · ${g.last_good.city || ''}` : '—';
    const expected = s.config?.egress?.expected_ip;
    $('hero-ip').textContent = expected && ip !== expected ? `${ip} (!=${expected})` : ip;
    $('hero-geo').textContent = geo;
    $('hero-tz').textContent = `${g.effective_timezone || '—'} · ${g.effective_language || 'en-US'}`;
    $('hero-streams').textContent = String(g.active_streams ?? 0);
    if ($('hero-probe-meta')) {
      const lp = g.last_probe || {};
      if (lp.checked_at) {
        const lat = lp.latency_ms ? ` · ${lp.latency_ms}ms` : '';
        $('hero-probe-meta').textContent = `校验 ${fmtTime(lp.checked_at)}${lat}`;
      } else {
        $('hero-probe-meta').textContent = '待首次校验';
      }
    }

    const alertEl = $('gate-alert');
    if (!g.open && g.lock_reason) {
      alertEl.textContent = '已阻断 Claude 请求 · ' + g.lock_reason;
      alertEl.classList.remove('hidden');
    } else if ((s.direct_leaks_count || 0) > 0) {
      alertEl.textContent = `检测到 ${s.direct_leaks_count} 个宿主机直连 Anthropic Socket，请在「安全检测」或「泄漏审计」核对发起进程`;
      alertEl.classList.remove('hidden');
    } else {
      alertEl.classList.add('hidden');
    }

    const lockBtn = $('btn-lock-toggle');
    lockBtn.textContent = g.manual_lock ? '已熔断 · 点击恢复' : '紧急熔断';
    lockBtn.className = g.manual_lock ? 'btn btn-danger btn-sm' : 'btn btn-ghost btn-sm';

    $('version-badge').textContent = `${s.version || '0.1.0'} · PID ${s.pid}`;
    $('sb-status-dot').className = 'status-dot ' + (g.open ? 'ok' : 'bad');
    $('sb-gateway-status').textContent = g.open ? `已放行 · ${ip}` : '门禁已锁定';
    $('sb-gateway-ports').textContent = `:${s.config?.listen?.http_port || 18897}`;

    const hostUser = s.host_user || 'local';
    const activeAcct = (accountsData?.accounts || []).find((a) => a.active || a.name === s.active_profile);
    const activeAvatar = s.active_avatar_id || activeAcct?.avatar_id || '';
    const activeDisplay = s.active_display_name || activeAcct?.display_name || hostUser;
    $('sb-host-avatar').innerHTML = avatarMediaHTML({ name: s.active_profile || 'default', avatar_id: activeAvatar });
    $('sb-host-name').textContent = activeDisplay;
    $('sb-active-summary').textContent = `本地隔离 · @${s.active_profile || 'default'}`;

    if ($('nav-account-count')) $('nav-account-count').textContent = String(s.profiles_count || 1);

    const stats = s.stats || {};
    const fc = Number(stats.blocked_failclosed || 0);
    const tel = Number(stats.blocked_telemetry || 0);
    const dl = Number(s.direct_leaks_count || 0);
    $('kpi-proxied').textContent = String(stats.proxied_requests || 0);
    $('kpi-blocked-fc').textContent = String(fc);
    $('kpi-blocked-fc').className = 'kpi-value' + (fc > 0 ? ' accent-red' : '');
    $('kpi-blocked-tel').textContent = String(tel);
    $('kpi-blocked-tel').className = 'kpi-value' + (tel > 0 ? ' accent-amber' : '');
    $('kpi-direct-leaks').textContent = String(dl);
    $('kpi-direct-leaks').className = 'kpi-value' + (dl > 0 ? ' accent-red' : '');
    $('nav-leak-count').textContent = String(stats.leak_count || 0);
    $('nav-cap-count').textContent = String(stats.capture_count || 0);
    $('toggle-capture').checked = !!stats.capture_enabled;

    renderVMProxy(s.vm_proxy);
    renderRules(s.rules);
    renderPACStatus(s.browser_pac);
    renderServiceAutostart(s.service);
    renderUpstreamRoute(s.upstream_route);
    renderStorageInfo(s.storage);
    populateSettings(s.config);

    const locked = !!s.ui_locked;
    $('workbench-lock-overlay').classList.toggle('hidden', !locked);
    if (locked) {
      $('lock-overlay-status').textContent = `HTTP :${s.config?.listen?.http_port || 18897} · 静态出口 ${ip} 持续守护`;
    }
  }

  function renderPACStatus(pac) {
    if (!pac) return;
    pacEnabled = !!pac.matches_peaceclaude;
    const tag = $('pac-status-tag');
    const btn = $('btn-toggle-pac');
    if (pacEnabled) {
      tag.className = 'status-tag pass';
      tag.textContent = '已开启 PAC';
      btn.textContent = '关闭浏览器 PAC';
    } else {
      tag.className = 'status-tag warn';
      tag.textContent = pac.system_pac_enabled ? '非 PeaceClaude PAC' : '未开启';
      btn.textContent = '开启浏览器 PAC';
    }
    $('pac-status-desc').textContent = pac.note || '';
    $('account-pac-warn')?.classList.toggle('hidden', pacEnabled);
  }

  let autostartPolicyEnabled = true;
  function renderServiceAutostart(svc) {
    if (!svc) return;
    autostartPolicyEnabled = svc.auto_start_enabled !== false;
    const tag = $('autostart-status-tag');
    const desc = $('autostart-status-desc');
    const btn = $('btn-toggle-autostart');
    if (!tag || !desc || !btn) return;
    if (!autostartPolicyEnabled) {
      tag.className = 'status-tag';
      tag.textContent = '已关闭';
      desc.textContent = `开机自启策略已关闭（${svc.launch_agent_path || '~/Library/LaunchAgents/com.peaceclaude.daemon.plist'}），当前守护进程运行于 PID ${svc.pid || '—'}。`;
      btn.textContent = '开启开机自启';
      return;
    }
    if (svc.launch_agent_installed && svc.launch_agent_valid && svc.launch_agent_loaded) {
      tag.className = 'status-tag pass';
      tag.textContent = '已注册开机自启';
      desc.textContent = `com.peaceclaude.daemon 已注册并由 launchd 托管 (PID ${svc.pid || '—'} · ${svc.launch_agent_binary || svc.launch_agent_path || ''})。`;
    } else if (svc.launch_agent_installed && svc.launch_agent_valid) {
      tag.className = 'status-tag warn';
      tag.textContent = '待激活托管';
      desc.textContent = `已写入开机项 plist（${svc.launch_agent_path || ''}），将在下次启动 PeaceClaude.app 或运行 peaceclaude service ensure-autostart 时接入 launchd。`;
    } else {
      tag.className = 'status-tag warn';
      tag.textContent = '未注册开机项';
      desc.textContent = `检测到尚未注册或失效的 LaunchAgent（${svc.launch_agent_path || ''}）；点击右侧或打开 PeaceClaude.app 将自动补齐注册。`;
    }
    btn.textContent = '关闭开机自启';
  }

  function renderUpstreamRoute(ur) {
    const tag = $('upstream-route-hint');
    if (!tag || !ur) return;
    if (ur.bound_physical && ur.active_interface) {
      tag.className = 'status-tag pass';
      tag.textContent = `出站 ${ur.active_interface} (已绕开 VPN)`;
      tag.title = '直连外部静态代理已绑定物理出口网卡 (IP_BOUND_IF)，不影响公司内网与飞书走 VPN';
    } else if (ur.using_fallback) {
      tag.className = 'status-tag warn';
      tag.textContent = '已回退系统路由';
      tag.title = ur.fallback_reason || '物理网卡直连失败，已自动回退系统默认路由';
    } else {
      tag.className = 'status-tag';
      tag.textContent = ur.active_interface === 'system-default' ? '系统默认路由' : (ur.active_interface || '系统路由');
      tag.title = ur.skipped_reason || '';
    }
  }

  function renderStorageInfo(st) {
    const el = $('storage-paths-box');
    if (!el || !st) return;
    const rows = [
      ['存储引擎与权限', st.backend_type || '本地目录 + JSON (0700/0600)'],
      ['根目录 (BaseDir)', st.base_dir],
      ['多账号隔离目录', `${st.profiles_dir} (${fmtBytes(st.profiles_bytes)})`],
      ['全局配置文件', st.config_path],
      ['日志与 30 天保留', `${st.logs_dir} (${fmtBytes(st.logs_bytes)} · ${st.retention_days || 30} 天)`],
      ['leaks.jsonl 当前占用', fmtBytes(st.leaks_bytes)],
      ['captures.jsonl 当前占用', fmtBytes(st.captures_bytes)],
      ['共享规则目录', st.shared_rules_dir],
      ['清理归档备份目录', st.backups_dir],
    ];
    el.innerHTML = rows.map(([k, v]) => `<div class="kv-row"><span>${esc(k)}</span><code>${esc(v || '—')}</code></div>`).join('');
  }

  // --- Accounts: Hero Instrument Stage + Compact Roster ---
  function renderAvatarPicker(containerId, hiddenInputId, selectedId) {
    const box = $(containerId);
    const input = $(hiddenInputId);
    const activeId = (selectedId && ANIMAL_AVATAR_MAP[selectedId]) ? selectedId : 'tiger';
    if (input) input.value = activeId;
    if (!box) return;
    box.innerHTML = ANIMAL_AVATARS.map((item) => `
      <button type="button" class="avatar-choice ${item.id === activeId ? 'active' : ''}" data-avatar-pick="${esc(item.id)}" title="${esc(item.label)}">
        <img src="/avatars/${esc(item.id)}.png" alt="${esc(item.label)}" />
        <span>${esc(item.label)}</span>
      </button>`).join('');
  }
  function bindAvatarPicker(containerId, hiddenInputId) {
    $(containerId)?.addEventListener('click', (ev) => {
      const btn = ev.target.closest('button[data-avatar-pick]');
      if (!btn) return;
      const picked = btn.dataset.avatarPick;
      const input = $(hiddenInputId);
      if (input) input.value = picked;
      $(containerId).querySelectorAll('button[data-avatar-pick]').forEach((b) => {
        b.classList.toggle('active', b.dataset.avatarPick === picked);
      });
    });
  }
  function avatarInitial(a) {
    const src = (a.account && a.account.email) || a.display_name || a.name || 'C';
    return String(src).trim().charAt(0).toUpperCase();
  }
  function billingLabel(bt) {
    if (!bt) return '';
    if (bt.includes('apple')) return 'Apple 内购';
    if (bt.includes('stripe')) return 'Stripe 订阅';
    return bt;
  }
  function formatResetRel(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '';
    const diffMin = Math.round((d.getTime() - Date.now()) / 60000);
    if (diffMin <= 0) return '即将重置';
    if (diffMin < 60) return `${diffMin}m 后重置`;
    const hrs = Math.floor(diffMin / 60);
    if (hrs < 24) return `${hrs}h ${diffMin % 60}m 后重置`;
    return `${Math.floor(hrs / 24)}d 后重置`;
  }
  function usageBarHTML(win, title) {
    if (!win) {
      return `
        <div class="usage-row">
          <div class="usage-row-top">
            <span class="usage-label">${esc(title)}</span>
            <span class="usage-meta">待同步实时用量</span>
          </div>
          <div class="progress-track"><div class="progress-fill" style="width:0%"></div></div>
        </div>`;
    }
    const pct = Math.max(0, Math.min(100, Math.round(win.utilization || 0)));
    const cls = pct >= 85 ? 'danger' : pct >= 65 ? 'warn' : '';
    const resetTxt = formatResetRel(win.resets_at);
    return `
      <div class="usage-row">
        <div class="usage-row-top">
          <span class="usage-label">${esc(title)}</span>
          <span class="usage-meta"><strong>${pct}%</strong>${resetTxt ? ' · ' + esc(resetTxt) : ''}</span>
        </div>
        <div class="progress-track"><div class="progress-fill ${cls}" style="width:${pct}%"></div></div>
      </div>`;
  }
  function isAccountActivated(a) {
    const acct = a.account || {};
    return !!(a.has_cli_creds || a.has_desktop_auth || acct.source === 'claude_json');
  }
  function planTierClass(plan) {
    const p = String(plan || '').toLowerCase();
    if (!p || p === '未识别') return 'tier-unknown';
    if (p.includes('free')) return 'tier-free';
    if (p.includes('ultra')) return 'tier-ultra';
    if (p.includes('max')) return 'tier-max';
    if (p.includes('pro')) return 'tier-pro';
    if (p.includes('team') || p.includes('enterprise')) return 'tier-team';
    return 'tier-unknown';
  }
  function isFreeAccount(a) {
    const usage = a.usage || {};
    const acct = a.account || {};
    if (usage.five_hour || usage.weekly) return false;
    if (usage.status === 'free_no_quota' || usage.source === 'free_tier') return true;
    return String(acct.plan_label || '').toLowerCase() === 'free';
  }
  function secBadgeHTML(a) {
    const sec = a.security;
    if (!sec || typeof sec.score !== 'number') return '';
    const cls = sec.grade === 'safe' ? 'safe' : (sec.grade === 'caution' ? 'caution' : 'unsafe');
    const title = sec.summary || `${sec.label || ''} · 点击查看安全检测`;
    return `<button type="button" class="sec-score-badge ${cls}" data-acc-security="${esc(a.name)}" title="${esc(title)}">安全 ${esc(sec.score)}分</button>`;
  }
  function freeUsageNoticeHTML(a) {
    const usage = a.usage || {};
    const reason = usage.hint || usage.note || '按会话动态限流，升级 Pro / Max 后自动采集实时 5h / Weekly 用量进度';
    return `
      <div class="free-usage-strip">
        <span class="free-strip-title">免费账号（Free）无 5h / Weekly 付费配额</span>
        <span class="free-strip-sep" aria-hidden="true">·</span>
        <span class="free-strip-desc">${esc(reason)}</span>
      </div>`;
  }
  function freeUsageRosterHTML() {
    return `
      <div class="roster-free-usage">
        <strong>免费账号 (Free)</strong>
        <span>无 5h / Weekly 付费配额</span>
      </div>`;
  }

  function renderAccounts(data) {
    accountsData = data;
    const items = data.accounts || [];
    const runningItems = items.filter((a) => !!a.running_desktop_pid);
    const activatedCount = items.filter(isAccountActivated).length;
    const singleActive = data.single_active_desktop !== false;

    $('nav-account-count').textContent = String(items.length);
    const rec = data.recommended_profile ? ` · 优先建议 ${data.recommended_profile}` : '';
    $('accounts-summary-line').textContent = `${items.length} 个隔离档案 · ${activatedCount} 个已激活 · ${runningItems.length} 个 Desktop 运行中${rec}`;
    $('account-multi-warn')?.classList.toggle('hidden', runningItems.length <= 1);

    if (!items.length) {
      $('account-hero-slot').innerHTML = '<div class="card"><p class="muted">暂无账号，请点击右上角「新增账号」</p></div>';
      $('account-roster-wrap').classList.add('hidden');
      return;
    }

    const hero = items.find((a) => a.active) || items[0];
    const others = items.filter((a) => a.name !== hero.name);
    const canDelete = items.length > 1;

    // Render Hero Active Account Card
    {
      const acct = hero.account || {};
      const usage = hero.usage || {};
      const plan = acct.plan_label || '未识别';
      const displayName = hero.display_name || hero.name;
      const activated = isAccountActivated(hero);
      const isFree = isFreeAccount(hero);
      const otherRunning = !hero.running_desktop_pid && runningItems.length > 0 && singleActive;
      const idShort = acct.account_uuid ? acct.account_uuid.slice(0, 8) : (acct.org_uuid ? 'org:' + acct.org_uuid.slice(0, 8) : '待绑定');
      const regText = acct.account_age_text ? `${acct.account_age_text} (${fmtDate(acct.account_created_at)})` : '注册时间未记录';
      const usageSrc = usage.source === 'oauth_api'
        ? 'OAuth 实时'
        : usage.source === 'desktop_history'
        ? '本地样本'
        : usage.source === 'free_tier'
        ? 'Free 档案'
        : '待同步';
      const subText = acct.email ? `${acct.email} · @${hero.name}` : `@${hero.name}${hero.note ? ' · ' + hero.note : ''}`;
      let deskLabel = '启动 Desktop 登录';
      if (hero.running_desktop_pid) deskLabel = '切到前台';
      else if (otherRunning && activated) deskLabel = '切换并打开 Desktop';
      else if (otherRunning) deskLabel = '切换并登录 Desktop';
      else if (activated) deskLabel = '启动 Desktop';

      $('account-hero-slot').innerHTML = `
        <article class="card hero-account-card ${hero.active ? 'is-current-account' : ''}" data-acc-card="${esc(hero.name)}">
          <div class="hero-card-header">
            <div class="account-title-wrap">
              <div class="avatar-circle lg" title="点击「详情与编辑」可切换 9 款 3D 动物头像">${avatarMediaHTML(hero)}</div>
              <div style="min-width:0; flex:1;">
                <div class="account-title-line">
                  <h3 class="account-name name-ellipsis" title="${esc(displayName)}">${esc(displayName)}</h3>
                  <span class="plan-badge ${planTierClass(plan)}">${esc(plan)}</span>
                  ${hero.active ? '<span class="active-use-badge">当前使用</span>' : '<span class="mono-tag">待选</span>'}
                  ${secBadgeHTML(hero)}
                  <span class="state-dot-label ${activated ? 'ok' : ''}">
                    <span class="status-dot ${activated ? 'ok' : ''}"></span>
                    ${activated ? '已激活' : '未登录'}
                  </span>
                </div>
                <div class="account-sub">${esc(subText)}</div>
              </div>
            </div>
            <div class="hero-primary-actions">
              <button class="btn ${hero.running_desktop_pid || !otherRunning ? 'btn-primary' : 'btn-accent'} btn-sm" data-acc-desktop="${esc(hero.name)}" type="button">${esc(deskLabel)}</button>
              ${hero.running_desktop_pid ? `<button class="btn btn-ghost btn-sm" data-acc-stop="${esc(hero.name)}" type="button">停止</button>` : ''}
              <button class="btn btn-secondary btn-sm" data-acc-browser="${esc(hero.name)}" type="button">隔离浏览器登录</button>
            </div>
          </div>

          <div class="hero-quota-band">
            ${isFree ? freeUsageNoticeHTML(hero) : `<div class="usage-bars-grid">${usageBarHTML(usage.five_hour, '5-Hour Limit')}${usageBarHTML(usage.weekly, 'Weekly · All Models')}</div>`}
          </div>

          <div class="hero-card-footer">
            <div class="hero-telemetry-strip">
              <div class="hero-tele-item"><small>账号 ID / 订阅</small><span>${esc(idShort)}${acct.billing_type ? ' · ' + esc(billingLabel(acct.billing_type)) : ''}</span></div>
              <div class="hero-tele-item"><small>注册资历 / 来源</small><span>${esc(regText)} · ${esc(usageSrc)}</span></div>
              <div class="hero-tele-item"><small>Desktop 进程</small><span class="mono">${hero.running_desktop_pid ? 'PID ' + esc(hero.running_desktop_pid) : '未启动'}</span></div>
              <div class="hero-tele-item"><small>隔离设备指纹</small><span class="mono">${esc((hero.fingerprint?.device_uuid || '—').slice(0, 12))}</span></div>
            </div>
            <div class="hero-secondary-actions">
              <button class="btn btn-ghost btn-sm" data-acc-detail="${esc(hero.name)}" type="button">详情与编辑</button>
              <button class="btn btn-ghost btn-sm" data-acc-cli="${esc(hero.name)}" type="button">复制 CLI</button>
              ${canDelete ? `<button class="btn btn-danger-ghost btn-sm" data-acc-delete="${esc(hero.name)}" type="button">删除</button>` : ''}
            </div>
          </div>
        </article>`;
    }

    // Render Standby Roster Rows
    if (others.length === 0) {
      $('account-roster-wrap').classList.add('hidden');
      $('account-roster-rows').innerHTML = '';
    } else {
      $('account-roster-wrap').classList.remove('hidden');
      $('roster-count-note').textContent = `共 ${others.length} 个待命档案`;
      $('account-roster-rows').innerHTML = others.map((a) => {
        const acct = a.account || {};
        const usage = a.usage || {};
        const plan = acct.plan_label || '未识别';
        const displayName = a.display_name || a.name;
        const activated = isAccountActivated(a);
        const isFree = isFreeAccount(a);
        const fh = usage.five_hour ? Math.round(usage.five_hour.utilization || 0) : null;
        const wk = usage.weekly ? Math.round(usage.weekly.utilization || 0) : null;
        const otherRunning = !a.running_desktop_pid && runningItems.length > 0 && singleActive;
        let deskLabel = a.running_desktop_pid ? '前台' : (otherRunning ? '切换登录' : (activated ? '启动' : '登录'));
        return `
          <div class="roster-row" data-acc-card="${esc(a.name)}">
            <div class="roster-col-id">
              <div class="avatar-circle">${avatarMediaHTML(a)}</div>
              <div>
                <div class="roster-name-line">
                  <strong class="name-ellipsis" title="${esc(displayName)}">${esc(displayName)}</strong>
                  <span class="plan-badge ${planTierClass(plan)}">${esc(plan)}</span>
                  ${a.active ? '<span class="active-use-badge">当前使用</span>' : ''}
                  ${secBadgeHTML(a)}
                </div>
                <div class="roster-sub">${esc(acct.email ? `${acct.email} · @${a.name}` : `@${a.name}${a.note ? ' · ' + a.note : ''}`)}</div>
              </div>
            </div>

            <div class="roster-col-state">
              <span class="state-dot-label ${activated ? 'ok' : ''}">
                <span class="status-dot ${activated ? 'ok' : ''}"></span>
                ${activated ? '已激活' : '未登录'}
              </span>
              <span class="mono-tag">${a.running_desktop_pid ? 'PID ' + esc(a.running_desktop_pid) : 'Desktop 未运行'}</span>
            </div>

            <div class="roster-col-usage">
              ${isFree ? freeUsageRosterHTML() : `
              <div class="mini-bar-row">
                <span>5h</span>
                <div class="progress-track"><div class="progress-fill ${fh != null && fh >= 85 ? 'danger' : fh != null && fh >= 65 ? 'warn' : ''}" style="width:${fh ?? 0}%"></div></div>
                <span>${fh == null ? '—' : fh + '%'}</span>
              </div>
              <div class="mini-bar-row">
                <span>7d</span>
                <div class="progress-track"><div class="progress-fill ${wk != null && wk >= 85 ? 'danger' : wk != null && wk >= 65 ? 'warn' : ''}" style="width:${wk ?? 0}%"></div></div>
                <span>${wk == null ? '—' : wk + '%'}</span>
              </div>`}
            </div>

            <div class="btn-row">
              <button class="btn ${otherRunning ? 'btn-accent' : 'btn-secondary'} btn-sm" data-acc-desktop="${esc(a.name)}" type="button">${esc(deskLabel)}</button>
              ${a.running_desktop_pid ? `<button class="btn btn-ghost btn-sm" data-acc-stop="${esc(a.name)}" type="button">停止</button>` : ''}
              <button class="btn btn-secondary btn-sm" data-acc-browser="${esc(a.name)}" type="button">浏览器</button>
              <button class="btn btn-secondary btn-sm" data-acc-detail="${esc(a.name)}" type="button">详情</button>
              <button class="btn btn-ghost btn-sm" data-acc-use="${esc(a.name)}" type="button">设为使用</button>
              ${canDelete ? `<button class="btn btn-danger-ghost btn-sm" data-acc-delete="${esc(a.name)}" type="button">删除</button>` : ''}
            </div>
          </div>`;
      }).join('');
    }
  }

  function renderAccountDetail(a) {
    currentDetailName = a.name;
    const acct = a.account || {};
    const usage = a.usage || {};
    const activated = isAccountActivated(a);
    const isFree = isFreeAccount(a);
    const displayName = a.display_name || a.name;

    $('det-avatar').innerHTML = avatarMediaHTML(a);
    $('det-display-name').textContent = displayName;
    $('det-display-name').title = displayName;
    $('det-plan-badge').textContent = acct.plan_label || '未识别';
    $('det-plan-badge').className = 'plan-badge ' + planTierClass(acct.plan_label);
    $('det-activated-tag').classList.toggle('hidden', !activated);
    $('det-active-tag').classList.toggle('hidden', !a.active);
    const detSecEl = $('det-sec-badge');
    if (detSecEl) {
      const sec = a.security;
      if (sec && typeof sec.score === 'number') {
        const cls = sec.grade === 'safe' ? 'safe' : (sec.grade === 'caution' ? 'caution' : 'unsafe');
        detSecEl.className = `sec-score-badge ${cls}`;
        detSecEl.textContent = `安全 ${sec.score}分`;
        detSecEl.title = sec.summary || '点击前往安全检测';
      } else {
        detSecEl.className = 'sec-score-badge hidden';
      }
    }
    const runTag = $('det-running-tag');
    runTag.classList.toggle('hidden', !a.running_desktop_pid);
    if (a.running_desktop_pid) runTag.textContent = `Desktop PID ${a.running_desktop_pid}`;

    $('det-subtitle').textContent = `档案 ID：${a.name}` + (acct.email ? ` · ${acct.email}` : '') + (a.note ? ` · ${a.note}` : '');
    $('det-btn-desktop').textContent = a.running_desktop_pid ? '切到 Desktop 前台' : (activated ? '切换并打开 Desktop' : '启动 Desktop 登录');
    $('det-btn-stop-desktop').classList.toggle('hidden', !a.running_desktop_pid);
    $('det-btn-use').classList.toggle('hidden', !!a.active);
    const canDelete = ((accountsData && accountsData.accounts) || []).length > 1;
    $('det-btn-delete').classList.toggle('hidden', !canDelete);

    $('det-usage-source').textContent = usage.source === 'oauth_api'
      ? `OAuth 实时 API (${fmtTime(usage.fetched_at)})`
      : usage.source === 'desktop_history'
      ? `Desktop 本地样本 (${fmtTime(usage.fetched_at)})`
      : usage.source === 'free_tier'
      ? 'Free 档案（无需额度同步）'
      : '未连接实时 API';

    const bars = isFree
      ? [freeUsageNoticeHTML(a)]
      : [
          usageBarHTML(usage.five_hour, '5-Hour Limit (5小时额度)'),
          usageBarHTML(usage.weekly, 'Weekly · All Models (7天总用量)'),
        ];
    if (!isFree && usage.weekly_sonnet) bars.push(usageBarHTML(usage.weekly_sonnet, 'Weekly · Sonnet'));
    if (!isFree && usage.weekly_opus) bars.push(usageBarHTML(usage.weekly_opus, 'Weekly · Opus'));
    if (usage.extra_usage) {
      const eu = usage.extra_usage;
      bars.push(`<div class="kv-row"><span>Extra Usage</span><code>${eu.enabled ? `已开启 · ${eu.used_credits ?? 0} / ${eu.monthly_limit ?? '—'} ${eu.currency || ''}` : '未开启'}</code></div>`);
    }
    $('det-usage-bars').innerHTML = bars.join('');
    $('det-usage-note').textContent = usage.hint || usage.note || usage.error || (isFree ? '当前账号为免费版（Free），无固定 5h / Weekly 付费配额' : '如开启 OAuth API 实时额度同步，可在终端完成 peaceclaude cli 登录');

    const kvRows = [
      ['档案标识与目录', a.dir || a.name],
      ['账号激活态', activated ? '已激活（本地存在认证凭据）' : '未登录 / 待完成 OAuth 认证'],
      ['Desktop 进程', a.running_desktop_pid ? `运行中 (PID ${a.running_desktop_pid})` : '未启动'],
      ['账号邮箱', acct.email || '未登录或仅网页登录'],
      ['Account UUID', acct.account_uuid || '—'],
      ['Organization ID', acct.org_uuid || '—'],
      ['套餐 / Rate Tier', `${acct.plan_label || '未识别'}${acct.rate_limit_tier ? ' · ' + acct.rate_limit_tier : ''}`],
      ['计费方式', billingLabel(acct.billing_type) || '—'],
      ['注册日期', acct.account_created_at ? `${fmtDate(acct.account_created_at)}（${acct.account_age_text || ''}）` : '未记录'],
      ['订阅起始', acct.subscription_created_at ? `${fmtDate(acct.subscription_created_at)}（${acct.subscription_age_text || ''}）` : '—'],
      ['隔离设备 UUID', a.fingerprint?.device_uuid || '—'],
      ['最后启动', a.last_used_at ? new Date(a.last_used_at).toLocaleString('zh-CN', { hour12: false }) : '—'],
    ];
    $('det-identity-kv').innerHTML = kvRows.map(([k, v]) => `<div class="kv-row"><span>${esc(k)}</span><code>${esc(v)}</code></div>`).join('');

    $('det-edit-display').value = a.display_name || '';
    renderAvatarPicker('det-avatar-picker', 'det-edit-avatar', resolveAvatarID(a));
    $('det-edit-note').value = a.note || '';
    $('det-edit-plan').value = a.manual_plan || '';
    $('det-edit-reg').value = a.manual_registered_at || '';
    $('det-edit-tz').value = a.timezone || '';
    $('det-edit-lang').value = a.language || '';
  }

  async function openAccountDetail(name, refresh = false) {
    try {
      const det = await api(`/api/accounts/detail?name=${encodeURIComponent(name)}&refresh=${refresh ? '1' : '0'}`);
      renderAccountDetail(det);
      $('account-detail-modal').classList.remove('hidden');
    } catch (e) { toast('读取详情失败: ' + e.message); }
  }

  // --- Security / Doctor / Leaks / Captures ---
  function renderDoctor(doc) {
    const score = Number(doc.score || 0);
    const circ = 264;
    const ring = $('score-ring-fg');
    ring.style.strokeDashoffset = String(circ * (1 - score / 100));
    ring.style.stroke = doc.grade === 'safe' ? 'var(--green)' : doc.grade === 'caution' ? 'var(--amber)' : 'var(--red)';
    $('doctor-score').textContent = String(score);
    $('doctor-crit-count').textContent = `${doc.critical_passed || 0} / ${doc.critical_total || 11}`;

    const badge = $('doctor-grade');
    badge.className = 'grade-badge ' + (doc.grade === 'safe' ? '' : doc.grade);
    badge.textContent = doc.grade === 'safe' ? '安全就绪' : doc.grade === 'caution' ? '可用但有提醒' : '存在阻塞风险';
    $('doctor-summary-text').textContent = `宿主机 ${doc.host_timezone || '?'} -> 隔离环境 ${doc.peace_timezone || '?'} / ${doc.peace_language || 'en-US'}`;

    const fails = (doc.checks || []).filter((c) => c.status === 'fail');
    const warns = (doc.checks || []).filter((c) => c.status === 'warn');
    const anomalies = fails.length + warns.length;
    const secBadge = $('nav-security-badge');
    if (anomalies === 0) {
      secBadge.textContent = `${doc.critical_passed || 11}/${doc.critical_total || 11}`;
      secBadge.className = 'nav-count';
      $('doctor-anomaly-box').classList.add('hidden');
    } else {
      secBadge.textContent = `${anomalies} 异常`;
      secBadge.className = 'nav-count ' + (fails.length > 0 ? 'danger' : 'warn');
      $('doctor-anomaly-box').textContent = `发现 ${fails.length} 项高风险与 ${warns.length} 项提醒，请按建议逐项排查`;
      $('doctor-anomaly-box').classList.remove('hidden');
    }

    const fixAllBtn = $('btn-fix-all-doctor');
    const fixableCount = (doc.checks || []).filter((c) => c.status !== 'pass' && c.fixable && c.fix_action).length;
    if (fixAllBtn) {
      fixAllBtn.disabled = fixableCount === 0;
      fixAllBtn.textContent = fixableCount > 0 ? `一键修复全部 (${fixableCount})` : '全部安全项已就绪';
    }

    $('doctor-checks').innerHTML = (doc.checks || []).map((c) => {
      const fixText = c.status !== 'pass' ? (c.fix_hint || c.remediation || '') : '';
      const canFix = c.status !== 'pass' && c.fixable && c.fix_action;
      return `
      <div class="check-row">
        <div class="check-row-head">
          <div class="check-row-head-left">
            <span class="status-tag ${esc(c.status)}">${c.status === 'pass' ? '通过' : c.status === 'warn' ? '提醒' : '失败'}</span>
            <div class="check-title">${esc(c.title)}</div>
          </div>
          ${canFix ? `<div class="check-row-act"><button type="button" class="btn ${c.status === 'fail' ? 'btn-primary' : 'btn-secondary'} btn-sm" data-doc-fix-id="${esc(c.id)}" data-doc-fix-act="${esc(c.fix_action)}">${esc(c.fix_label || '一键修复')}</button></div>` : ''}
        </div>
        <div class="check-row-body">
          <div class="check-detail">${esc(c.detail)}</div>
          ${fixText ? `<div class="check-fix">建议：${esc(fixText)}</div>` : ''}
        </div>
      </div>`;
    }).join('');
  }

  function renderRules(st) {
    if (!st) return;
    $('toggle-shared-rules').checked = !!st.enabled;
    const profEntries = Object.entries(st.profiles || {}).map(([name, state]) => `<div class="kv-row"><span>账号 ${esc(name)}</span><code>${esc(state)}</code></div>`).join('');
    $('rules-status-box').innerHTML = `
      <div class="kv-row"><span>共享目录</span><code>${esc(st.shared_dir || '')}</code></div>
      <div class="kv-row"><span>同步条目</span><code>${esc((st.items || []).join(', '))}</code></div>
      ${profEntries}`;
  }

  function renderVMProxy(vp) {
    if (!vp) return;
    const pfState = vp.peace_pf_anchor_exists
      ? '已安装 (/etc/pf.anchors/peaceclaude)'
      : vp.legacy_pf_anchor_exists
      ? '已有宿主 pf (/etc/pf.anchors/claude.egress)'
      : '未安装系统 pfctl';
    $('vmproxy-box').innerHTML = [
      ['pf 护盾状态', pfState],
      ['pf 语法校验', vp.syntax_valid ? '通过 (pfctl -n)' : (vp.syntax_detail || '未就绪')],
      ['Cowork VM 接入', vp.config_enabled ? `已启用 (${(vp.subnets || []).join(', ')})` : '未开启'],
      ['pf 规则路径', vp.rendered_anchor_path || '~/.peaceclaude/state/pf/peaceclaude.pf.anchor'],
    ].map(([k, v]) => `<div class="kv-row"><span>${esc(k)}</span><code>${esc(v)}</code></div>`).join('');
    if (vp.install_script_path) $('pf-install-cmd').textContent = `sudo bash "${vp.install_script_path}"`;
  }

  function populateSettings(cfg) {
    if (!cfg || populateSettings._dirty) return;
    $('cfg-upstream-mode').value = cfg.upstream.mode || 'local-proxy';
    $('cfg-upstream-addr').value = cfg.upstream.address || '';
    $('cfg-expected-ip').value = cfg.egress.expected_ip || '';
    $('cfg-allowed-countries').value = (cfg.egress.allowed_countries || []).join(',');
    $('cfg-language').value = cfg.identity.language || 'en-US';
    $('cfg-timezone').value = cfg.identity.timezone || '';
    $('cfg-single-active').checked = cfg.identity.single_active_desktop !== false;
    $('cfg-auto-peace').checked = !!cfg.identity.auto_peace_desktop;
    $('cfg-fail-closed').checked = !!cfg.egress.fail_closed;
    $('cfg-sync-tz').checked = !!cfg.identity.sync_timezone;
    $('cfg-telemetry-shield').checked = !!cfg.shield.telemetry_shield;
    $('cfg-strict-split').checked = !!cfg.egress.strict_domain_isolation;
    $('cfg-skip-sse').checked = !!cfg.egress.skip_probe_during_sse;
    $('cfg-strip-host-env').checked = !!cfg.identity.strip_host_anthropic_env;
    $('cfg-corp-direct').checked = !!cfg.shield.allow_corporate_direct;
    if ($('cfg-proxy-auth-domains')) $('cfg-proxy-auth-domains').checked = !!cfg.shield.proxy_auth_domains;
    if ($('cfg-desktop-proxy-mode')) $('cfg-desktop-proxy-mode').value = (cfg.shield.desktop_proxy_mode === 'proxy' ? 'proxy' : 'pac');
    if ($('cfg-extra-claude-domains')) $('cfg-extra-claude-domains').value = (cfg.shield.extra_claude_domains || []).join(', ');
    if ($('cfg-corporate-domains')) $('cfg-corporate-domains').value = (cfg.shield.corporate_domains || []).join(', ');
    if ($('cfg-extra-blocked-domains')) $('cfg-extra-blocked-domains').value = (cfg.shield.extra_blocked_domains || []).join(', ');
    if ($('cfg-bypass-vpn')) $('cfg-bypass-vpn').checked = cfg.upstream.bypass_vpn !== false;
    $('cfg-vm-proxy').checked = !!cfg.listen.vm_proxy_enabled;
    $('cfg-cred-hint').textContent = cfg.upstream.has_credentials ? '上游账密已加密存盘（界面不回显）' : '当前上游无认证凭据';
  }

  // --- Remote Pairing & 120s Ticket Countdown ---
  function updateCountdownUI(ticket) {
    clearInterval(pairCountdownTimer);
    pairCountdownTimer = null;
    const tag = $('remote-countdown-tag');
    const bar = $('remote-countdown-bar');
    if (!ticket || !ticket.expires_at) {
      if (tag) { tag.textContent = '待生成'; tag.className = 'countdown-tag mono'; }
      if (bar) bar.style.width = '0%';
      return;
    }
    const expMs = new Date(ticket.expires_at).getTime();
    const tick = () => {
      const leftSec = Math.max(0, Math.round((expMs - Date.now()) / 1000));
      if (leftSec <= 0) {
        tag.textContent = '已过期';
        tag.className = 'countdown-tag mono expired';
        bar.style.width = '0%';
        clearInterval(pairCountdownTimer);
        pairCountdownTimer = null;
        return;
      }
      tag.textContent = `剩余 ${leftSec}s`;
      tag.className = 'countdown-tag mono live';
      bar.style.width = Math.max(2, Math.min(100, (leftSec / 120) * 100)).toFixed(1) + '%';
    };
    tick();
    pairCountdownTimer = setInterval(tick, 1000);
  }

  function renderRemote(r) {
    remoteData = r;
    const mode = r.relay_mode || 'lan';
    $('remote-mode-seg').querySelectorAll('button[data-rmode]').forEach((b) => {
      b.classList.toggle('active', b.dataset.rmode === mode);
    });
    $('remote-lan-panel').classList.toggle('hidden', mode !== 'lan');
    $('remote-custom-panel').classList.toggle('hidden', mode !== 'custom');
    $('remote-official-panel').classList.toggle('hidden', mode !== 'official');

    $('topo-daemon-addr').textContent = `127.0.0.1:${statusData?.config?.listen?.control_port || 18899}`;
    $('topo-channel-mode').textContent = mode === 'custom' ? '自建 HTTPS 中继' : mode === 'official' ? '架构边界说明' : '局域网 / Tailscale';
    $('topo-mobile-addr').textContent = r.listen_addr || '待开启监听';

    $('cfg-remote-enabled').checked = !!r.enabled;
    if (r.mobile_port) $('cfg-remote-port').value = String(r.mobile_port);
    if (r.custom_relay_url && document.activeElement !== $('cfg-custom-relay-url')) {
      $('cfg-custom-relay-url').value = r.custom_relay_url;
    }

    const addrs = (r.host_addresses || []).map((a) => `${a.kind === 'tailscale' ? 'Tailscale' : '局域网'} · ${a.ip} (${a.interface})`).join(' | ') || '未检测到局域网 IP';
    $('remote-lan-info').innerHTML = [
      ['移动控制监听状态', r.listen_addr ? `运行中 (${r.listen_addr})` : '未启动（生成通行证时自动开启）'],
      ['本机可达地址', addrs],
    ].map(([k, v]) => `<div class="kv-row"><span>${esc(k)}</span><code>${esc(v)}</code></div>`).join('');

    const devs = r.paired_devices || [];
    const ticket = r.active_pair;
    const qrBox = $('remote-qr-box');
    const metaEl = $('remote-pair-meta');

    if (ticket && ticket.svg) {
      qrBox.classList.add('has-svg');
      qrBox.innerHTML = ticket.svg;
      metaEl.classList.remove('hidden');
      $('remote-pair-code-text').textContent = ticket.code;
      $('remote-pair-url-text').textContent = ticket.url;
      $('btn-gen-pair-qr').innerHTML = '<span class="nav-ico" data-icon="qr-code"></span><span>刷新配对通行证</span>';
      hydrateIcons($('btn-gen-pair-qr'));
      updateCountdownUI(ticket);
    } else {
      qrBox.classList.remove('has-svg');
      qrBox.innerHTML = `
        <div class="qr-standby">
          <span class="nav-ico qr-placeholder-ico" data-icon="qr-code"></span>
          <ol class="qr-steps">
            <li>手机与 Mac 连入同一 Wi-Fi 或 Tailscale</li>
            <li>点击下方按钮生成 120 秒一次性二维码</li>
            <li>相机扫码即自动换取 Bearer Token 进入控制台</li>
          </ol>
        </div>`;
      hydrateIcons(qrBox);
      metaEl.classList.add('hidden');
      $('btn-gen-pair-qr').innerHTML = '<span class="nav-ico" data-icon="qr-code"></span><span>生成配对通行证</span>';
      hydrateIcons($('btn-gen-pair-qr'));
      updateCountdownUI(null);
    }

    const devList = $('remote-devices-list');
    $('btn-revoke-remote').classList.toggle('hidden', devs.length === 0);
    $('remote-devices-sub').textContent = devs.length > 0 ? `共授权 ${devs.length} 台手机控制台会话` : '尚未授权任何移动设备';
    if (devs.length > 0) {
      devList.innerHTML = devs.map((d) => `
        <div class="kv-row">
          <span>${esc(d.name)} (${esc(d.os)}) · 配对于 ${esc(fmtTime(d.paired_at))}</span>
          <button class="btn btn-ghost btn-sm" data-revoke-dev="${esc(d.id)}" type="button">撤销设备</button>
        </div>`).join('');
    } else {
      devList.innerHTML = '<p class="muted">生成右侧一次性配对通行证并扫码后，设备会话将列于此处，支持随时单台踢除</p>';
    }
    $('remote-card-title').textContent = devs.length > 0 ? `一次性配对通行证（已连 ${devs.length} 台）` : '一次性配对通行证';
  }

  async function loadRemote() {
    try { renderRemote(await api('/api/remote')); } catch (e) { toast('读取远程配对状态失败: ' + e.message); }
  }

  let nativeBrowsersData = null;
  let lastGuideExtURL = '';
  let lastGuideBrowserID = '';

  function summarizeProfileList(profs) {
    if (!profs || profs.length === 0) return '';
    if (profs.length <= 3) return profs.join(', ');
    return `${profs.slice(0, 3).join(', ')} 等共 ${profs.length} 个`;
  }

  function renderNativeBrowsers(rep) {
    nativeBrowsersData = rep;
    const grid = $('nb-browser-grid');
    const meta = $('nb-meta-line');
    if (!grid || !meta || !rep) return;

    const pacBadge = rep.pac_enabled
      ? '<span class="status-tag pass">PAC 已开启</span>'
      : '<span class="status-tag warn">PAC 未开启</span>';
    const domains = (rep.protected_domains || []).join(' · ');
    meta.innerHTML = `
      ${pacBadge}
      <span>目标指纹：<code>${esc(rep.target_timezone || 'America/Denver')}</code> · <code>${esc(rep.target_locale || 'en-US')}</code></span>
      <span>护航域：<code>${esc(domains)}</code></span>
      <span>扩展目录：<code>${esc(rep.extension_dir || '')}</code></span>`;

    const installed = (rep.browsers || []).filter((b) => b.installed);
    const list = installed.length ? installed : (rep.browsers || []);
    const anySaved = list.some((b) => !!b.selected);

    grid.innerHTML = list.map((b) => {
      const checked = anySaved ? !!b.selected : (b.installed && b.supports_extension);
      let stClass = 'warn';
      let stText = '未护航';
      if (b.protection_level === 'full') {
        stClass = 'pass';
        stText = '全链路已护航';
      } else if (b.protection_level === 'partial') {
        stClass = 'warn';
        stText = `部分配置档已护航 (${(b.loaded_profiles || []).length}/${b.total_profiles || (b.loaded_profiles || []).length})`;
      } else if (b.protection_level === 'network_only') {
        stClass = b.supports_extension ? 'warn' : 'muted-tag';
        stText = b.supports_extension ? '仅网络层 · 待载入扩展' : '仅支持网络层 PAC';
      } else if (!b.installed) {
        stClass = 'muted-tag';
        stText = '未安装';
      }
      const loadedSummary = summarizeProfileList(b.loaded_profiles);
      const missingSummary = summarizeProfileList(b.missing_profiles);
      const profLines = [];
      if (loadedSummary) {
        profLines.push(`<div class="nb-card-profs" title="${esc((b.loaded_profiles || []).join('\n'))}">已生效配置档：<code>${esc(loadedSummary)}</code></div>`);
      }
      if (missingSummary && (b.protection_level === 'partial' || !b.extension_enabled)) {
        profLines.push(`<div class="nb-card-profs" title="${esc((b.missing_profiles || []).join('\n'))}">待加载配置档：<code>${esc(missingSummary)}</code></div>`);
      }
      const profs = profLines.join('');
      const guideBtnLabel = b.protection_level === 'full' ? '查看扩展目录' : (b.protection_level === 'partial' ? '补载其余 Space / 配置档' : '打开目录并引导载入');
      const guideBtn = b.installed && b.supports_extension
        ? `<button type="button" class="btn btn-secondary btn-sm" data-nb-guide="${esc(b.id)}">${guideBtnLabel}</button>`
        : `<span class="muted" style="font-size:11.5px;">${b.supports_extension ? '请先安装此浏览器' : '不支持 Chromium 扩展'}</span>`;

      return `
        <div class="nb-card ${checked ? 'is-selected' : ''} ${b.installed ? '' : 'is-absent'}" data-nb-card="${esc(b.id)}">
          <div class="nb-card-top">
            <label class="nb-check-label">
              <input type="checkbox" class="nb-browser-cb" value="${esc(b.id)}" ${checked ? 'checked' : ''} ${b.installed ? '' : 'disabled'} />
              <strong>${esc(b.name)}</strong>
            </label>
            <span class="status-tag ${stClass}">${esc(stText)}</span>
          </div>
          <div class="nb-card-note">${esc(b.note || '')}</div>
          ${profs}
          <div class="nb-card-foot">${guideBtn}</div>
        </div>`;
    }).join('');
  }

  async function loadNativeBrowsers() {
    try { renderNativeBrowsers(await api('/api/native-browsers')); } catch (e) { toast('读取原生浏览器状态失败: ' + e.message); }
  }

  async function refreshAll() {
    try { renderStatus(await api('/api/status')); } catch (e) { toast('连接守护进程失败: ' + e.message); }
  }

  async function loadAccounts(refresh = false) {
    try { renderAccounts(await api(`/api/accounts${refresh ? '?refresh=1' : ''}`)); } catch (e) { toast('加载账号列表失败: ' + e.message); }
  }

  async function loadDoctor() {
    try { renderDoctor(await api('/api/doctor')); } catch (e) { toast(e.message); }
  }

  async function loadLeaks() {
    try {
      const d = await api('/api/leaks?limit=150');
      const hits = d.host_direct_sockets || d.direct_sockets || [];
      const banner = $('host-sockets-banner');
      banner.classList.remove('hidden');
      if (hits.length === 0) {
        banner.className = 'host-sockets-banner';
        banner.innerHTML = '<span>lsof 扫描未发现宿主机进程直连 Anthropic CIDR</span>';
      } else {
        banner.className = 'host-sockets-banner danger';
        const hitList = hits.map((h) => `${h.process || h.command || '未知进程'}(PID ${h.pid}) -> ${h.dest_ip || ''}`).join(', ');
        banner.innerHTML = `
          <span>告警：发现 ${hits.length} 个直连 Anthropic Socket（${esc(hitList)}）</span>
          <div class="btn-row">
            <button type="button" class="btn btn-secondary btn-sm" id="btn-leak-enable-pac">开启浏览器 PAC</button>
            <button type="button" class="btn btn-primary btn-sm" id="btn-leak-rescan">重新扫描</button>
          </div>`;
      }
      const events = d.events || [];
      $('leaks-tbody').innerHTML = events.length === 0
        ? '<tr><td colspan="5" class="muted">暂无泄漏或阻断记录</td></tr>'
        : events.map((ev) => `<tr><td class="mono">${esc(fmtTime(ev.timestamp))}</td><td><span class="status-tag warn">${esc(ev.kind)}</span></td><td class="mono">${esc(ev.host)}${ev.process ? ' · ' + esc(ev.process) : ''}</td><td class="mono">${esc(ev.action)}</td><td>${esc(ev.reason)}</td></tr>`).join('');
    } catch (e) { toast(e.message, 'err'); }
  }

  async function loadCaptures() {
    try {
      const d = await api('/api/capture?limit=150');
      const items = d.records || d.captures || [];
      $('capture-tbody').innerHTML = items.length === 0
        ? '<tr><td colspan="8" class="muted">暂无抓包记录，可在右上角开启脱敏抓包</td></tr>'
        : items.map((c) => `
          <tr class="clickable" data-cap-id="${esc(c.id)}">
            <td class="mono">${esc(fmtTime(c.timestamp))}</td>
            <td class="mono">${esc(c.protocol)} ${esc(c.method || '')}</td>
            <td class="mono">${esc(c.host)}${c.path ? esc(c.path) : ''}</td>
            <td>${esc(c.category)}</td>
            <td><span class="status-tag ${c.action === 'proxy' ? 'pass' : 'fail'}">${esc(c.action)}</span></td>
            <td class="mono">${esc(c.egress_ip || '—')}</td>
            <td class="mono">${c.status_code || '—'} · ${c.duration_ms || 0}ms</td>
            <td class="mono">${fmtBytes(c.bytes_out)}↑ / ${fmtBytes(c.bytes_in)}↓</td>
          </tr>`).join('');
    } catch (e) { toast(e.message, 'err'); }
  }

  function checkBrowserFingerprint() {
    const opts = Intl.DateTimeFormat().resolvedOptions();
    const tz = opts.timeZone || '未知';
    const intlLocale = opts.locale || '未知';
    const lang = navigator.language || '未知';
    const langsList = navigator.languages || [];
    const langs = langsList.join(', ') || lang;
    const hasCNLang = langsList.some((x) => String(x).toLowerCase().startsWith('zh')) || String(lang).toLowerCase().startsWith('zh');
    const isNonCNIntl = !String(intlLocale).toLowerCase().startsWith('zh');
    const dnt = navigator.doNotTrack === '1' ? 'on' : 'off';
    const gpc = navigator.globalPrivacyControl ? 'on' : 'off';
    const isUS = tz.startsWith('America/') && lang.toLowerCase().startsWith('en') && !hasCNLang && isNonCNIntl;
    $('browser-fp-grid').innerHTML = [
      ['浏览器时区', tz, tz.startsWith('America/')],
      ['UTC 偏移 (分钟)', String(-new Date().getTimezoneOffset()), -new Date().getTimezoneOffset() !== 480],
      ['首选语言', lang, lang.toLowerCase().startsWith('en')],
      ['Accept-Languages', langs, !hasCNLang],
      ['Intl Locale', intlLocale, isNonCNIntl],
      ['DNT / GPC 隐私信号', `DNT: ${dnt}, GPC: ${gpc}`, dnt === 'on' && gpc === 'on'],
      ['当前面板来源', location.origin, true],
      ['隔离建议', isUS ? '已处于隔离低风险环境' : '请点右上「隔离浏览器实测」开 fuck-claude.app 验证', isUS],
    ].map(([k, v, ok]) => `<div class="fp-item ${ok ? 'ok' : 'warn'}"><div class="fp-k">${esc(k)}</div><div class="fp-v">${esc(v)}</div></div>`).join('');
  }

  // --- Wire Events ---
  hydrateIcons();

  document.querySelectorAll('.nav-item[data-tab]').forEach((btn) => {
    btn.addEventListener('click', () => switchTab(btn.dataset.tab));
  });
  $('net-subtabs')?.addEventListener('click', (ev) => {
    const btn = ev.target.closest('button[data-net-sub]');
    if (btn) switchNetSub(btn.dataset.netSub);
  });

  // Bottom-Left Command Sheet
  const closeCommandSheet = () => {
    $('sidebar-command-sheet').classList.add('hidden');
    $('account-dock-btn').setAttribute('aria-expanded', 'false');
  };
  $('account-dock-btn').addEventListener('click', (ev) => {
    ev.stopPropagation();
    const sheet = $('sidebar-command-sheet');
    const opening = sheet.classList.contains('hidden');
    sheet.classList.toggle('hidden', !opening);
    $('account-dock-btn').setAttribute('aria-expanded', opening ? 'true' : 'false');
  });
  document.addEventListener('click', (ev) => {
    if (!$('sidebar-command-sheet').contains(ev.target) && !$('account-dock-btn').contains(ev.target)) {
      closeCommandSheet();
    }
  });
  $('quick-theme-seg').addEventListener('click', (ev) => {
    const btn = ev.target.closest('button[data-qmode]');
    if (!btn) return;
    appearanceState.theme_mode = btn.dataset.qmode;
    editingSidePinned = false;
    saveAppearanceDebounced();
  });
  $('pop-go-appearance').addEventListener('click', () => { closeCommandSheet(); switchTab('appearance'); });
  $('pop-go-remote').addEventListener('click', () => { closeCommandSheet(); switchTab('remote'); });
  $('pop-go-settings').addEventListener('click', () => { closeCommandSheet(); switchTab('network'); });
  $('pop-go-storage').addEventListener('click', () => { closeCommandSheet(); switchTab('storage'); });

  bindAvatarPicker('id-avatar-picker', 'id-edit-avatar');
  bindAvatarPicker('det-avatar-picker', 'det-edit-avatar');
  $('pop-edit-identity')?.addEventListener('click', async () => {
    closeCommandSheet();
    const profName = statusData?.active_profile || 'default';
    let activeAcct = (accountsData?.accounts || []).find((a) => a.active || a.name === profName);
    if (!activeAcct) {
      try {
        const list = await api('/api/accounts');
        accountsData = list;
        activeAcct = (list.accounts || []).find((a) => a.active || a.name === profName);
      } catch {}
    }
    const curDisplay = activeAcct?.display_name ?? statusData?.active_display_name ?? '';
    const curAvatar = activeAcct?.avatar_id || statusData?.active_avatar_id || '';
    $('id-modal-sub').textContent = `作用于当前活跃账号档案 @${profName} 及左下角身份标识`;
    $('id-edit-display').value = curDisplay;
    renderAvatarPicker('id-avatar-picker', 'id-edit-avatar', resolveAvatarID({ name: profName, avatar_id: curAvatar }));
    $('identity-modal').classList.remove('hidden');
    $('id-edit-display').focus();
  });
  const closeIdentityModal = () => $('identity-modal')?.classList.add('hidden');
  $('id-modal-close')?.addEventListener('click', closeIdentityModal);
  $('id-btn-cancel')?.addEventListener('click', closeIdentityModal);
  $('identity-modal')?.addEventListener('click', (ev) => {
    if (ev.target === $('identity-modal')) closeIdentityModal();
  });
  $('identity-form')?.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    const profName = statusData?.active_profile || 'default';
    const payload = {
      name: profName,
      display_name: $('id-edit-display').value.trim(),
      avatar_id: $('id-edit-avatar').value,
    };
    try {
      await api('/api/accounts/update', { method: 'POST', body: JSON.stringify(payload) });
      closeIdentityModal();
      await Promise.all([refreshAll(), loadAccounts(false)]);
      toast('已更新显示昵称与 3D 动物头像', 'ok');
    } catch (e) { toast('保存失败: ' + e.message, 'err'); }
  });

  const toggleWorkbenchLock = async (locked) => {
    try {
      await api('/api/workbench/lock', { method: 'POST', body: JSON.stringify({ locked }) });
      await refreshAll();
    } catch (e) { toast(e.message); }
  };
  $('pop-lock-ui').addEventListener('click', () => { closeCommandSheet(); toggleWorkbenchLock(true); });
  $('btn-unlock-ui').addEventListener('click', () => toggleWorkbenchLock(false));
  $('pop-stop-active-desktop').addEventListener('click', async () => {
    closeCommandSheet();
    try {
      const r = await api('/api/accounts/stop-desktop', { method: 'POST', body: '{}' });
      toast(r.stopped > 0 ? '已停止当前账号 Claude Desktop' : '当前账号未运行 Desktop');
      loadAccounts(false);
    } catch (e) { toast(e.message); }
  });

  // Top Telemetry Rail Actions
  $('btn-recheck').addEventListener('click', () => withBtnBusy($('btn-recheck'), '探测中…', async () => {
    const res = await api('/api/check', { method: 'POST', body: '{}' });
    await refreshAll();
    const r = res.result || {};
    if (r.ok) {
      toast(`出口校验通过：${r.egress_ip} (${r.country || '—'}) · ${r.latency_ms || 0}ms · ${r.source || 'trace'}`, 'ok');
    } else {
      toast(`出口校验失败（门禁已锁定）：${r.failure_reason || '上游不可达'}`, 'err');
    }
  }));

  $('btn-lock-toggle').addEventListener('click', () => {
    const locked = !!(statusData && statusData.gate && statusData.gate.manual_lock);
    return withBtnBusy($('btn-lock-toggle'), locked ? '恢复中…' : '熔断中…', async () => {
      const r = await api('/api/lock', { method: 'POST', body: JSON.stringify({ locked: !locked, reason: locked ? '' : '控制台手动紧急熔断' }) });
      await refreshAll();
      if (!locked) {
        toast(`已触发紧急熔断：切断 ${r.closed_conns || 0} 条活跃连接，已阻止 Claude 直连`, 'warn');
      } else {
        const openNow = !!(r.gate && r.gate.open);
        toast(openNow ? '已解除紧急熔断，静态 IP 出口已恢复放行' : '已解除手动熔断，但出口探测未通过仍锁定', openNow ? 'ok' : 'warn');
      }
    });
  });

  $('btn-launch-desktop').addEventListener('click', () => withBtnBusy($('btn-launch-desktop'), '启动中…', async () => {
    const r = await api('/api/launch/desktop', { method: 'POST', body: '{}' });
    const extra = r.stopped_others && r.stopped_others.length ? '（已关闭原实例防回调串号）' : '';
    toast(`已打开 ${r.profile} Desktop (PID ${r.pid})${extra}`, 'ok');
    loadAccounts(false);
  }));

  $('btn-launch-browser').addEventListener('click', () => withBtnBusy($('btn-launch-browser'), '打开中…', async () => {
    const r = await api('/api/launch/browser', { method: 'POST', body: '{}' });
    const bName = (r.browser || '').split('/').pop() || 'Browser';
    const pidHint = r.pid ? ` · PID ${r.pid}` : '';
    toast(`已为档案 ${r.profile || 'default'} 打开独立隔离浏览器（${bName}${pidHint}），Cookie 与时区完全隔离`, 'ok');
  }));

  // Inline PAC Quick Enable Banner on Accounts Tab
  $('btn-enable-pac-inline')?.addEventListener('click', () => withBtnBusy($('btn-enable-pac-inline'), '开启中…', async () => {
    const res = await api('/api/browser-pac', { method: 'POST', body: JSON.stringify({ enabled: true }) });
    const pac = res.status || res;
    renderPACStatus(pac);
    await refreshAll();
    if (pac.matches_peaceclaude) {
      toast('已为系统浏览器开启 PeaceClaude PAC（仅分流 Claude 与 Google 登录链路）', 'ok');
    } else {
      toast('PAC 未能绑定到系统网络：' + (pac.note || '请检查系统网络设置权限'), 'err');
    }
  }));

  // Accounts Actions
  $('btn-refresh-accounts').addEventListener('click', async () => {
    toast('正在刷新全部账号额度…');
    await loadAccounts(true);
    toast('额度已更新');
  });
  $('btn-converge-desktops').addEventListener('click', async () => {
    try {
      const r = await api('/api/launch/desktop', { method: 'POST', body: JSON.stringify({ keep_others: false }) });
      toast(`已保留当前使用账号 ${r.profile} (PID ${r.pid})，其余干扰窗口已关闭`);
      await loadAccounts(false);
    } catch (e) { toast(e.message); }
  });

  function todayLocalYMD() {
    const d = new Date();
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
  }
  function suggestNextAccountName() {
    const existing = new Set(((accountsData && accountsData.accounts) || []).map((a) => a.name));
    let n = Math.max(2, existing.size + 1);
    while (existing.has(`account-${n}`)) n++;
    return `account-${n}`;
  }

  $('btn-open-create-account').addEventListener('click', () => {
    $('create-name').value = suggestNextAccountName();
    $('create-display').value = '';
    $('create-note').value = '';
    $('create-plan').value = 'Pro';
    $('create-reg').value = todayLocalYMD();
    $('create-use-now').checked = true;
    $('account-create-modal').classList.remove('hidden');
    $('create-display').focus();
  });
  $('create-btn-close').addEventListener('click', () => $('account-create-modal').classList.add('hidden'));
  $('create-btn-cancel').addEventListener('click', () => $('account-create-modal').classList.add('hidden'));
  $('form-create-account').addEventListener('submit', async (ev) => {
    ev.preventDefault();
    const payload = {
      name: $('create-name').value.trim(),
      display_name: $('create-display').value.trim(),
      note: $('create-note').value.trim(),
      manual_plan: $('create-plan').value,
      manual_registered_at: $('create-reg').value.trim(),
      use_now: $('create-use-now').checked,
    };
    try {
      await api('/api/profiles/create', { method: 'POST', body: JSON.stringify(payload) });
      $('form-create-account').reset();
      $('create-use-now').checked = true;
      $('account-create-modal').classList.add('hidden');
      await refreshAll();
      await loadAccounts(false);
      toast('已创建隔离账号: ' + payload.name);
    } catch (e) { toast('创建失败: ' + e.message); }
  });

  // Delegated Account Card & Roster Events
  $('tab-accounts').addEventListener('click', async (ev) => {
    const secBtn = ev.target.closest('[data-acc-security]');
    if (secBtn) {
      switchTab('security');
      return;
    }
    const deskBtn = ev.target.closest('[data-acc-desktop]');
    const stopBtn = ev.target.closest('[data-acc-stop]');
    const browBtn = ev.target.closest('[data-acc-browser]');
    const detBtn = ev.target.closest('[data-acc-detail]');
    const cliBtn = ev.target.closest('[data-acc-cli]');
    const useBtn = ev.target.closest('[data-acc-use]');
    const delBtn = ev.target.closest('[data-acc-delete]');
    if (!deskBtn && !stopBtn && !browBtn && !detBtn && !cliBtn && !useBtn && !delBtn) return;
    try {
      if (deskBtn) {
        const name = deskBtn.dataset.accDesktop;
        const r = await api('/api/launch/desktop', { method: 'POST', body: JSON.stringify({ profile: name, set_active: true }) });
        const sw = r.stopped_others && r.stopped_others.length ? '（已关闭旧实例以防回调串号）' : '';
        toast(`已切换至 ${name} Desktop (PID ${r.pid})${sw}`);
        await refreshAll();
        await loadAccounts(false);
      } else if (stopBtn) {
        const name = stopBtn.dataset.accStop;
        await api('/api/accounts/stop-desktop', { method: 'POST', body: JSON.stringify({ name }) });
        toast(`已停止 ${name} Desktop`);
        await loadAccounts(false);
      } else if (browBtn) {
        const r = await api('/api/launch/browser', { method: 'POST', body: JSON.stringify({ profile: browBtn.dataset.accBrowser, url: 'https://claude.ai/login' }) });
        const bName = (r.browser || '').split('/').pop() || 'Browser';
        toast(`已打开 ${browBtn.dataset.accBrowser} 隔离浏览器（${bName}${r.pid ? ' · PID ' + r.pid : ''}）`, 'ok');
      } else if (detBtn) {
        await openAccountDetail(detBtn.dataset.accDetail, false);
      } else if (cliBtn) {
        const cmd = `peaceclaude cli --profile ${cliBtn.dataset.accCli}`;
        try { await navigator.clipboard.writeText(cmd); toast('已复制: ' + cmd); } catch { toast(cmd); }
      } else if (useBtn) {
        const name = useBtn.dataset.accUse;
        await api('/api/profiles/use', { method: 'POST', body: JSON.stringify({ name }) });
        await refreshAll();
        await loadAccounts(false);
        toast('已设为当前使用: ' + name, 'ok');
      } else if (delBtn) {
        const name = delBtn.dataset.accDelete;
        if (pendingConfirm.listDel !== name) {
          document.querySelectorAll('[data-acc-delete].confirming').forEach((b) => {
            b.classList.remove('confirming');
            b.textContent = '删除';
          });
          pendingConfirm.listDel = name;
          delBtn.classList.add('confirming');
          delBtn.textContent = '确认删除?';
          clearTimeout(pendingConfirm.listDelTimer);
          pendingConfirm.listDelTimer = setTimeout(() => {
            if (pendingConfirm.listDel === name) pendingConfirm.listDel = null;
            document.querySelectorAll('[data-acc-delete]').forEach((b) => {
              if (b.dataset.accDelete === name) {
                b.classList.remove('confirming');
                b.textContent = '删除';
              }
            });
          }, 4000);
          return;
        }
        clearTimeout(pendingConfirm.listDelTimer);
        pendingConfirm.listDel = null;
        delBtn.disabled = true;
        await api('/api/accounts/delete', { method: 'POST', body: JSON.stringify({ name }) });
        await Promise.all([refreshAll(), loadAccounts(false)]);
        toast('已删除账号: ' + name, 'ok');
      }
    } catch (e) { toast(e.message); }
  });

  // Detail Modal Events
  $('det-btn-close').addEventListener('click', () => $('account-detail-modal').classList.add('hidden'));
  $('account-detail-modal').addEventListener('click', (ev) => { if (ev.target === $('account-detail-modal')) $('account-detail-modal').classList.add('hidden'); });
  $('account-create-modal').addEventListener('click', (ev) => { if (ev.target === $('account-create-modal')) $('account-create-modal').classList.add('hidden'); });

  document.addEventListener('keydown', (ev) => {
    const tag = document.activeElement?.tagName;
    const typing = tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT';
    if (ev.key === 'Escape') {
      closeCommandSheet();
      $('account-detail-modal').classList.add('hidden');
      $('account-create-modal').classList.add('hidden');
      return;
    }
    if (!typing && (ev.metaKey || ev.ctrlKey) && ev.key.toLowerCase() === 'l') {
      ev.preventDefault();
      toggleWorkbenchLock(!(statusData && statusData.ui_locked));
    }
    if (!typing && (ev.metaKey || ev.ctrlKey) && ev.key === ',') {
      ev.preventDefault();
      switchTab('network');
    }
  });

  $('det-btn-refresh-usage').addEventListener('click', async () => {
    if (!currentDetailName) return;
    await openAccountDetail(currentDetailName, true);
    loadAccounts(false);
  });
  $('det-btn-desktop').addEventListener('click', async () => {
    if (!currentDetailName) return;
    try {
      const r = await api('/api/launch/desktop', { method: 'POST', body: JSON.stringify({ profile: currentDetailName, set_active: true }) });
      toast(`已切换至 ${currentDetailName} Desktop (PID ${r.pid})`);
      await openAccountDetail(currentDetailName, false);
      await loadAccounts(false);
      await refreshAll();
    } catch (e) { toast(e.message); }
  });
  $('det-btn-stop-desktop').addEventListener('click', async () => {
    if (!currentDetailName) return;
    await api('/api/accounts/stop-desktop', { method: 'POST', body: JSON.stringify({ name: currentDetailName }) });
    await openAccountDetail(currentDetailName, false);
    await loadAccounts(false);
  });
  $('det-btn-browser').addEventListener('click', () => {
    if (!currentDetailName) return;
    return withBtnBusy($('det-btn-browser'), '打开中…', async () => {
      const r = await api('/api/launch/browser', { method: 'POST', body: JSON.stringify({ profile: currentDetailName, url: 'https://claude.ai/login' }) });
      const bName = (r.browser || '').split('/').pop() || 'Browser';
      toast(`已打开 ${currentDetailName} 隔离浏览器（${bName}${r.pid ? ' · PID ' + r.pid : ''}）`, 'ok');
    });
  });
  $('det-btn-copy-cli').addEventListener('click', async () => {
    if (!currentDetailName) return;
    const cmd = `peaceclaude cli --profile ${currentDetailName}`;
    try { await navigator.clipboard.writeText(cmd); toast('已复制: ' + cmd); } catch { toast(cmd); }
  });
  $('det-sec-badge')?.addEventListener('click', () => {
    $('account-detail-modal').classList.add('hidden');
    switchTab('security');
  });
  $('det-btn-use').addEventListener('click', async () => {
    if (!currentDetailName) return;
    await api('/api/profiles/use', { method: 'POST', body: JSON.stringify({ name: currentDetailName }) });
    await openAccountDetail(currentDetailName, false);
    await refreshAll();
    await loadAccounts(false);
    toast('已设为当前使用: ' + currentDetailName, 'ok');
  });
  $('det-edit-form').addEventListener('submit', async (ev) => {
    ev.preventDefault();
    if (!currentDetailName) return;
    const payload = {
      name: currentDetailName,
      display_name: $('det-edit-display').value.trim(),
      avatar_id: $('det-edit-avatar')?.value || '',
      note: $('det-edit-note').value,
      manual_plan: $('det-edit-plan').value,
      manual_registered_at: $('det-edit-reg').value,
      timezone: $('det-edit-tz').value,
      language: $('det-edit-lang').value,
    };
    try {
      const updated = await api('/api/accounts/update', { method: 'POST', body: JSON.stringify(payload) });
      renderAccountDetail(updated);
      await Promise.all([refreshAll(), loadAccounts(false)]);
      toast('已保存账号信息与头像', 'ok');
    } catch (e) { toast('保存失败: ' + e.message, 'err'); }
  });
  $('det-btn-clean-dry').addEventListener('click', async () => {
    if (!currentDetailName) return;
    const rep = await api('/api/profiles/clean', { method: 'POST', body: JSON.stringify({ name: currentDetailName, dry_run: true }) });
    $('det-clean-output').textContent = JSON.stringify(rep, null, 2);
  });
  $('det-btn-clean-real').addEventListener('click', async () => {
    if (!currentDetailName) return;
    if (pendingConfirm.clean !== currentDetailName) {
      pendingConfirm.clean = currentDetailName;
      $('det-btn-clean-real').textContent = '确认清理并重生成指纹?';
      setTimeout(() => { pendingConfirm.clean = null; $('det-btn-clean-real').textContent = '清理指纹并备份'; }, 4000);
      return;
    }
    pendingConfirm.clean = null;
    $('det-btn-clean-real').textContent = '清理指纹并备份';
    const rep = await api('/api/profiles/clean', { method: 'POST', body: JSON.stringify({ name: currentDetailName, dry_run: false }) });
    $('det-clean-output').textContent = JSON.stringify(rep, null, 2);
    await openAccountDetail(currentDetailName, false);
    await loadAccounts(false);
    toast('已备份并重生成设备 UUID');
  });
  $('det-btn-delete').addEventListener('click', async () => {
    if (!currentDetailName) return;
    if (pendingConfirm.del !== currentDetailName) {
      pendingConfirm.del = currentDetailName;
      $('det-btn-delete').textContent = '确认永久删除?';
      setTimeout(() => { pendingConfirm.del = null; $('det-btn-delete').textContent = '删除账号'; }, 4000);
      return;
    }
    pendingConfirm.del = null;
    $('det-btn-delete').textContent = '删除账号';
    await api('/api/accounts/delete', { method: 'POST', body: JSON.stringify({ name: currentDetailName }) });
    $('account-detail-modal').classList.add('hidden');
    await Promise.all([refreshAll(), loadAccounts(false)]);
    toast('已删除账号', 'ok');
  });

  async function runDoctorFix(payload) {
    const res = await fetch('/api/doctor/fix', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload || {}),
    });
    const text = await res.text();
    let data = {};
    try { data = text ? JSON.parse(text) : {}; } catch { data = { raw: text }; }
    if (data.doctor) renderDoctor(data.doctor);
    await Promise.all([loadDoctor(), refreshAll(), loadAccounts(false)]);
    const acts = data.actions || [];
    const errs = data.errors || (data.error ? [data.error] : []);
    if (errs.length > 0 && acts.length === 0) {
      toast('修复未完成：' + errs.join('；'), 'err');
      return;
    }
    if (errs.length > 0) {
      toast(`${acts.join('；')}（部分仍需处理：${errs.join('；')}）`, 'warn');
      return;
    }
    toast(acts.length > 0 ? acts.join('；') : '当前无需要自动修复的风险项', acts.length > 0 ? 'ok' : 'warn');
  }

  // Security / Leaks / Captures
  $('btn-run-doctor').addEventListener('click', () => withBtnBusy($('btn-run-doctor'), '检测中…', async () => {
    await loadDoctor();
    toast('全量 11 项安全检测已完成', 'ok');
  }));
  $('btn-fix-all-doctor')?.addEventListener('click', () => withBtnBusy($('btn-fix-all-doctor'), '修复中…', async () => {
    await runDoctorFix({ fix_all: true });
  }));
  $('btn-install-pf-card')?.addEventListener('click', () => withBtnBusy($('btn-install-pf-card'), '等待授权…', async () => {
    await runDoctorFix({ action: 'install_pf' });
  }));
  $('doctor-checks')?.addEventListener('click', (ev) => {
    const btn = ev.target.closest('button[data-doc-fix-id]');
    if (!btn) return;
    withBtnBusy(btn, '修复中…', async () => {
      await runDoctorFix({ check_id: btn.dataset.docFixId, action: btn.dataset.docFixAct });
    });
  });
  $('host-sockets-banner')?.addEventListener('click', async (ev) => {
    const pacBtn = ev.target.closest('#btn-leak-enable-pac');
    if (pacBtn) {
      return withBtnBusy(pacBtn, '开启中…', async () => {
        const res = await api('/api/browser-pac', { method: 'POST', body: JSON.stringify({ enabled: true }) });
        const pac = res.status || res;
        renderPACStatus(pac);
        await refreshAll();
        await loadLeaks();
        toast(pac.matches_peaceclaude ? '已开启系统浏览器 PAC 接管' : (pac.note || 'PAC 开启未生效'), pac.matches_peaceclaude ? 'ok' : 'err');
      });
    }
    const rescanBtn = ev.target.closest('#btn-leak-rescan');
    if (rescanBtn) {
      return withBtnBusy(rescanBtn, '扫描中…', async () => {
        await Promise.all([loadLeaks(), refreshAll()]);
        toast('已完成宿主机 Socket 重扫', 'ok');
      });
    }
  });
  $('btn-pause-recheck').addEventListener('click', () => withBtnBusy($('btn-pause-recheck'), '重检中…', async () => {
    const res = await api('/api/pause-and-recheck', { method: 'POST', body: '{}' });
    if (res.doctor) renderDoctor(res.doctor);
    await refreshAll();
    toast(`已暂停外网（切断 ${res.closed_conns || 0} 条连接）并完成重检`, 'ok');
  }));
  $('btn-copy-report').addEventListener('click', async () => {
    try {
      const txt = JSON.stringify({ status: statusData, doctor: await api('/api/doctor') }, null, 2);
      await navigator.clipboard.writeText(txt);
      toast('已复制诊断报告', 'ok');
    } catch (e) { toast('复制失败: ' + e.message, 'err'); }
  });
  $('btn-inspect-desktop').addEventListener('click', async () => {
    try {
      const rep = await api('/api/inspect/desktop');
      toast(rep.summary, rep.exposed_count > 0 ? 'warn' : 'ok');
    } catch (e) { toast(e.message, 'err'); }
  });
  $('btn-check-browser-fp').addEventListener('click', () => { checkBrowserFingerprint(); toast('浏览器指纹已刷新', 'ok'); });
  $('btn-go-native-shield')?.addEventListener('click', () => {
    switchTab('network');
    switchNetSub('proxy');
    setTimeout(() => {
      $('native-browsers-card')?.scrollIntoView({ behavior: 'smooth', block: 'center' });
    }, 80);
  });
  $('btn-launch-browser-fc')?.addEventListener('click', () => withBtnBusy($('btn-launch-browser-fc'), '启动中…', async () => {
    const r = await api('/api/launch/browser', { method: 'POST', body: JSON.stringify({ url: 'https://fuck-claude.app/' }) });
    const bName = (r.browser || '').split('/').pop() || 'Browser';
    toast(`已用 ${r.profile || 'default'} 隔离浏览器打开 fuck-claude.app（${bName}${r.pid ? ' · PID ' + r.pid : ''}），请点击 START SCAN 实测`, 'ok');
  }));
  $('btn-run-diagnose').addEventListener('click', () => withBtnBusy($('btn-run-diagnose'), '探测中…', async () => {
    const el = $('diagnose-result');
    el.innerHTML = '<p class="muted">正在经静态 IP 探测上游端点…</p>';
    const rep = await api('/api/diagnose');
    const verdict = rep.classification || rep.verdict || 'unknown';
    const eps = rep.endpoints || rep.probes || [];
    el.innerHTML =
      `<div class="kv-row"><span>分诊结论 (${esc(verdict)})</span><code>${esc(rep.summary || '')}</code></div>` +
      (rep.recommendation ? `<div class="kv-row"><span>处理建议</span><code>${esc(rep.recommendation)}</code></div>` : '') +
      eps.map((p) => {
        const label = p.url || p.endpoint || '';
        const detail = p.detail || (p.error ? `失败: ${p.error}` : `HTTP ${p.status_code || '—'} · ${p.latency_ms || 0}ms`);
        return `<div class="kv-row"><span>${esc(label)}</span><code>${esc(detail)}</code></div>`;
      }).join('');
    toast(`三分诊完成：${verdict}`, verdict === 'healthy' ? 'ok' : 'warn');
  }));
  $('btn-refresh-leaks').addEventListener('click', loadLeaks);
  $('btn-clear-leaks').addEventListener('click', async () => {
    try { await api('/api/leaks/clear', { method: 'POST', body: '{}' }); loadLeaks(); refreshAll(); toast('已清空泄漏记录', 'ok'); } catch (e) { toast(e.message, 'err'); }
  });
  $('toggle-capture').addEventListener('change', async (ev) => {
    try { await api('/api/capture/toggle', { method: 'POST', body: JSON.stringify({ enabled: ev.target.checked }) }); toast(ev.target.checked ? '已开启脱敏抓包' : '已关闭脱敏抓包', 'ok'); } catch (e) { toast(e.message, 'err'); }
  });
  $('btn-refresh-capture').addEventListener('click', loadCaptures);
  $('btn-clear-capture').addEventListener('click', async () => {
    try { await api('/api/capture/clear', { method: 'POST', body: '{}' }); loadCaptures(); refreshAll(); toast('已清空抓包记录', 'ok'); } catch (e) { toast(e.message, 'err'); }
  });
  $('capture-tbody').addEventListener('click', async (ev) => {
    const tr = ev.target.closest('tr[data-cap-id]');
    if (!tr) return;
    try {
      const item = await api('/api/capture?id=' + encodeURIComponent(tr.dataset.capId));
      $('capture-detail-pre').textContent = JSON.stringify(item, null, 2);
      $('capture-detail').classList.remove('hidden');
    } catch (e) { toast('读取抓包详情失败: ' + e.message, 'err'); }
  });
  $('btn-close-cap-detail').addEventListener('click', () => $('capture-detail').classList.add('hidden'));

  // Network & General Strategy Forms
  async function saveConfigPayload(payload, okMsg) {
    try {
      await api('/api/config/update', { method: 'POST', body: JSON.stringify(payload) });
      populateSettings._dirty = false;
      if ($('cfg-upstream-pass')) $('cfg-upstream-pass').value = '';
      await refreshAll();
      toast(okMsg);
    } catch (e) { toast('保存失败: ' + e.message); }
  }
  const parseDomainInputList = (raw) => {
    const seen = new Set();
    const out = [];
    for (const item of String(raw || '').split(/[\s,;，；]+/)) {
      const clean = item.trim().toLowerCase();
      if (!clean || seen.has(clean)) continue;
      seen.add(clean);
      out.push(clean);
    }
    return out;
  };
  const gatherAllSettings = () => ({
    upstream_mode: $('cfg-upstream-mode').value,
    upstream_address: $('cfg-upstream-addr').value.trim(),
    upstream_username: $('cfg-upstream-user').value,
    upstream_password: $('cfg-upstream-pass').value,
    expected_ip: $('cfg-expected-ip').value.trim(),
    allowed_countries: $('cfg-allowed-countries').value.split(',').map((s) => s.trim()).filter(Boolean),
    language: $('cfg-language').value.trim(),
    timezone: $('cfg-timezone').value.trim(),
    single_active_desktop: $('cfg-single-active').checked,
    auto_peace_desktop: $('cfg-auto-peace').checked,
    fail_closed: $('cfg-fail-closed').checked,
    sync_timezone: $('cfg-sync-tz').checked,
    telemetry_shield: $('cfg-telemetry-shield').checked,
    strict_domain_isolation: $('cfg-strict-split').checked,
    skip_probe_during_sse: $('cfg-skip-sse').checked,
    strip_host_anthropic_env: $('cfg-strip-host-env').checked,
    allow_corporate_direct: $('cfg-corp-direct').checked,
    proxy_auth_domains: $('cfg-proxy-auth-domains') ? $('cfg-proxy-auth-domains').checked : false,
    desktop_proxy_mode: $('cfg-desktop-proxy-mode') ? $('cfg-desktop-proxy-mode').value : 'pac',
    corporate_domains: $('cfg-corporate-domains') ? parseDomainInputList($('cfg-corporate-domains').value) : [],
    extra_claude_domains: $('cfg-extra-claude-domains') ? parseDomainInputList($('cfg-extra-claude-domains').value) : [],
    extra_blocked_domains: $('cfg-extra-blocked-domains') ? parseDomainInputList($('cfg-extra-blocked-domains').value) : [],
    bypass_vpn: $('cfg-bypass-vpn') ? $('cfg-bypass-vpn').checked : true,
    vm_proxy_enabled: $('cfg-vm-proxy').checked,
  });
  $('cfg-desktop-proxy-mode')?.addEventListener('change', () => {
    saveConfigPayload(gatherAllSettings(), $('cfg-desktop-proxy-mode').value === 'pac'
      ? '已切换为智能分流 PAC 模式（重启 Claude Desktop 后生效）'
      : '已切换为强制全代理 + 绕过列表模式（重启 Claude Desktop 后生效）');
  });
  $('cfg-proxy-auth-domains')?.addEventListener('change', () => {
    saveConfigPayload(gatherAllSettings(), $('cfg-proxy-auth-domains').checked
      ? '已启用登录辅助域经静态 IP 代理，PAC 已热更新'
      : '已切换为严格仅 Claude 域名走静态 IP（Google 登录与其余网站直连）');
  });
  $('cfg-bypass-vpn')?.addEventListener('change', () => {
    saveConfigPayload(gatherAllSettings(), $('cfg-bypass-vpn').checked
      ? '已开启直连物理网卡绕开 VPN，并立即重测静态出口链路'
      : '已切换为走系统默认路由（不再跳过公司 VPN）');
  });
  $('form-network').addEventListener('input', () => { populateSettings._dirty = true; });
  $('form-general').addEventListener('input', () => { populateSettings._dirty = true; });
  $('form-network').addEventListener('submit', (ev) => { ev.preventDefault(); saveConfigPayload(gatherAllSettings(), '代理配置已保存并热生效'); });
  $('form-general').addEventListener('submit', (ev) => { ev.preventDefault(); saveConfigPayload(gatherAllSettings(), '隔离与切号策略已保存'); });
  $('toggle-shared-rules').addEventListener('change', async (ev) => {
    const st = await api('/api/rules/toggle', { method: 'POST', body: JSON.stringify({ enabled: ev.target.checked }) });
    renderRules(st);
    toast(ev.target.checked ? '已开启跨账号规则共享' : '已关闭全局规则共享');
  });
  $('btn-toggle-pac').addEventListener('click', () => withBtnBusy($('btn-toggle-pac'), pacEnabled ? '关闭中…' : '开启中…', async () => {
    const want = !pacEnabled;
    const res = await api('/api/browser-pac', { method: 'POST', body: JSON.stringify({ enabled: want }) });
    const pac = res.status || res;
    renderPACStatus(pac);
    await refreshAll();
    if (want) {
      if (pac.matches_peaceclaude) {
        toast('已为系统浏览器开启 PeaceClaude PAC（仅 Claude 相关域名走静态 IP）', 'ok');
      } else {
        toast('开启 PAC 后检测未匹配：' + (pac.note || '请检查系统网络代理设置'), 'err');
      }
    } else {
      toast(pac.matches_peaceclaude ? '关闭 PAC 后仍生效，请检查系统网络' : '已关闭系统浏览器 PAC', pac.matches_peaceclaude ? 'warn' : 'ok');
    }
  }));
  $('btn-toggle-autostart')?.addEventListener('click', () => withBtnBusy($('btn-toggle-autostart'), '处理中…', async () => {
    const want = !autostartPolicyEnabled;
    const res = await api('/api/service/autostart', { method: 'POST', body: JSON.stringify({ enabled: want }) });
    if (res.service) renderServiceAutostart(res.service);
    await refreshAll();
    toast(res.message || (want ? '已开启开机自启守护' : '已关闭开机自启守护'), 'ok');
  }));

  async function openNativeBrowserGuide(browserID, showToast = true) {
    lastGuideBrowserID = browserID;
    const res = await api('/api/native-browsers/guide', {
      method: 'POST',
      body: JSON.stringify({ browser_id: browserID }),
    });
    lastGuideExtURL = res.extensions_url || '';
    if (lastGuideExtURL) {
      try { await navigator.clipboard.writeText(lastGuideExtURL); } catch {}
    }
    const box = $('nb-guide-box');
    if (box) {
      $('nb-guide-title').textContent = `${res.browser_name || browserID} 定向护航扩展加载指引（只需一次性拖入，重启不会自动加载）`;
      $('nb-guide-steps').innerHTML = (res.steps || []).map((s) => `<li>${esc(s)}</li>`).join('');
      box.classList.remove('hidden');
    }
    if (showToast) {
      toast(res.message + (lastGuideExtURL ? `（已复制 ${lastGuideExtURL}）` : ''), 'ok');
    }
    return res;
  }

  // Native Browser Multi-Select Protection Events
  $('nb-browser-grid')?.addEventListener('change', (ev) => {
    const cb = ev.target.closest('.nb-browser-cb');
    if (!cb) return;
    cb.closest('.nb-card')?.classList.toggle('is-selected', cb.checked);
  });
  $('nb-browser-grid')?.addEventListener('click', async (ev) => {
    const btn = ev.target.closest('button[data-nb-guide]');
    if (!btn) return;
    const browserID = btn.dataset.nbGuide;
    await withBtnBusy(btn, '打开中…', () => openNativeBrowserGuide(browserID, true));
  });
  $('btn-nb-refresh')?.addEventListener('click', () => withBtnBusy($('btn-nb-refresh'), '检测中…', async () => {
    await loadNativeBrowsers();
    toast('原生浏览器护航状态已刷新', 'ok');
  }));
  const copyExtDirPath = async () => {
    const p = nativeBrowsersData?.extension_dir || '~/.peaceclaude/extensions/native-browser-shield';
    try { await navigator.clipboard.writeText(p); toast('已复制扩展目录: ' + p, 'ok'); } catch { toast(p); }
  };
  $('btn-nb-copy-path')?.addEventListener('click', copyExtDirPath);
  $('btn-nb-copy-path-guide')?.addEventListener('click', copyExtDirPath);
  $('btn-nb-open-finder')?.addEventListener('click', () => withBtnBusy($('btn-nb-open-finder'), '打开中…', async () => {
    const id = lastGuideBrowserID || (nativeBrowsersData?.browsers || []).find((b) => b.installed && b.supports_extension)?.id || 'arc';
    await openNativeBrowserGuide(id, true);
  }));
  $('btn-nb-copy-exturl')?.addEventListener('click', async () => {
    if (!lastGuideExtURL) return;
    try { await navigator.clipboard.writeText(lastGuideExtURL); toast('已复制扩展管理页地址: ' + lastGuideExtURL, 'ok'); } catch { toast(lastGuideExtURL); }
  });
  $('btn-nb-close-guide')?.addEventListener('click', () => $('nb-guide-box')?.classList.add('hidden'));
  $('btn-nb-apply')?.addEventListener('click', () => withBtnBusy($('btn-nb-apply'), '开启中…', async () => {
    const selected = Array.from(document.querySelectorAll('.nb-browser-cb:checked')).map((el) => el.value);
    const res = await api('/api/native-browsers/protect', {
      method: 'POST',
      body: JSON.stringify({ browsers: selected, enable_pac: true }),
    });
    if (res.report) renderNativeBrowsers(res.report);
    await refreshAll();
    const pendingExt = (res.report?.browsers || []).filter(
      (b) => selected.includes(b.id) && b.supports_extension && b.protection_level !== 'full'
    );
    if (pendingExt.length > 0) {
      await openNativeBrowserGuide(pendingExt[0].id, false);
      toast(`PAC 已开启！注意单纯重启浏览器不会自动加载扩展，请按下方指引把目录拖入 ${pendingExt[0].extensions_url || '扩展页'}`, 'warn');
    } else {
      toast('所选原生浏览器已处于全链路护航状态！', 'ok');
    }
  }));

  // Appearance Studio Events
  $('app-theme-mode-seg').addEventListener('click', (ev) => {
    const btn = ev.target.closest('button[data-mode]');
    if (!btn) return;
    appearanceState.theme_mode = btn.dataset.mode;
    editingSidePinned = false;
    saveAppearanceDebounced();
  });
  $('edit-side-seg').addEventListener('click', (ev) => {
    const btn = ev.target.closest('button[data-eside]');
    if (!btn) return;
    editingSide = btn.dataset.eside;
    editingSidePinned = true;
    renderAppearanceUI();
  });
  $('preset-swatch-grid').addEventListener('click', (ev) => {
    const card = ev.target.closest('[data-preset-key]');
    if (!card) return;
    const key = card.dataset.presetKey;
    if (editingSide === 'light') {
      appearanceState.preset_light = key;
      appearanceState.custom_light = null;
    } else {
      appearanceState.preset_dark = key;
      appearanceState.custom_dark = null;
    }
    appearanceState.active_saved_theme = '';
    saveAppearanceDebounced();
  });
  $('btn-reset-side').addEventListener('click', () => {
    if (editingSide === 'light') appearanceState.custom_light = null;
    else appearanceState.custom_dark = null;
    saveAppearanceDebounced();
    toast('已恢复当前方案预设颜色');
  });
  ['accent', 'bg', 'text'].forEach((field) => {
    const updateHex = (val) => {
      if (!/^#[0-9a-f]{6}$/i.test(val.trim())) return;
      const customKey = editingSide === 'light' ? 'custom_light' : 'custom_dark';
      appearanceState[customKey] = { ...(appearanceState[customKey] || {}), [field]: val.toLowerCase() };
      appearanceState.active_saved_theme = '';
      saveAppearanceDebounced();
    };
    $(`pick-side-${field}`).addEventListener('input', (ev) => updateHex(ev.target.value));
    $(`hex-side-${field}`).addEventListener('change', (ev) => updateHex(ev.target.value));
  });
  $('select-ui-font').addEventListener('change', (ev) => { appearanceState.ui_font = ev.target.value; saveAppearanceDebounced(); });
  $('select-code-font').addEventListener('change', (ev) => { appearanceState.code_font = ev.target.value; saveAppearanceDebounced(); });
  $('select-ui-size').addEventListener('change', (ev) => { appearanceState.ui_font_size = Number(ev.target.value); saveAppearanceDebounced(); });
  $('select-code-size').addEventListener('change', (ev) => { appearanceState.code_font_size = Number(ev.target.value); saveAppearanceDebounced(); });

  $('btn-save-appearance')?.addEventListener('click', () => withBtnBusy($('btn-save-appearance'), '保存中…', async () => {
    clearTimeout(saveAppTimer);
    applyAppearance();
    await api('/api/appearance/update', { method: 'POST', body: JSON.stringify(appearanceState) });
    toast('已保存并应用当前主题配置', 'ok');
  }));

  // Remote Pairing Events
  $('remote-mode-seg').addEventListener('click', async (ev) => {
    const btn = ev.target.closest('button[data-rmode]');
    if (!btn) return;
    try {
      const res = await api('/api/remote/relay', { method: 'POST', body: JSON.stringify({ relay_mode: btn.dataset.rmode }) });
      if (res.status) renderRemote(res.status);
    } catch (e) { toast(e.message); }
  });
  $('btn-gen-pair-qr').addEventListener('click', async () => {
    try {
      const res = await api('/api/remote/pair', { method: 'POST', body: '{}' });
      if (res.status) renderRemote(res.status);
      toast('已签发 120 秒有效一次性配对通行证');
    } catch (e) { toast(e.message); }
  });
  $('btn-copy-pair-code').addEventListener('click', async () => {
    const code = remoteData?.active_pair?.code;
    if (!code) return;
    try { await navigator.clipboard.writeText(code); toast('已复制一次性配对码'); } catch { toast(code); }
  });
  $('btn-revoke-remote').addEventListener('click', async () => {
    const res = await api('/api/remote/revoke', { method: 'POST', body: '{}' });
    if (res.status) renderRemote(res.status);
    toast('已撤销所有已授权手机会话');
  });
  $('remote-devices-list').addEventListener('click', async (ev) => {
    const btn = ev.target.closest('[data-revoke-dev]');
    if (!btn) return;
    const res = await api('/api/remote/revoke', { method: 'POST', body: JSON.stringify({ id: btn.dataset.revokeDev }) });
    if (res.status) renderRemote(res.status);
    toast('已断开指定设备');
  });
  $('btn-save-remote').addEventListener('click', async () => {
    try {
      const res = await api('/api/remote/relay', {
        method: 'POST',
        body: JSON.stringify({
          relay_mode: 'lan',
          enabled: $('cfg-remote-enabled').checked,
          mobile_port: Number($('cfg-remote-port').value) || 18900,
        }),
      });
      if (res.status) renderRemote(res.status);
      toast('局域网 / Tailscale 端口配置已更新');
    } catch (e) { toast(e.message); }
  });
  $('btn-test-relay').addEventListener('click', async () => {
    const url = $('cfg-custom-relay-url').value.trim();
    $('remote-relay-test-msg').textContent = '探测中…';
    try {
      const res = await api('/api/remote/relay', { method: 'POST', body: JSON.stringify({ test_only: true, custom_relay_url: url }) });
      $('remote-relay-test-msg').textContent = res.health?.detail || (res.ok ? '连通正常' : '不可达');
    } catch (e) { $('remote-relay-test-msg').textContent = e.message; }
  });
  $('btn-save-custom-relay').addEventListener('click', async () => {
    try {
      const res = await api('/api/remote/relay', {
        method: 'POST',
        body: JSON.stringify({
          relay_mode: 'custom',
          custom_relay_url: $('cfg-custom-relay-url').value.trim(),
          relay_admin_token: $('cfg-custom-relay-token').value,
        }),
      });
      $('cfg-custom-relay-token').value = '';
      if (res.status) renderRemote(res.status);
      toast(res.health?.ok ? '自建中继已保存并探测通过' : '已保存自建中继（探测未通: ' + (res.health?.detail || '') + '）');
    } catch (e) { toast(e.message); }
  });

  // CLI Guide Copy Actions
  $('tab-cli')?.addEventListener('click', async (ev) => {
    const btn = ev.target.closest('button[data-copy-cmd]');
    if (!btn) return;
    const cmd = btn.dataset.copyCmd || '';
    try { await navigator.clipboard.writeText(cmd); toast('已复制命令: ' + cmd, 'ok'); } catch { toast(cmd); }
  });
  $('btn-cli-copy-active')?.addEventListener('click', async () => {
    const active = (statusData && statusData.active_profile) || 'default';
    const cmd = `peaceclaude cli --profile ${active}`;
    try { await navigator.clipboard.writeText(cmd); toast('已复制当前使用账号命令: ' + cmd, 'ok'); } catch { toast(cmd); }
  });

  // Boot & Theme Hydration
  try {
    const cached = localStorage.getItem('peaceclaude_appearance');
    if (cached) applyAppearance(JSON.parse(cached));
  } catch {}
  if (window.matchMedia) {
    window.matchMedia('(prefers-color-scheme: light)').addEventListener('change', () => {
      if (appearanceState.theme_mode === 'system') applyAppearance();
    });
  }

  api('/api/appearance').then((app) => applyAppearance(app)).catch(() => applyAppearance());

  const initialHash = (location.hash || '#accounts').replace(/^#/, '');
  switchTab(initialHash || 'accounts');
  refreshAll();
  setInterval(refreshAll, 5000);
})();
