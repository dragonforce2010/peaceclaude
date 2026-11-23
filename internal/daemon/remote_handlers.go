package daemon

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"peaceclaude/internal/audit"
	"peaceclaude/internal/config"
	"peaceclaude/internal/host"
	"peaceclaude/internal/launcher"
	"peaceclaude/internal/profile"
	"peaceclaude/internal/remote"
	"peaceclaude/internal/ui"
	"peaceclaude/internal/version"
)

type StorageSummary struct {
	BaseDir        string `json:"base_dir"`
	ConfigPath     string `json:"config_path"`
	ProfilesDir    string `json:"profiles_dir"`
	LogsDir        string `json:"logs_dir"`
	StateDir       string `json:"state_dir"`
	SharedRulesDir string `json:"shared_rules_dir"`
	BackupsDir     string `json:"backups_dir"`
	BackendType    string `json:"backend_type"`
	RetentionDays  int    `json:"retention_days"`
	ProfilesBytes  int64  `json:"profiles_bytes"`
	LogsBytes      int64  `json:"logs_bytes"`
	LeaksBytes     int64  `json:"leaks_bytes"`
	CapturesBytes  int64  `json:"captures_bytes"`
}

func currentHostUsername() string {
	if u, err := user.Current(); err == nil && strings.TrimSpace(u.Username) != "" {
		return strings.TrimSpace(u.Username)
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Base(h)
	}
	return "local"
}

func dirSizeBytes(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, ierr := d.Info(); ierr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func fileSizeBytes(p string) int64 {
	if fi, err := os.Stat(p); err == nil {
		return fi.Size()
	}
	return 0
}

var (
	storageCacheMu   sync.Mutex
	storageCacheAt   time.Time
	storageCacheBase string
	storageCacheVal  StorageSummary
)

func collectStorageSummary() StorageSummary {
	base := config.BaseDir()
	storageCacheMu.Lock()
	if storageCacheBase == base && time.Since(storageCacheAt) < 30*time.Second {
		cached := storageCacheVal
		storageCacheMu.Unlock()
		return cached
	}
	storageCacheMu.Unlock()

	sum := StorageSummary{
		BaseDir:        base,
		ConfigPath:     config.ConfigPath(),
		ProfilesDir:    config.ProfilesDir(),
		LogsDir:        config.LogsDir(),
		StateDir:       config.StateDir(),
		SharedRulesDir: config.SharedRulesDir(),
		BackupsDir:     config.BackupsDir(),
		BackendType:    "本地文件目录 + JSON (0700/0600，非 SQLite)",
		RetentionDays:  audit.DefaultRetentionDays,
		ProfilesBytes:  dirSizeBytes(config.ProfilesDir()),
		LogsBytes:      dirSizeBytes(config.LogsDir()),
		LeaksBytes:     fileSizeBytes(config.LeaksLogPath()),
		CapturesBytes:  fileSizeBytes(config.CaptureLogPath()),
	}

	storageCacheMu.Lock()
	storageCacheBase = base
	storageCacheAt = time.Now()
	storageCacheVal = sum
	storageCacheMu.Unlock()
	return sum
}

func (s *Server) handleWorkbenchLock(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Locked bool `json:"locked"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.uiLocked.Store(body.Locked)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"ui_locked": body.Locked,
	})
}

func (s *Server) handleAppearanceGet(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	app := s.cfg.Appearance
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, app)
}

func (s *Server) handleAppearanceUpdate(w http.ResponseWriter, r *http.Request) {
	var nextApp config.AppearanceConfig
	if err := decodeJSON(r, &nextApp); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Lock()
	next := *s.cfg
	next.Appearance = nextApp
	if err := config.Save(&next); err != nil {
		s.mu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.swapConfigLocked(next)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"appearance": next.Appearance,
	})
}

func (s *Server) syncMobileListener(cfg *config.Config) error {
	wantOn := cfg != nil && cfg.Remote.Enabled && cfg.Remote.RelayMode == "lan"
	wantPort := 18900
	if cfg != nil && cfg.Remote.MobilePort > 0 {
		wantPort = cfg.Remote.MobilePort
	}
	wantAddr := fmt.Sprintf("0.0.0.0:%d", wantPort)

	s.mobileMu.Lock()
	defer s.mobileMu.Unlock()

	if !wantOn {
		s.closeMobileLocked(context.Background())
		return nil
	}
	if s.mobileSrv != nil && s.mobileListenAddr == wantAddr {
		return nil
	}
	s.closeMobileLocked(context.Background())

	ln, err := net.Listen("tcp", wantAddr)
	if err != nil {
		return fmt.Errorf("listen mobile remote port %s: %w", wantAddr, err)
	}
	srv := &http.Server{
		Handler:           s.mobileHandler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.mobileLn = ln
	s.mobileSrv = srv
	s.mobileListenAddr = ln.Addr().String()
	go func(listener net.Listener, server *http.Server) {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			// Expected on teardown
		}
	}(ln, srv)
	return nil
}

func (s *Server) stopMobileListener(ctx context.Context) {
	s.mobileMu.Lock()
	defer s.mobileMu.Unlock()
	s.closeMobileLocked(ctx)
}

func (s *Server) closeMobileLocked(ctx context.Context) {
	if s.mobileSrv != nil {
		shCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = s.mobileSrv.Shutdown(shCtx)
		cancel()
		s.mobileSrv = nil
	}
	if s.mobileLn != nil {
		_ = s.mobileLn.Close()
		s.mobileLn = nil
	}
	s.mobileListenAddr = ""
}

func (s *Server) mobileBindAddr() string {
	s.mobileMu.Lock()
	defer s.mobileMu.Unlock()
	return s.mobileListenAddr
}

func (s *Server) handleRemoteStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, s.remoteMgr.Status(&cfg, s.mobileBindAddr()))
}

func (s *Server) handleRemotePairCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Host string `json:"host"`
	}
	_ = decodeJSON(r, &body)

	s.mu.Lock()
	cfg := *s.cfg
	if cfg.Remote.RelayMode == "" {
		cfg.Remote.RelayMode = "lan"
	}
	if cfg.Remote.RelayMode == "lan" && !cfg.Remote.Enabled {
		cfg.Remote.Enabled = true
		if err := config.Save(&cfg); err == nil {
			s.swapConfigLocked(cfg)
		}
	}
	s.mu.Unlock()

	if cfg.Remote.RelayMode == "lan" {
		if err := s.syncMobileListener(&cfg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	ticket, err := s.remoteMgr.CreatePairTicket(r.Context(), &cfg, body.Host)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"ticket": ticket,
		"status": s.remoteMgr.Status(&cfg, s.mobileBindAddr()),
	})
}

func (s *Server) handleRemoteRevoke(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	_ = decodeJSON(r, &body)

	var revoked int
	if strings.TrimSpace(body.ID) != "" {
		if s.remoteMgr.RevokeDevice(strings.TrimSpace(body.ID)) {
			revoked = 1
		}
	} else {
		revoked = s.remoteMgr.RevokeAll()
		s.mu.Lock()
		next := *s.cfg
		next.Remote.Enabled = false
		if config.Save(&next) == nil {
			s.swapConfigLocked(next)
		}
		s.mu.Unlock()
		_ = s.syncMobileListener(&next)
	}
	cfg := s.configSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"revoked": revoked,
		"status":  s.remoteMgr.Status(&cfg, s.mobileBindAddr()),
	})
}

func (s *Server) handleRemoteRelayUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled         *bool  `json:"enabled"`
		RelayMode       string `json:"relay_mode"`
		CustomRelayURL  string `json:"custom_relay_url"`
		RelayAdminToken string `json:"relay_admin_token"`
		MobilePort      int    `json:"mobile_port"`
		TestOnly        bool   `json:"test_only"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if body.TestOnly {
		h := s.remoteMgr.CheckRelayHealth(r.Context(), body.CustomRelayURL)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     h.OK,
			"health": h,
		})
		return
	}

	s.mu.Lock()
	next := *s.cfg
	if body.Enabled != nil {
		next.Remote.Enabled = *body.Enabled
	}
	if body.RelayMode != "" {
		next.Remote.RelayMode = body.RelayMode
	}
	if next.Remote.RelayMode == "custom" {
		norm, err := remote.ValidateRelayURL(body.CustomRelayURL)
		if err != nil {
			s.mu.Unlock()
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		next.Remote.CustomRelayURL = norm
	} else if body.CustomRelayURL != "" {
		if norm, err := remote.ValidateRelayURL(body.CustomRelayURL); err == nil {
			next.Remote.CustomRelayURL = norm
		}
	}
	if body.RelayAdminToken != "" {
		next.Remote.RelayAdminToken = body.RelayAdminToken
	}
	if body.MobilePort > 0 {
		next.Remote.MobilePort = body.MobilePort
	}
	if err := config.Save(&next); err != nil {
		s.mu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.swapConfigLocked(next)
	s.mu.Unlock()

	if err := s.syncMobileListener(&next); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var health *remote.RelayHealth
	if next.Remote.RelayMode == "custom" && next.Remote.CustomRelayURL != "" {
		h := s.remoteMgr.CheckRelayHealth(r.Context(), next.Remote.CustomRelayURL)
		health = &h
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"health": health,
		"status": s.remoteMgr.Status(&next, s.mobileBindAddr()),
	})
}

func (s *Server) mobileHandler() http.Handler {
	mux := http.NewServeMux()
	servePage := func(w http.ResponseWriter, _ *http.Request) {
		raw, err := ui.MobileHTML()
		if err != nil {
			http.Error(w, "mobile UI missing", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(raw)
	}
	mux.HandleFunc("/", servePage)
	mux.HandleFunc("/mobile", servePage)
	mux.HandleFunc("/mobile/api/redeem", s.handleMobileRedeem)
	mux.HandleFunc("/mobile/api/state", s.withMobileAuth(s.handleMobileState))
	mux.HandleFunc("/mobile/api/action", s.withMobileAuth(s.handleMobileAction))
	return mux
}

func (s *Server) handleMobileRedeem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	var body struct {
		Code       string `json:"code"`
		DeviceName string `json:"device_name"`
		OS         string `json:"os"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	tok, dev, err := s.remoteMgr.RedeemPair(body.Code, body.DeviceName, body.OS)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"token":  tok,
		"device": dev,
	})
}

func (s *Server) withMobileAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		tok := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") || tok == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未配对或凭证无效，请在电脑端重新扫码"})
			return
		}
		if _, ok := s.remoteMgr.AuthenticateBearer(tok); !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "设备会话已失效或已被电脑端撤回"})
			return
		}
		next(w, r)
	}
}

func (s *Server) handleMobileState(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()

	list, _ := profile.List(&cfg)
	rep := host.InspectRunningDesktop(&cfg)
	refresh := r.URL.Query().Get("refresh") == "1"

	accounts := make([]map[string]any, 0, len(list))
	for _, p := range list {
		pids := findRunningPIDsForProfile(rep, p)
		runningPID := ""
		if len(pids) > 0 {
			runningPID = pids[0]
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		usage := s.getProfileUsage(ctx, &cfg, &p, refresh)
		cancel()
		activated := p.HasCLICreds || p.HasDesktopAuth || p.Account.Source == "claude_json"
		accounts = append(accounts, map[string]any{
			"name":                p.Name,
			"display_name":        p.DisplayName,
			"active":              p.Active,
			"activated":           activated,
			"running_desktop_pid": runningPID,
			"plan_label":          p.Account.PlanLabel,
			"email":               p.Account.Email,
			"account_age_text":    p.Account.AccountAgeText,
			"usage":               usage,
		})
	}
	hostName, _ := os.Hostname()
	docCtx, docCancel := context.WithTimeout(r.Context(), 4*time.Second)
	docRep := host.RunDoctor(docCtx, &cfg, true, s.gate.Snapshot())
	docCancel()

	writeJSON(w, http.StatusOK, map[string]any{
		"version":        version.Version,
		"platform":       runtime.GOOS,
		"host_name":      hostName,
		"gate":           s.gate.Snapshot(),
		"stats":          s.audit.Stats(),
		"active_profile": cfg.ActiveProfile,
		"ui_locked":      s.uiLocked.Load(),
		"upstream_route": s.gateway.UpstreamRouteStatus(),
		"browser_pac":    host.InspectBrowserPAC(&cfg),
		"doctor_summary": map[string]any{
			"score":           docRep.Score,
			"grade":           docRep.Grade,
			"critical_passed": docRep.CriticalPassed,
			"critical_total":  docRep.CriticalTotal,
		},
		"accounts": accounts,
	})
}

func (s *Server) handleMobileAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	var body struct {
		Action  string `json:"action"` // set_active | launch_desktop | stop_desktop | check_gate | lock_gate | unlock_gate | lock_ui | toggle_pac
		Profile string `json:"profile"`
		Locked  *bool  `json:"locked"`
		Enabled *bool  `json:"enabled"`
		Reason  string `json:"reason"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()
	name := strings.TrimSpace(body.Profile)
	if name == "" {
		name = cfg.ActiveProfile
	}

	switch body.Action {
	case "set_active":
		s.mu.Lock()
		next := *s.cfg
		if _, err := profile.Ensure(&next, name, ""); err != nil {
			s.mu.Unlock()
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		next.ActiveProfile = name
		err := config.Save(&next)
		if err == nil {
			s.swapConfigLocked(next)
		}
		s.mu.Unlock()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "active": name})
	case "launch_desktop":
		spec, pid, stopped, err := launcher.LaunchDesktopWithOptions(r.Context(), &cfg, s.selfBin, name, launcher.DesktopLaunchOptions{
			ExclusiveSwitch: cfg.Identity.SingleActiveDesktop,
		})
		if err != nil {
			writeJSON(w, http.StatusPreconditionFailed, map[string]string{"error": err.Error()})
			return
		}
		profile.TouchLastUsed(spec.Profile)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "pid": pid, "profile": spec.Profile, "stopped_others": stopped})
	case "stop_desktop":
		p, err := profile.Load(&cfg, name)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		rep := host.InspectRunningDesktop(&cfg)
		pids := findRunningPIDsForProfile(rep, *p)
		stopped := 0
		if len(pids) > 0 {
			stopped = launcher.TerminatePIDs(pids, 4*time.Second)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "stopped": stopped})
	case "check_gate":
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		res := s.gate.Check(ctx, false)
		cancel()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": res, "gate": s.gate.Snapshot()})
	case "lock_gate":
		reason := strings.TrimSpace(body.Reason)
		if reason == "" {
			reason = "Locked from mobile PeaceSwitch remote"
		}
		s.gate.SetManualLock(true, reason)
		closed := s.gateway.CloseProtectedConns()
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":           true,
			"closed_conns": closed,
			"gate":         s.gate.Snapshot(),
		})
	case "unlock_gate":
		s.gate.SetManualLock(false, "")
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		res := s.gate.Check(ctx, false)
		cancel()
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     true,
			"result": res,
			"gate":   s.gate.Snapshot(),
		})
	case "lock_ui":
		want := !s.uiLocked.Load()
		if body.Locked != nil {
			want = *body.Locked
		}
		s.uiLocked.Store(want)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"ui_locked": want,
		})
	case "toggle_pac":
		curPAC := host.InspectBrowserPAC(&cfg)
		want := !curPAC.MatchesPeace
		if body.Enabled != nil {
			want = *body.Enabled
		}
		s.mu.Lock()
		next := *s.cfg
		next.Listen.AutoEnableBrowserPAC = &want
		next.Listen.BrowserPACPaused = !want
		if err := config.Save(&next); err != nil {
			s.mu.Unlock()
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		s.swapConfigLocked(next)
		s.mu.Unlock()

		var pacSt host.BrowserPACStatus
		if os.Getenv("PEACECLAUDE_HOME") == "" && os.Getenv("PEACECLAUDE_NO_SYS_PAC") == "" {
			var pacErr error
			pacSt, _, pacErr = host.SetSystemBrowserPAC(&next, want, false)
			if pacErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": pacErr.Error()})
				return
			}
		} else {
			pacSt = host.InspectBrowserPAC(&next)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":          true,
			"enabled":     want,
			"browser_pac": pacSt,
		})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported mobile action"})
	}
}
