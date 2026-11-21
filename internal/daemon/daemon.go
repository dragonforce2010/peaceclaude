package daemon

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"peaceclaude/internal/audit"
	"peaceclaude/internal/config"
	"peaceclaude/internal/egress"
	"peaceclaude/internal/host"
	"peaceclaude/internal/launcher"
	"peaceclaude/internal/policy"
	"peaceclaude/internal/profile"
	"peaceclaude/internal/proxy"
	"peaceclaude/internal/remote"
	"peaceclaude/internal/upstream"
)

type Server struct {
	mu          sync.RWMutex
	cfg         *config.Config
	selfBin     string
	startedAt   time.Time
	gate        *egress.Gate
	policy      *policy.Engine
	audit       *audit.Store
	gateway     *proxy.Gateway

	httpLn      net.Listener
	socksLn     net.Listener
	ctrlLn      net.Listener
	httpSrv     *http.Server
	ctrlSrv     *http.Server
	cancel      context.CancelFunc
	doneCh      chan struct{}
	shutOnce    sync.Once

	directMu    sync.RWMutex
	directHits  []host.DirectSocketHit

	takeoverMu           sync.Mutex
	preExistingPIDs      map[string]bool
	lastTakeover         time.Time
	takeoverAttempts     []time.Time
	takeoverConsecFails  int
	autoPeacePausedUntil time.Time
	autoPeaceLockFile    *os.File

	notifyMu    sync.Mutex
	lastNotify  time.Time

	usageMu    sync.RWMutex
	usageCache map[string]profile.UsageSnapshot

	remoteMgr        *remote.Manager
	uiLocked         atomic.Bool
	mobileMu         sync.Mutex
	mobileLn         net.Listener
	mobileSrv        *http.Server
	mobileListenAddr string
}

func New(cfg *config.Config, selfBin string) *Server {
	eng := policy.NewEngine(cfg)
	gate := egress.NewGate(cfg)
	store := audit.NewStore(cfg.Capture.Enabled, cfg.Capture.MaxEntries)
	gw := proxy.NewGateway(cfg, eng, gate, store)

	s := &Server{
		cfg:        cfg,
		selfBin:    selfBin,
		startedAt:  time.Now().UTC(),
		gate:       gate,
		policy:     eng,
		audit:      store,
		gateway:    gw,
		doneCh:     make(chan struct{}),
		usageCache: make(map[string]profile.UsageSnapshot),
		remoteMgr:  remote.NewManager(),
	}

	gate.SetOnStateChange(func(open bool, reason string, res egress.ProbeResult) {
		if !open {
			closed := gw.CloseProtectedConns()
			log.Printf("[egress-gate] LOCKED (fail-closed): %s (closed %d active conns)", reason, closed)
			s.notifyMacOS("PeaceClaude 安全熔断", fmt.Sprintf("已阻断 Claude 出站：%s", reason))
		} else {
			log.Printf("[egress-gate] OPEN: egress=%s (%s · %s · TZ=%s)",
				res.EgressIP, res.Country, res.City, gate.Snapshot().EffectiveTimezone)
			go gw.WarmUpstream(context.Background())
			go s.syncNativeBrowserShield()
			s.notifyMacOS("PeaceClaude 恢复放行", fmt.Sprintf("静态 IP 出口就绪：%s (%s)", res.EgressIP, res.Country))
		}
	})
	return s
}

func (s *Server) syncNativeBrowserShield() {
	cfg := s.configSnapshot()
	snap := s.gate.Snapshot()
	if _, err := launcher.EnsureNativeBrowserShield(&cfg, snap); err != nil {
		log.Printf("[native-shield] sync skipped: %v", err)
	}
}

func (s *Server) configSnapshot() config.Config {
	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()
	return cfg
}

// swapConfigLocked installs an immutable Copy-on-Write Config snapshot and propagates
// it to gateway, policy engine, and egress gate while holding s.mu.
func (s *Server) swapConfigLocked(next config.Config) *config.Config {
	cp := next
	s.cfg = &cp
	s.gateway.UpdateConfig(s.cfg)
	s.gate.UpdateConfig(s.cfg)
	return s.cfg
}

func (s *Server) Gate() *egress.Gate      { return s.gate }
func (s *Server) Gateway() *proxy.Gateway { return s.gateway }
func (s *Server) Audit() *audit.Store     { return s.audit }
func (s *Server) Done() <-chan struct{}   { return s.doneCh }

const usageCacheTTL = 2 * time.Minute

func (s *Server) getProfileUsage(ctx context.Context, cfg *config.Config, prof *profile.Profile, force bool) profile.UsageSnapshot {
	if prof == nil {
		return profile.UsageSnapshot{Status: "error", Source: "none"}
	}
	if !force {
		s.usageMu.RLock()
		cached, ok := s.usageCache[prof.Name]
		s.usageMu.RUnlock()
		if ok && time.Since(cached.FetchedAt) < usageCacheTTL && (cached.Status == "ok" || cached.Status == "history") {
			return cached
		}
	}
	gateOpen, _ := s.gate.AllowTraffic()
	dialer := upstream.FromConfig(cfg.Upstream)
	snap := profile.FetchUsage(ctx, cfg, prof, gateOpen, dialer)
	s.usageMu.Lock()
	s.usageCache[prof.Name] = snap
	s.usageMu.Unlock()
	return snap
}

func (s *Server) Start(ctx context.Context) error {
	if err := config.EnsureDirs(); err != nil {
		return err
	}
	_, _ = profile.Ensure(s.cfg, s.cfg.ActiveProfile, "")

	bindHost := s.cfg.Listen.BindAddr
	if s.cfg.Listen.VMProxyEnabled && s.cfg.Listen.VMProxyBind != "" {
		// Keep control plane strictly on loopback; proxy ports can bind 0.0.0.0 with strict subnet ACL.
		bindHost = "0.0.0.0"
	}

	var err error
	s.httpLn, err = net.Listen("tcp", fmt.Sprintf("%s:%d", bindHost, s.cfg.Listen.HTTPPort))
	if err != nil {
		return fmt.Errorf("listen HTTP proxy: %w", err)
	}
	s.socksLn, err = net.Listen("tcp", fmt.Sprintf("%s:%d", bindHost, s.cfg.Listen.SOCKS5Port))
	if err != nil {
		_ = s.httpLn.Close()
		return fmt.Errorf("listen SOCKS5 proxy: %w", err)
	}
	s.ctrlLn, err = net.Listen("tcp", fmt.Sprintf("%s:%d", s.cfg.Listen.BindAddr, s.cfg.Listen.ControlPort))
	if err != nil {
		_ = s.httpLn.Close()
		_ = s.socksLn.Close()
		return fmt.Errorf("listen control API: %w", err)
	}

	_ = os.WriteFile(config.DaemonPIDPath(), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600)

	s.httpSrv = &http.Server{
		Handler:           s.gateway,
		ReadHeaderTimeout: 15 * time.Second,
	}
	s.ctrlSrv = &http.Server{
		Handler:           s.controlHandler(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	go func() {
		if err := s.httpSrv.Serve(s.httpLn); err != nil && err != http.ErrServerClosed {
			log.Printf("[http-proxy] stopped: %v", err)
		}
	}()
	go func() {
		if err := s.gateway.ServeSOCKS5(s.socksLn); err != nil {
			log.Printf("[socks5-proxy] stopped: %v", err)
		}
	}()
	go func() {
		if err := s.ctrlSrv.Serve(s.ctrlLn); err != nil && err != http.ErrServerClosed {
			log.Printf("[control-api] stopped: %v", err)
		}
	}()

	// Snapshot any pre-existing exposed Claude.app PIDs at daemon start so we never
	// abruptly kill a window the user was already reading before starting PeaceClaude.
	// Any *newly* opened unisolated Claude.app will be auto-taken over cleanly.
	s.snapshotPreExistingDesktop()

	log.Printf("[peaceclaude] daemon listening | HTTP=%s | SOCKS5=%s | UI=http://%s (running initial egress probe...)",
		s.httpLn.Addr(), s.socksLn.Addr(), s.ctrlLn.Addr())

	// Run initial egress probe asynchronously so Control API (/api/health) becomes
	// ready in <100ms and LaunchAgent never times out or spawns duplicate daemons.
	go func() {
		initCtx, initCancel := context.WithTimeout(runCtx, 25*time.Second)
		res := s.gate.Check(initCtx, false)
		initCancel()
		log.Printf("[peaceclaude] initial egress probe done | egress_ok=%t ip=%s", res.OK, res.EgressIP)
		s.syncNativeBrowserShield()
		s.probeLoop(runCtx)
	}()
	go s.hostLeakLoop(runCtx)
	go s.desktopGuardLoop(runCtx)
	go s.retentionLoop(runCtx)
	if err := s.syncMobileListener(s.cfg); err != nil {
		log.Printf("[remote] failed to start mobile listener: %v", err)
	}
	if os.Getenv("PEACECLAUDE_HOME") == "" && os.Getenv("PEACECLAUDE_NO_SYS_PAC") == "" && s.cfg.ShouldAutoEnableBrowserPAC() {
		go s.ensureDefaultBrowserPAC()
	}
	return nil
}

func (s *Server) ensureDefaultBrowserPAC() {
	cfg := s.configSnapshot()
	if !cfg.ShouldAutoEnableBrowserPAC() {
		return
	}
	st := host.InspectBrowserPAC(&cfg)
	if st.MatchesPeace {
		return
	}
	nextSt, _, err := host.SetSystemBrowserPAC(&cfg, true, false)
	if err != nil {
		log.Printf("[browser-pac] auto-enable skipped: %v", err)
		return
	}
	log.Printf("[browser-pac] auto-enabled default PAC (%s) on services=%v", nextSt.PACURL, nextSt.ActiveServices)
}

func (s *Server) retentionLoop(ctx context.Context) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			leaks, caps := s.audit.PruneOlderThan(audit.DefaultRetention)
			if leaks > 0 || caps > 0 {
				log.Printf("[audit-retention] pruned entries older than %d days: leaks=%d captures=%d",
					audit.DefaultRetentionDays, leaks, caps)
			}
		}
	}
}

func (s *Server) ControlAddr() string {
	if s.ctrlLn != nil {
		return s.ctrlLn.Addr().String()
	}
	cfg := s.configSnapshot()
	return cfg.ControlAddr()
}

func (s *Server) HTTPProxyAddr() string {
	if s.httpLn != nil {
		return s.httpLn.Addr().String()
	}
	cfg := s.configSnapshot()
	return cfg.HTTPProxyAddr()
}

func (s *Server) SOCKS5ProxyAddr() string {
	if s.socksLn != nil {
		return s.socksLn.Addr().String()
	}
	cfg := s.configSnapshot()
	return cfg.SOCKS5ProxyAddr()
}

func (s *Server) Shutdown(ctx context.Context) {
	s.shutOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		s.gateway.CloseProtectedConns()
		if s.httpSrv != nil {
			_ = s.httpSrv.Shutdown(ctx)
		}
		if s.socksLn != nil {
			_ = s.socksLn.Close()
		}
		if s.ctrlSrv != nil {
			_ = s.ctrlSrv.Shutdown(ctx)
		}
		s.stopMobileListener(ctx)
		s.releaseAutoPeaceLock()
		_ = os.Remove(config.DaemonPIDPath())
		close(s.doneCh)
	})
}

func (s *Server) probeLoop(ctx context.Context) {
	for {
		s.mu.RLock()
		goodInt := time.Duration(s.cfg.Egress.ProbeIntervalSec) * time.Second
		badInt := time.Duration(s.cfg.Egress.BadIntervalSec) * time.Second
		s.mu.RUnlock()
		if goodInt < 5*time.Second {
			goodInt = 5 * time.Second
		}
		if badInt < 2*time.Second {
			badInt = 2 * time.Second
		}

		wait := goodInt
		if ok, _ := s.gate.AllowTraffic(); !ok || s.gate.Snapshot().ConsecutiveFails > 0 {
			wait = badInt
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			prevSnap := s.gate.Snapshot()
			pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			s.gate.Check(pctx, true)
			cancel()
			curSnap := s.gate.Snapshot()
			if curSnap.EffectiveTimezone != prevSnap.EffectiveTimezone || curSnap.EffectiveLanguage != prevSnap.EffectiveLanguage {
				go s.syncNativeBrowserShield()
			}
		}
	}
}

func (s *Server) hostLeakLoop(ctx context.Context) {
	s.scanDirectSockets()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scanDirectSockets()
		}
	}
}

func (s *Server) scanDirectSockets() {
	s.mu.RLock()
	expIP := s.cfg.Egress.ExpectedIP
	s.mu.RUnlock()
	hits := host.ScanDirectAnthropicSockets(expIP)
	s.directMu.Lock()
	prevCount := len(s.directHits)
	s.directHits = hits
	s.directMu.Unlock()
	if len(hits) > 0 && len(hits) != prevCount {
		for _, h := range hits {
			s.audit.RecordLeak(audit.LeakEvent{
				Kind:    "host_direct_socket",
				Host:    h.DestIP,
				Port:    h.DestPort,
				Process: fmt.Sprintf("%s(%s)", h.Process, h.PID),
				Action:  "alert",
				Reason:  "host process connected directly to Anthropic CIDR bypassing proxy",
			})
		}
	}
}

func (s *Server) currentDirectHits() []host.DirectSocketHit {
	s.directMu.RLock()
	defer s.directMu.RUnlock()
	return append([]host.DirectSocketHit(nil), s.directHits...)
}

const bootTakeoverGraceWindow = 3 * time.Minute

// parseBootTimeSec parses the integer epoch seconds from `sysctl -n kern.boottime`,
// e.g. "{ sec = 1795279619, usec = 805208 } Sun Nov 22 00:46:59 2026".
func parseBootTimeSec(raw string) (int64, bool) {
	idx := strings.Index(raw, "sec")
	if idx < 0 {
		return 0, false
	}
	rest := raw[idx+len("sec"):]
	eq := strings.IndexByte(rest, '=')
	if eq < 0 {
		return 0, false
	}
	rest = strings.TrimSpace(rest[eq+1:])
	end := strings.IndexAny(rest, ",} \t\r\n")
	if end >= 0 {
		rest = rest[:end]
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
	if err != nil || sec <= 0 {
		return 0, false
	}
	return sec, true
}

func readSystemUptime() (time.Duration, bool) {
	if runtime.GOOS != "darwin" {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sysctl", "-n", "kern.boottime").Output()
	if err != nil {
		return 0, false
	}
	sec, ok := parseBootTimeSec(string(out))
	if !ok {
		return 0, false
	}
	return time.Since(time.Unix(sec, 0)), true
}

func shouldExemptPreExistingDesktop(autoPeaceEnabled bool, systemUptime time.Duration, uptimeKnown bool) bool {
	if !autoPeaceEnabled {
		return true
	}
	if uptimeKnown && systemUptime >= 0 && systemUptime <= bootTakeoverGraceWindow {
		// Within 3 minutes of system boot, never exempt unisolated native Claude.app instances
		// restored by macOS Login Window before PeaceClaude LaunchAgent started.
		return false
	}
	return true
}

func (s *Server) snapshotPreExistingDesktop() {
	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()
	rep := host.InspectRunningDesktop(&cfg)
	uptime, uptimeOK := readSystemUptime()
	exempt := shouldExemptPreExistingDesktop(cfg.Identity.AutoPeaceDesktop, uptime, uptimeOK)

	s.takeoverMu.Lock()
	if !exempt {
		s.preExistingPIDs = map[string]bool{}
		s.takeoverMu.Unlock()
		var bareCount int
		for _, p := range rep.Processes {
			if isAutoPeaceTarget(p) {
				bareCount++
			}
		}
		if bareCount > 0 {
			log.Printf("[auto-peace] boot grace window (uptime=%s): %d pre-existing unisolated Claude.app process(es) will be taken over",
				uptime.Round(time.Second), bareCount)
		}
		return
	}
	s.preExistingPIDs = make(map[string]bool, len(rep.Processes))
	for _, p := range rep.Processes {
		if !p.Isolated {
			s.preExistingPIDs[p.PID] = true
		}
	}
	s.takeoverMu.Unlock()
}

func shouldDisableAutoPeace() (bool, string) {
	if os.Getenv("PEACECLAUDE_DISABLE_AUTO_PEACE") == "1" {
		return true, "PEACECLAUDE_DISABLE_AUTO_PEACE=1"
	}
	if strings.TrimSpace(os.Getenv("PEACECLAUDE_HOME")) != "" && os.Getenv("PEACECLAUDE_FORCE_AUTO_PEACE") != "1" {
		return true, "custom PEACECLAUDE_HOME sandbox"
	}
	return false, ""
}

func isAutoPeaceTarget(p host.DesktopProcAudit) bool {
	if p.Isolated {
		return false
	}
	raw := p.RawCommand
	if raw == "" {
		raw = p.Command
	}
	// Auto-Peace only intercepts unisolated native Claude.app launches that carry NO --user-data-dir.
	// Any instance already launched with a custom/isolated --user-data-dir is left alone so two
	// isolated or sandbox daemons can NEVER kill-loop each other.
	return !strings.Contains(raw, "--user-data-dir=")
}

func (s *Server) acquireAutoPeaceLock() bool {
	lockPath := filepath.Join("/tmp", fmt.Sprintf("peaceclaude-auto-peace-%d.lock", os.Getuid()))
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return false
	}
	s.takeoverMu.Lock()
	s.autoPeaceLockFile = f
	s.takeoverMu.Unlock()
	return true
}

func (s *Server) releaseAutoPeaceLock() {
	s.takeoverMu.Lock()
	f := s.autoPeaceLockFile
	s.autoPeaceLockFile = nil
	s.takeoverMu.Unlock()
	if f != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
}

func (s *Server) allowAutoPeaceTrigger(now time.Time) bool {
	if !s.autoPeacePausedUntil.IsZero() && now.Before(s.autoPeacePausedUntil) {
		return false
	}
	cutoff := now.Add(-60 * time.Second)
	kept := s.takeoverAttempts[:0]
	for _, ts := range s.takeoverAttempts {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	s.takeoverAttempts = kept
	if len(s.takeoverAttempts) >= 3 {
		s.autoPeacePausedUntil = now.Add(10 * time.Minute)
		log.Printf("[auto-peace] circuit breaker tripped (>=3 triggers in 60s), pausing auto-peace for 10m")
		s.notifyMacOS("PeaceClaude Auto-Peace 已自动暂停", "检测到 60 秒内高频重复触发接管，已保护性暂停 10 分钟防止循环切窗")
		return false
	}
	s.takeoverAttempts = append(s.takeoverAttempts, now)
	return true
}

func (s *Server) recordAutoPeaceOutcome(now time.Time, failed bool) {
	s.takeoverMu.Lock()
	defer s.takeoverMu.Unlock()
	if !failed {
		s.takeoverConsecFails = 0
		return
	}
	s.takeoverConsecFails++
	if s.takeoverConsecFails >= 2 {
		s.autoPeacePausedUntil = now.Add(10 * time.Minute)
		log.Printf("[auto-peace] circuit breaker tripped (>=2 consecutive failures), pausing auto-peace for 10m")
	}
}

// desktopGuardLoop watches for accidentally launched unisolated Claude.app processes
// and automatically terminates & relaunches them under PeaceClaude's isolated profile.
func (s *Server) desktopGuardLoop(ctx context.Context) {
	if disabled, reason := shouldDisableAutoPeace(); disabled {
		log.Printf("[auto-peace] watcher disabled (%s)", reason)
		return
	}
	if !s.acquireAutoPeaceLock() {
		log.Printf("[auto-peace] another PeaceClaude instance holds host auto-peace lock; watcher disabled here")
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkAndAutoPeaceDesktop(ctx)
		}
	}
}

func (s *Server) checkAndAutoPeaceDesktop(ctx context.Context) {
	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()
	rep := host.InspectRunningDesktop(&cfg)
	if rep.ExposedCount == 0 {
		// Clear pre-existing tracker once old unisolated windows are closed.
		s.takeoverMu.Lock()
		if len(s.preExistingPIDs) > 0 {
			s.preExistingPIDs = map[string]bool{}
		}
		s.takeoverMu.Unlock()
		return
	}

	now := time.Now()
	s.takeoverMu.Lock()
	if time.Since(s.lastTakeover) < 5*time.Second {
		s.takeoverMu.Unlock()
		return
	}
	var newExposed []string
	for _, p := range rep.Processes {
		if isAutoPeaceTarget(p) && !s.preExistingPIDs[p.PID] {
			newExposed = append(newExposed, p.PID)
		}
	}
	if len(newExposed) == 0 {
		s.takeoverMu.Unlock()
		return
	}
	if !cfg.Identity.AutoPeaceDesktop {
		s.lastTakeover = now
		for _, pid := range newExposed {
			s.preExistingPIDs[pid] = true
		}
		s.takeoverMu.Unlock()
		s.warnUnisolatedDesktop(newExposed)
		return
	}
	if !s.allowAutoPeaceTrigger(now) {
		s.takeoverMu.Unlock()
		return
	}
	s.lastTakeover = now
	s.takeoverMu.Unlock()

	_, _, blocked, err := s.executeTakeover(ctx, &cfg, newExposed, "auto-peace")
	s.recordAutoPeaceOutcome(time.Now(), err != nil || blocked != "")
}

func (s *Server) warnUnisolatedDesktop(pids []string) {
	s.audit.RecordLeak(audit.LeakEvent{
		Kind:    "unisolated_desktop_warning",
		Host:    "Claude.app",
		Process: strings.Join(pids, ","),
		Action:  "warn",
		Reason:  "检测到未隔离的原生 Claude.app 启动，已弹出交互警告并引导一键接管",
	})
	if os.Getenv("PEACECLAUDE_NO_NOTIFY") == "1" {
		return
	}
	msg := fmt.Sprintf("检测到直接打开了未隔离的原生 Claude.app (PID %s)，可能绕过静态 IP 直连 Anthropic！\n\n点击「立即切换安全模式」将自动关闭原生窗口并用 PeaceClaude 隔离重新打开。", strings.Join(pids, ", "))
	script := fmt.Sprintf(`set choice to button returned of (display alert "PeaceClaude 安全警告" message "%s" as warning buttons {"暂时忽略", "立即切换安全模式"} default button "立即切换安全模式")
return choice`, launcher.EscapeAppleScript(msg))
	go func() {
		out, err := exec.Command("/usr/bin/osascript", "-e", script).Output()
		if err == nil && strings.Contains(string(out), "立即切换安全模式") {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, _, _, _ = s.TakeoverAllExposed(ctx)
		}
	}()
}

func isOwnedUnisolatedTarget(p host.DesktopProcAudit, ownProfilesDir string) bool {
	if isAutoPeaceTarget(p) {
		return true
	}
	if p.Isolated {
		return false
	}
	raw := p.RawCommand
	if raw == "" {
		raw = p.Command
	}
	dataDir := launcher.ExtractUserDataDir(raw)
	if dataDir == "" {
		return false
	}
	cleanOwn := filepath.Clean(ownProfilesDir)
	return dataDir == cleanOwn || strings.HasPrefix(dataDir, cleanOwn+string(filepath.Separator))
}

// TakeoverAllExposed terminates unisolated native Claude.app instances (and malformed own-profile
// instances, never foreign test sandboxes) and relaunches Claude Desktop in isolated mode if Egress Gate is OPEN.
func (s *Server) TakeoverAllExposed(ctx context.Context) (terminated int, relaunchedPID int, blockedReason string, err error) {
	cfg := s.configSnapshot()
	rep := host.InspectRunningDesktop(&cfg)
	ownProfiles := config.ProfilesDir()
	var exposed []string
	for _, p := range rep.Processes {
		if isOwnedUnisolatedTarget(p, ownProfiles) {
			exposed = append(exposed, p.PID)
		}
	}
	s.takeoverMu.Lock()
	s.preExistingPIDs = map[string]bool{}
	s.lastTakeover = time.Now()
	s.takeoverAttempts = nil
	s.takeoverConsecFails = 0
	s.autoPeacePausedUntil = time.Time{}
	s.takeoverMu.Unlock()

	terminated, relaunchedPID, blockedReason, err = s.executeTakeover(ctx, &cfg, exposed, "manual-takeover")
	return
}

func (s *Server) executeTakeover(ctx context.Context, cfg *config.Config, pids []string, trigger string) (terminated int, relaunchedPID int, blockedReason string, err error) {
	// Harvest any claude:// deep-link URLs passed on the unisolated cold-start command line
	// before terminating it so login/deep-link navigation forwards cleanly into the isolated instance.
	var deepLinks []string
	if len(pids) > 0 {
		pidSet := make(map[string]bool, len(pids))
		for _, pid := range pids {
			pidSet[pid] = true
		}
		seenURL := map[string]bool{}
		rep := host.InspectRunningDesktop(cfg)
		for _, proc := range rep.Processes {
			if !pidSet[proc.PID] {
				continue
			}
			raw := proc.RawCommand
			if raw == "" {
				raw = proc.Command
			}
			for _, u := range launcher.ExtractClaudeDeepLinkArgs(raw) {
				if !seenURL[u] {
					seenURL[u] = true
					deepLinks = append(deepLinks, u)
				}
			}
		}

		terminated = launcher.TerminatePIDs(pids, 4*time.Second)
		s.audit.RecordLeak(audit.LeakEvent{
			Kind:    "auto_peace_takeover",
			Host:    "Claude.app",
			Process: strings.Join(pids, ","),
			Action:  trigger,
			Reason:  fmt.Sprintf("终止 %d 个未隔离原生 Claude.app 进程，准备切换安全隔离模式", terminated),
		})
	}

	// Verify Egress Gate before relaunching; if daemon just booted and initial probe hasn't finished,
	// perform one immediate probe before enforcing Fail-Closed block.
	gateOpen, lockReason := s.gate.AllowTraffic()
	if !gateOpen && cfg.Egress.FailClosed {
		snap := s.gate.Snapshot()
		if !snap.ManualLock && snap.LastProbe.CheckedAt.IsZero() {
			probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_ = s.gate.Check(probeCtx, false)
			cancel()
			gateOpen, lockReason = s.gate.AllowTraffic()
		}
	}
	if !gateOpen && cfg.Egress.FailClosed {
		blockedReason = lockReason
		if blockedReason == "" {
			blockedReason = "Egress Gate 已熔断"
		}
		log.Printf("[auto-peace] terminated %d unisolated Claude.app (%s), blocked relaunch: %s", terminated, trigger, blockedReason)
		s.notifyMacOS("PeaceClaude 安全拦截", fmt.Sprintf("已关闭未隔离 Claude.app；因静态 IP 门禁锁定未重开：%s", blockedReason))
		return terminated, 0, blockedReason, nil
	}

	spec, pid, _, launchErr := launcher.LaunchDesktopWithOptions(ctx, cfg, s.selfBin, cfg.ActiveProfile, launcher.DesktopLaunchOptions{
		ExtraArgs:       deepLinks,
		ExclusiveSwitch: cfg.Identity.SingleActiveDesktop,
	})
	if launchErr != nil {
		log.Printf("[auto-peace] relaunch isolated Claude Desktop failed: %v", launchErr)
		s.notifyMacOS("PeaceClaude 接管失败", launchErr.Error())
		return terminated, 0, "", launchErr
	}
	log.Printf("[auto-peace] %s succeeded: terminated=%d, relaunched isolated Claude Desktop PID=%d profile=%s deepLinks=%d",
		trigger, terminated, pid, spec.Profile, len(deepLinks))
	s.notifyMacOS("PeaceClaude 已自动接管", fmt.Sprintf("已将误点的原生 Claude.app 切入安全隔离模式 (档案 %s · 出口 %s)",
		spec.Profile, s.gate.Snapshot().LastGood.EgressIP))
	return terminated, pid, "", nil
}

func (s *Server) notifyMacOS(title, body string) {
	if os.Getenv("PEACECLAUDE_NO_NOTIFY") == "1" {
		return
	}
	s.notifyMu.Lock()
	if time.Since(s.lastNotify) < 60*time.Second {
		s.notifyMu.Unlock()
		return
	}
	s.lastNotify = time.Now()
	s.notifyMu.Unlock()
	safeTitle := launcher.EscapeAppleScript(title)
	safeBody := launcher.EscapeAppleScript(body)
	go func() {
		_ = exec.Command("/usr/bin/osascript", "-e",
			fmt.Sprintf(`display notification "%s" with title "%s"`, safeBody, safeTitle)).Run()
	}()
}
