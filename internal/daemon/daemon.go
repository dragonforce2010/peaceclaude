package daemon

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"peaceclaude/internal/audit"
	"peaceclaude/internal/config"
	"peaceclaude/internal/egress"
	"peaceclaude/internal/host"
	"peaceclaude/internal/launcher"
	"peaceclaude/internal/policy"
	"peaceclaude/internal/profile"
	"peaceclaude/internal/proxy"
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

	takeoverMu      sync.Mutex
	preExistingPIDs map[string]bool
	lastTakeover    time.Time

	notifyMu    sync.Mutex
	lastNotify  time.Time
}

func New(cfg *config.Config, selfBin string) *Server {
	eng := policy.NewEngine(cfg)
	gate := egress.NewGate(cfg)
	store := audit.NewStore(cfg.Capture.Enabled, cfg.Capture.MaxEntries)
	gw := proxy.NewGateway(cfg, eng, gate, store)

	s := &Server{
		cfg:       cfg,
		selfBin:   selfBin,
		startedAt: time.Now().UTC(),
		gate:      gate,
		policy:    eng,
		audit:     store,
		gateway:   gw,
		doneCh:    make(chan struct{}),
	}

	gate.SetOnStateChange(func(open bool, reason string, res egress.ProbeResult) {
		if !open {
			closed := gw.CloseProtectedConns()
			log.Printf("[egress-gate] LOCKED (fail-closed): %s (closed %d active conns)", reason, closed)
			s.notifyMacOS("PeaceClaude 安全熔断", fmt.Sprintf("已阻断 Claude 出站：%s", reason))
		} else {
			log.Printf("[egress-gate] OPEN: egress=%s (%s · %s · TZ=%s)",
				res.EgressIP, res.Country, res.City, gate.Snapshot().EffectiveTimezone)
			s.notifyMacOS("PeaceClaude 恢复放行", fmt.Sprintf("静态 IP 出口就绪：%s (%s)", res.EgressIP, res.Country))
		}
	})
	return s
}

func (s *Server) Gate() *egress.Gate      { return s.gate }
func (s *Server) Gateway() *proxy.Gateway { return s.gateway }
func (s *Server) Audit() *audit.Store     { return s.audit }
func (s *Server) Done() <-chan struct{}   { return s.doneCh }

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
		s.probeLoop(runCtx)
	}()
	go s.hostLeakLoop(runCtx)
	go s.desktopGuardLoop(runCtx)
	return nil
}

func (s *Server) ControlAddr() string {
	if s.ctrlLn != nil {
		return s.ctrlLn.Addr().String()
	}
	return s.cfg.ControlAddr()
}

func (s *Server) HTTPProxyAddr() string {
	if s.httpLn != nil {
		return s.httpLn.Addr().String()
	}
	return s.cfg.HTTPProxyAddr()
}

func (s *Server) SOCKS5ProxyAddr() string {
	if s.socksLn != nil {
		return s.socksLn.Addr().String()
	}
	return s.cfg.SOCKS5ProxyAddr()
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
		if ok, _ := s.gate.AllowTraffic(); !ok {
			wait = badInt
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			s.gate.Check(pctx, true)
			cancel()
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

func (s *Server) snapshotPreExistingDesktop() {
	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()
	rep := host.InspectRunningDesktop(&cfg)
	s.takeoverMu.Lock()
	s.preExistingPIDs = make(map[string]bool, len(rep.Processes))
	for _, p := range rep.Processes {
		if !p.Isolated {
			s.preExistingPIDs[p.PID] = true
		}
	}
	s.takeoverMu.Unlock()
}

// desktopGuardLoop watches for accidentally launched unisolated Claude.app processes
// and automatically terminates & relaunches them under PeaceClaude's isolated profile.
func (s *Server) desktopGuardLoop(ctx context.Context) {
	if os.Getenv("PEACECLAUDE_DISABLE_AUTO_PEACE") == "1" {
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

	s.takeoverMu.Lock()
	if time.Since(s.lastTakeover) < 5*time.Second {
		s.takeoverMu.Unlock()
		return
	}
	var newExposed []string
	for _, p := range rep.Processes {
		if !p.Isolated && !s.preExistingPIDs[p.PID] {
			newExposed = append(newExposed, p.PID)
		}
	}
	if len(newExposed) == 0 {
		s.takeoverMu.Unlock()
		return
	}
	s.lastTakeover = time.Now()
	if !cfg.Identity.AutoPeaceDesktop {
		for _, pid := range newExposed {
			s.preExistingPIDs[pid] = true
		}
		s.takeoverMu.Unlock()
		s.warnUnisolatedDesktop(newExposed)
		return
	}
	s.takeoverMu.Unlock()

	s.executeTakeover(ctx, &cfg, newExposed, "auto-peace")
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
return choice`, strings.ReplaceAll(msg, `"`, `'`))
	go func() {
		out, err := exec.Command("/usr/bin/osascript", "-e", script).Output()
		if err == nil && strings.Contains(string(out), "立即切换安全模式") {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, _, _, _ = s.TakeoverAllExposed(ctx)
		}
	}()
}

// TakeoverAllExposed terminates ALL running unisolated Claude.app instances (including pre-existing ones)
// and relaunches Claude Desktop in isolated PeaceClaude mode if Egress Gate is OPEN.
func (s *Server) TakeoverAllExposed(ctx context.Context) (terminated int, relaunchedPID int, blockedReason string, err error) {
	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()
	rep := host.InspectRunningDesktop(&cfg)
	var exposed []string
	for _, p := range rep.Processes {
		if !p.Isolated {
			exposed = append(exposed, p.PID)
		}
	}
	s.takeoverMu.Lock()
	s.preExistingPIDs = map[string]bool{}
	s.lastTakeover = time.Now()
	s.takeoverMu.Unlock()

	terminated, relaunchedPID, blockedReason, err = s.executeTakeover(ctx, &cfg, exposed, "manual-takeover")
	return
}

func (s *Server) executeTakeover(ctx context.Context, cfg *config.Config, pids []string, trigger string) (terminated int, relaunchedPID int, blockedReason string, err error) {
	if len(pids) > 0 {
		terminated = launcher.TerminatePIDs(pids, 4*time.Second)
		s.audit.RecordLeak(audit.LeakEvent{
			Kind:    "auto_peace_takeover",
			Host:    "Claude.app",
			Process: strings.Join(pids, ","),
			Action:  trigger,
			Reason:  fmt.Sprintf("终止 %d 个未隔离原生 Claude.app 进程，准备切换安全隔离模式", terminated),
		})
	}

	// Verify Egress Gate before relaunching; never relaunch while fail-closed locked.
	gateOpen, lockReason := s.gate.AllowTraffic()
	if !gateOpen && cfg.Egress.FailClosed {
		blockedReason = lockReason
		if blockedReason == "" {
			blockedReason = "Egress Gate 已熔断"
		}
		log.Printf("[auto-peace] terminated %d unisolated Claude.app (%s), blocked relaunch: %s", terminated, trigger, blockedReason)
		s.notifyMacOS("PeaceClaude 安全拦截", fmt.Sprintf("已关闭未隔离 Claude.app；因静态 IP 门禁锁定未重开：%s", blockedReason))
		return terminated, 0, blockedReason, nil
	}

	spec, pid, launchErr := launcher.LaunchDesktop(ctx, cfg, s.selfBin, cfg.ActiveProfile, nil, false)
	if launchErr != nil {
		log.Printf("[auto-peace] relaunch isolated Claude Desktop failed: %v", launchErr)
		s.notifyMacOS("PeaceClaude 接管失败", launchErr.Error())
		return terminated, 0, "", launchErr
	}
	log.Printf("[auto-peace] %s succeeded: terminated=%d, relaunched isolated Claude Desktop PID=%d profile=%s",
		trigger, terminated, pid, spec.Profile)
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
	safeTitle := strings.ReplaceAll(title, `"`, `'`)
	safeBody := strings.ReplaceAll(body, `"`, `'`)
	go func() {
		_ = exec.Command("/usr/bin/osascript", "-e",
			fmt.Sprintf(`display notification "%s" with title "%s"`, safeBody, safeTitle)).Run()
	}()
}
