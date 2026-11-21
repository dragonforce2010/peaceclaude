package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"peaceclaude/internal/config"
	"peaceclaude/internal/host"
	"peaceclaude/internal/launcher"
	"peaceclaude/internal/profile"
	"peaceclaude/internal/proxy"
	"peaceclaude/internal/ui"
	"peaceclaude/internal/version"
)

func (s *Server) controlHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/check", s.withCSRF(s.handleCheck))
	mux.HandleFunc("/api/lock", s.withCSRF(s.handleLock))
	mux.HandleFunc("/api/leaks", s.handleLeaks)
	mux.HandleFunc("/api/leaks/clear", s.withCSRF(s.handleLeaksClear))
	mux.HandleFunc("/api/capture", s.handleCapture)
	mux.HandleFunc("/api/capture/toggle", s.withCSRF(s.handleCaptureToggle))
	mux.HandleFunc("/api/capture/clear", s.withCSRF(s.handleCaptureClear))
	mux.HandleFunc("/api/profiles", s.handleProfiles)
	mux.HandleFunc("/api/profiles/create", s.withCSRF(s.handleProfileCreate))
	mux.HandleFunc("/api/profiles/use", s.withCSRF(s.handleProfileUse))
	mux.HandleFunc("/api/profiles/clean", s.withCSRF(s.handleProfileClean))
	mux.HandleFunc("/api/accounts", s.handleAccounts)
	mux.HandleFunc("/api/accounts/detail", s.handleAccountDetail)
	mux.HandleFunc("/api/accounts/update", s.withCSRF(s.handleAccountUpdate))
	mux.HandleFunc("/api/accounts/delete", s.withCSRF(s.handleAccountDelete))
	mux.HandleFunc("/api/accounts/stop-desktop", s.withCSRF(s.handleAccountStopDesktop))
	mux.HandleFunc("/api/rules/toggle", s.withCSRF(s.handleRulesToggle))
	mux.HandleFunc("/api/doctor", s.handleDoctor)
	mux.HandleFunc("/api/doctor/fix", s.withCSRF(s.handleDoctorFix))
	mux.HandleFunc("/api/diagnose", s.handleDiagnose)
	mux.HandleFunc("/api/pause-and-recheck", s.withCSRF(s.handlePauseAndRecheck))
	mux.HandleFunc("/api/inspect/desktop", s.handleInspectDesktop)
	mux.HandleFunc("/api/desktop/takeover", s.withCSRF(s.handleDesktopTakeover))
	mux.HandleFunc("/api/launch/desktop", s.withCSRF(s.handleLaunchDesktop))
	mux.HandleFunc("/api/launch/browser", s.withCSRF(s.handleLaunchBrowser))
	mux.HandleFunc("/api/browser-pac", s.withCSRF(s.handleBrowserPAC))
	mux.HandleFunc("/api/config/update", s.withCSRF(s.handleConfigUpdate))
	mux.HandleFunc("/api/workbench/lock", s.withCSRF(s.handleWorkbenchLock))
	mux.HandleFunc("/api/appearance", s.handleAppearanceGet)
	mux.HandleFunc("/api/appearance/update", s.withCSRF(s.handleAppearanceUpdate))
	mux.HandleFunc("/api/remote", s.handleRemoteStatus)
	mux.HandleFunc("/api/remote/pair", s.withCSRF(s.handleRemotePairCreate))
	mux.HandleFunc("/api/remote/revoke", s.withCSRF(s.handleRemoteRevoke))
	mux.HandleFunc("/api/remote/relay", s.withCSRF(s.handleRemoteRelayUpdate))
	mux.HandleFunc("/api/shutdown", s.withCSRF(s.handleShutdown))
	mux.HandleFunc("/peaceclaude.pac", s.handlePAC)
	mux.HandleFunc("/mobile", func(w http.ResponseWriter, r *http.Request) {
		raw, err := ui.MobileHTML()
		if err != nil {
			http.Error(w, "mobile UI not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(raw)
	})
	mux.HandleFunc("/mobile/api/redeem", s.withCSRF(s.handleMobileRedeem))
	mux.HandleFunc("/mobile/api/state", s.withMobileAuth(s.handleMobileState))
	mux.HandleFunc("/mobile/api/action", s.withMobileAuth(s.handleMobileAction))
	mux.Handle("/", ui.Handler())
	return mux
}

func (s *Server) withCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
			return
		}
		// Ensure Host is loopback (prevents DNS rebinding attacks).
		h := strings.ToLower(r.Host)
		if !strings.HasPrefix(h, "127.0.0.1") && !strings.HasPrefix(h, "localhost") && !strings.HasPrefix(h, "[::1]") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "non-loopback Host rejected"})
			return
		}
		// If browser sends Origin, it MUST match loopback origin.
		if origin := strings.ToLower(strings.TrimSpace(r.Header.Get("Origin"))); origin != "" {
			if !strings.HasPrefix(origin, "http://127.0.0.1") && !strings.HasPrefix(origin, "http://localhost") && !strings.HasPrefix(origin, "http://[::1]") {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin mutation rejected"})
				return
			}
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 256*1024))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	return json.Unmarshal(body, dst)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"pid":        os.Getpid(),
		"version":    version.Version,
		"started_at": s.startedAt,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	cfg := s.configSnapshot()

	profs, _ := profile.List(&cfg)
	activeDisplayName := ""
	activeAvatarID := ""
	for _, p := range profs {
		if p.Name == cfg.ActiveProfile || p.Active {
			activeDisplayName = p.DisplayName
			activeAvatarID = p.AvatarID
			break
		}
	}
	directHits := s.currentDirectHits()
	remoteSt := s.remoteMgr.Status(&cfg, s.mobileBindAddr())
	if remoteSt.ActivePair != nil {
		// Redact pairing secret & QR payload from passive polling /api/status;
		// full ticket is returned exclusively by /api/remote and POST /api/remote/pair.
		ap := *remoteSt.ActivePair
		ap.Code = ""
		ap.URL = ""
		ap.SVG = ""
		remoteSt.ActivePair = &ap
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"version":             version.Version,
		"pid":                 os.Getpid(),
		"uptime_sec":          int64(time.Since(s.startedAt).Seconds()),
		"host_user":           currentHostUsername(),
		"ui_locked":           s.uiLocked.Load(),
		"config":              cfg.Redacted(),
		"gate":                s.gate.Snapshot(),
		"stats":               s.audit.Stats(),
		"clash":               host.InspectClash(cfg.Upstream.ClashSocket),
		"vm_proxy":            host.InspectVMProxy(&cfg),
		"active_profile":      cfg.ActiveProfile,
		"active_display_name": activeDisplayName,
		"active_avatar_id":    activeAvatarID,
		"profiles_count":      len(profs),
		"rules":               profile.GetRulesStatus(&cfg),
		"direct_leaks_count":  len(directHits),
		"browser_pac":         host.InspectBrowserPAC(&cfg),
		"storage":             collectStorageSummary(),
		"remote":              remoteSt,
	})
}

func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	res := s.gate.Check(ctx, false)
	writeJSON(w, http.StatusOK, map[string]any{
		"result": res,
		"gate":   s.gate.Snapshot(),
	})
}

func (s *Server) handleLock(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Locked bool   `json:"locked"`
		Reason string `json:"reason"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.gate.SetManualLock(body.Locked, body.Reason)
	closed := 0
	if body.Locked {
		closed = s.gateway.CloseProtectedConns()
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		s.gate.Check(ctx, false)
		cancel()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"closed_conns": closed,
		"gate":         s.gate.Snapshot(),
	})
}

func (s *Server) handleLeaks(w http.ResponseWriter, r *http.Request) {
	since := 24 * time.Hour
	if sv := r.URL.Query().Get("since"); sv != "" {
		if d, err := time.ParseDuration(sv); err == nil {
			since = d
		}
	}
	limit := 200
	if lv := r.URL.Query().Get("limit"); lv != "" {
		if n, err := strconv.Atoi(lv); err == nil && n > 0 {
			limit = n
		}
	}
	s.scanDirectSockets()
	writeJSON(w, http.StatusOK, map[string]any{
		"events":              s.audit.ListLeaks(since, limit),
		"host_direct_sockets": s.currentDirectHits(),
	})
}

func (s *Server) handleLeaksClear(w http.ResponseWriter, _ *http.Request) {
	s.audit.ClearLeaks()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleCapture(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if lv := r.URL.Query().Get("limit"); lv != "" {
		if n, err := strconv.Atoi(lv); err == nil && n > 0 {
			limit = n
		}
	}
	if id := r.URL.Query().Get("id"); id != "" {
		rec, ok := s.audit.GetCapture(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "capture record not found"})
			return
		}
		writeJSON(w, http.StatusOK, rec)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": s.audit.CaptureEnabled(),
		"records": s.audit.ListCaptures(limit),
	})
}

func (s *Server) handleCaptureToggle(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.audit.SetCaptureEnabled(body.Enabled)
	s.mu.Lock()
	next := *s.cfg
	next.Capture.Enabled = body.Enabled
	_ = config.Save(&next)
	s.swapConfigLocked(next)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": body.Enabled})
}

func (s *Server) handleCaptureClear(w http.ResponseWriter, _ *http.Request) {
	s.audit.ClearCaptures()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleProfiles(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()
	list, err := profile.List(&cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"active":   cfg.ActiveProfile,
		"profiles": list,
	})
}

func (s *Server) handleProfileCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name               string `json:"name"`
		Note               string `json:"note"`
		DisplayName        string `json:"display_name"`
		ManualPlan         string `json:"manual_plan"`
		ManualRegisteredAt string `json:"manual_registered_at"`
		UseNow             bool   `json:"use_now"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Lock()
	cfg := *s.cfg
	s.mu.Unlock()
	name := strings.TrimSpace(body.Name)
	p, err := profile.Ensure(&cfg, name, strings.TrimSpace(body.Note))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.DisplayName != "" || body.ManualPlan != "" || body.ManualRegisteredAt != "" {
		patch := profile.MetaPatch{}
		if body.DisplayName != "" {
			patch.DisplayName = &body.DisplayName
		}
		if body.ManualPlan != "" {
			patch.ManualPlan = &body.ManualPlan
		}
		if body.ManualRegisteredAt != "" {
			patch.ManualRegisteredAt = &body.ManualRegisteredAt
		}
		updated, err2 := profile.UpdateMeta(&cfg, name, patch)
		if err2 != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err2.Error()})
			return
		}
		p = updated
	}
	if body.UseNow {
		s.mu.Lock()
		next := *s.cfg
		next.ActiveProfile = name
		if config.Save(&next) == nil {
			s.swapConfigLocked(next)
		}
		s.mu.Unlock()
		p.Active = true
	}
	writeJSON(w, http.StatusOK, p)
}

type accountView struct {
	profile.Profile
	Usage             profile.UsageSnapshot `json:"usage"`
	RunningDesktopPID string                `json:"running_desktop_pid,omitempty"`
	CLICommand        string                `json:"cli_command"`
}

func findRunningPIDsForProfile(rep host.DesktopInspectReport, prof profile.Profile) []string {
	var pids []string
	for _, proc := range rep.Processes {
		cmd := proc.RawCommand
		if cmd == "" {
			cmd = proc.Command
		}
		if strings.Contains(cmd, "--user-data-dir="+prof.DesktopDir) {
			pids = append(pids, proc.PID)
		}
	}
	return pids
}

func pickRecommendedProfile(items []accountView) (name string, reason string) {
	if len(items) == 0 {
		return "", ""
	}
	bestIdx := -1
	bestScore := 1e9
	for i, it := range items {
		if it.Usage.FiveHour == nil && it.Usage.Weekly == nil {
			continue
		}
		fh := 0.0
		if it.Usage.FiveHour != nil {
			fh = it.Usage.FiveHour.Utilization
		}
		wk := 0.0
		if it.Usage.Weekly != nil {
			wk = it.Usage.Weekly.Utilization
		}
		score := wk*0.6 + fh*0.4
		if score < bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	if bestIdx >= 0 {
		it := items[bestIdx]
		return it.Name, "当前剩余额度最充裕，建议优先使用"
	}
	return items[0].Name, "当前默认隔离档案"
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()

	list, err := profile.List(&cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	refresh := r.URL.Query().Get("refresh") == "1"
	withUsage := r.URL.Query().Get("with_usage") != "0"

	rep := host.InspectRunningDesktop(&cfg)
	views := make([]accountView, len(list))

	var wg sync.WaitGroup
	for i, p := range list {
		pids := findRunningPIDsForProfile(rep, p)
		runningPID := ""
		if len(pids) > 0 {
			runningPID = pids[0]
		}
		cliCmd := "peaceclaude cli --profile " + p.Name
		if p.Name == cfg.ActiveProfile {
			cliCmd = "peaceclaude cli"
		}
		views[i] = accountView{
			Profile:           p,
			RunningDesktopPID: runningPID,
			CLICommand:        cliCmd,
			Usage:             profile.UsageSnapshot{Profile: p.Name, Status: "no_token", Source: "none"},
		}
		if withUsage {
			wg.Add(1)
			go func(idx int, prof profile.Profile) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
				defer cancel()
				views[idx].Usage = s.getProfileUsage(ctx, &cfg, &prof, refresh)
			}(i, p)
		}
	}
	wg.Wait()

	recName, recReason := pickRecommendedProfile(views)
	writeJSON(w, http.StatusOK, map[string]any{
		"active":                cfg.ActiveProfile,
		"single_active_desktop": cfg.Identity.SingleActiveDesktop,
		"accounts":              views,
		"recommended_profile":   recName,
		"recommended_reason":    recReason,
	})
}

func (s *Server) handleAccountDetail(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		name = cfg.ActiveProfile
	}
	p, err := profile.Load(&cfg, name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	refresh := r.URL.Query().Get("refresh") == "1"
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	usage := s.getProfileUsage(ctx, &cfg, p, refresh)

	rep := host.InspectRunningDesktop(&cfg)
	pids := findRunningPIDsForProfile(rep, *p)
	runningPID := ""
	if len(pids) > 0 {
		runningPID = pids[0]
	}
	cliCmd := "peaceclaude cli --profile " + p.Name
	if p.Name == cfg.ActiveProfile {
		cliCmd = "peaceclaude cli"
	}
	writeJSON(w, http.StatusOK, accountView{
		Profile:           *p,
		Usage:             usage,
		RunningDesktopPID: runningPID,
		CLICommand:        cliCmd,
	})
}

func (s *Server) handleAccountUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name               string  `json:"name"`
		DisplayName        *string `json:"display_name"`
		AvatarID           *string `json:"avatar_id"`
		Note               *string `json:"note"`
		ManualPlan         *string `json:"manual_plan"`
		ManualRegisteredAt *string `json:"manual_registered_at"`
		Timezone           *string `json:"timezone"`
		Language           *string `json:"language"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(body.Name)
	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()
	var lockedPtr *bool
	if body.ManualPlan != nil || body.ManualRegisteredAt != nil {
		locked := (body.ManualPlan != nil && strings.TrimSpace(*body.ManualPlan) != "") ||
			(body.ManualRegisteredAt != nil && strings.TrimSpace(*body.ManualRegisteredAt) != "")
		lockedPtr = &locked
	}
	updated, err := profile.UpdateMeta(&cfg, name, profile.MetaPatch{
		DisplayName:        body.DisplayName,
		AvatarID:           body.AvatarID,
		Note:               body.Note,
		ManualPlan:         body.ManualPlan,
		ManualRegisteredAt: body.ManualRegisteredAt,
		ManualLocked:       lockedPtr,
		Timezone:           body.Timezone,
		Language:           body.Language,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleAccountDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(body.Name)
	cfg := s.configSnapshot()
	p, loadErr := profile.Load(&cfg, name)
	if loadErr == nil {
		rep := host.InspectRunningDesktop(&cfg)
		if pids := findRunningPIDsForProfile(rep, *p); len(pids) > 0 {
			launcher.TerminatePIDs(pids, 3*time.Second)
		}
	}
	err := profile.Delete(&cfg, name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.usageMu.Lock()
	delete(s.usageCache, name)
	s.usageMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": name})
}

func (s *Server) handleAccountStopDesktop(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = cfg.ActiveProfile
	}
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
}

func (s *Server) handleProfileUse(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(body.Name)
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
	writeJSON(w, http.StatusOK, map[string]string{"active": name})
}

func (s *Server) handleProfileClean(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string `json:"name"`
		DryRun bool   `json:"dry_run"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = cfg.ActiveProfile
	}
	rep, err := profile.CleanProfile(&cfg, name, body.DryRun)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleRulesToggle(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Lock()
	next := *s.cfg
	st, err := profile.SyncAllRules(&next, body.Enabled)
	if err == nil {
		s.swapConfigLocked(next)
	}
	s.mu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	rep := host.RunDoctor(ctx, &cfg, true, s.gate.Snapshot())
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleDoctorFix(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CheckID string `json:"check_id"`
		Action  string `json:"action"`
		FixAll  bool   `json:"fix_all"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 55*time.Second)
	defer cancel()

	cfg := s.configSnapshot()
	var actions []string
	var errorsList []string
	didHardening := false
	didRecheck := false
	didTakeover := false
	didPF := false

	applyAction := func(act string) {
		switch act {
		case "enable_hardening":
			if didHardening {
				return
			}
			didHardening = true
			s.mu.Lock()
			next := *s.cfg
			next.Egress.FailClosed = true
			next.Shield.TelemetryShield = true
			next.Identity.SyncTimezone = true
			next.Identity.StripHostAnthropicEnv = true
			next.Identity.DisableDesktopQUIC = true
			next.Identity.BlockDesktopLocalDNS = true
			next.Identity.AutoPeaceDesktop = true
			if strings.TrimSpace(next.Identity.Language) == "" {
				next.Identity.Language = "en-US"
			}
			if strings.TrimSpace(next.Identity.Locale) == "" {
				next.Identity.Locale = "en_US.UTF-8"
			}
			if next.Listen.BindAddr == "" {
				next.Listen.BindAddr = "127.0.0.1"
			}
			_, _ = profile.Ensure(&next, next.ActiveProfile, "")
			if err := config.Save(&next); err != nil {
				errorsList = append(errorsList, "保存安全配置失败: "+err.Error())
			} else {
				s.swapConfigLocked(next)
				_ = os.Chmod(config.ConfigPath(), 0o600)
				actions = append(actions, "已一键开启 Fail-Closed 门禁、遥测护盾、时区同步、env 剥离与防 QUIC/UDP 泄漏加固")
			}
			s.mu.Unlock()
		case "enable_pac":
			if os.Getenv("PEACECLAUDE_HOME") == "" {
				if _, _, err := host.SetSystemBrowserPAC(&cfg, true, false); err != nil {
					errorsList = append(errorsList, "开启浏览器 PAC 失败: "+err.Error())
				} else {
					s.mu.Lock()
					next := *s.cfg
					autoTrue := true
					next.Listen.AutoEnableBrowserPAC = &autoTrue
					next.Listen.BrowserPACPaused = false
					if config.Save(&next) == nil {
						s.swapConfigLocked(next)
					}
					s.mu.Unlock()
					actions = append(actions, "已开启系统普通浏览器全局 PAC 接管")
				}
			}
		case "takeover_desktop":
			if didTakeover {
				return
			}
			didTakeover = true
			term, pid, blocked, err := s.TakeoverAllExposed(ctx)
			if err != nil {
				errorsList = append(errorsList, "接管原生 Desktop 失败: "+err.Error())
			} else if blocked != "" {
				errorsList = append(errorsList, "已停止未隔离进程但门禁锁定暂未重启: "+blocked)
			} else {
				actions = append(actions, fmt.Sprintf("已接管 %d 个原生 Desktop 实例并启动隔离窗口 (PID=%d)", term, pid))
			}
		case "recheck_egress":
			if didRecheck {
				return
			}
			didRecheck = true
			s.gate.SetManualLock(false, "")
			res := s.gate.Check(ctx, false)
			if res.OK {
				actions = append(actions, fmt.Sprintf("出口复检通过：%s (%s · %dms)", res.EgressIP, res.Country, res.LatencyMs))
			} else {
				errorsList = append(errorsList, "出口重检仍异常: "+res.FailureReason)
			}
		case "install_pf_and_pac":
			if os.Getenv("PEACECLAUDE_HOME") == "" {
				if _, _, err := host.SetSystemBrowserPAC(&cfg, true, false); err == nil {
					s.mu.Lock()
					next := *s.cfg
					autoTrue := true
					next.Listen.AutoEnableBrowserPAC = &autoTrue
					next.Listen.BrowserPACPaused = false
					if config.Save(&next) == nil {
						s.swapConfigLocked(next)
					}
					s.mu.Unlock()
					actions = append(actions, "已自动开启浏览器全局 PAC")
				}
			}
			fallthrough
		case "install_pf":
			if didPF {
				return
			}
			didPF = true
			if os.Getenv("PEACECLAUDE_HOME") != "" {
				actions = append(actions, "沙盒环境跳过 macOS 管理员弹窗授权，请在实际宿主使用 `sudo bash install-pf.sh`")
				return
			}
			if _, err := host.InstallPFPrivileged(ctx, &cfg); err != nil {
				errorsList = append(errorsList, err.Error())
			} else {
				actions = append(actions, "已通过管理员授权成功安装并激活 macOS pf 防直连与 IPv6 阻断规则")
			}
		case "repair_binaries":
			_, _ = profile.Ensure(&cfg, cfg.ActiveProfile, "")
			_, _, _, _ = host.WritePFArtifacts(&cfg)
			actions = append(actions, "已补齐隔离目录与 pf 脚本产物（若缺 CLI 或 Claude.app 请确认系统安装路径）")
		default:
			if act != "" {
				errorsList = append(errorsList, "未知修复动作: "+act)
			}
		}
	}

	if body.FixAll {
		// Automatically apply non-disruptive fixes, and also enable PAC + takeover if needed.
		curRep := host.RunDoctor(ctx, &cfg, true, s.gate.Snapshot())
		for _, ck := range curRep.Checks {
			if ck.Status == "pass" || !ck.Fixable {
				continue
			}
			if ck.FixAction == "install_pf" || ck.FixAction == "install_pf_and_pac" {
				// Enable PAC if needed; leave privileged pf prompt to explicit click unless requested via check_id.
				if ck.FixAction == "install_pf_and_pac" {
					applyAction("enable_pac")
				}
				continue
			}
			applyAction(ck.FixAction)
		}
	} else {
		act := strings.TrimSpace(body.Action)
		if act == "" && strings.TrimSpace(body.CheckID) != "" {
			curRep := host.RunDoctor(ctx, &cfg, true, s.gate.Snapshot())
			for _, ck := range curRep.Checks {
				if ck.ID == body.CheckID {
					act = ck.FixAction
					break
				}
			}
		}
		if act == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing check_id or action"})
			return
		}
		applyAction(act)
	}

	cfgAfter := s.configSnapshot()
	freshRep := host.RunDoctor(ctx, &cfgAfter, true, s.gate.Snapshot())
	statusCode := http.StatusOK
	errMsg := ""
	if len(errorsList) > 0 {
		errMsg = strings.Join(errorsList, "；")
		if len(actions) == 0 {
			statusCode = http.StatusPreconditionFailed
		}
	}
	writeJSON(w, statusCode, map[string]any{
		"ok":      len(errorsList) == 0,
		"error":   errMsg,
		"actions": actions,
		"errors":  errorsList,
		"doctor":  freshRep,
	})
}

func (s *Server) handleDiagnose(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	rep := host.Diagnose(ctx, &cfg, s.gate.Snapshot())
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handlePauseAndRecheck(w http.ResponseWriter, r *http.Request) {
	// 1. Immediately pause outbound traffic & cut active protected connections.
	s.gate.SetManualLock(true, "暂停外网执行全量 11 项安全重检")
	closed := s.gateway.CloseProtectedConns()

	// 2. Unlock manual pause and run fresh egress probe; gate stays locked if probe fails.
	s.gate.SetManualLock(false, "")
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	probeRes := s.gate.Check(ctx, false)

	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()
	doc := host.RunDoctor(ctx, &cfg, true, s.gate.Snapshot())

	writeJSON(w, http.StatusOK, map[string]any{
		"closed_conns": closed,
		"probe":        probeRes,
		"gate":         s.gate.Snapshot(),
		"doctor":       doc,
	})
}

func (s *Server) handleInspectDesktop(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, host.InspectRunningDesktop(&cfg))
}

func (s *Server) handleDesktopTakeover(w http.ResponseWriter, r *http.Request) {
	term, pid, blocked, err := s.TakeoverAllExposed(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if blocked != "" {
		writeJSON(w, http.StatusPreconditionFailed, map[string]any{
			"terminated": term,
			"blocked":    true,
			"error":      "已关闭未隔离原生 Claude.app，但当前出口门禁已锁定，暂未重开: " + blocked,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"terminated":     term,
		"relaunched_pid": pid,
	})
}

func (s *Server) handleLaunchDesktop(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Profile    string `json:"profile"`
		SetActive  bool   `json:"set_active"`
		KeepOthers *bool  `json:"keep_others"`
	}
	_ = decodeJSON(r, &body)
	if body.SetActive && strings.TrimSpace(body.Profile) != "" {
		s.mu.Lock()
		next := *s.cfg
		if _, err := profile.Ensure(&next, strings.TrimSpace(body.Profile), ""); err == nil {
			next.ActiveProfile = strings.TrimSpace(body.Profile)
			if config.Save(&next) == nil {
				s.swapConfigLocked(next)
			}
		}
		s.mu.Unlock()
	}
	cfg := s.configSnapshot()

	exclusive := cfg.Identity.SingleActiveDesktop
	if body.KeepOthers != nil {
		exclusive = !*body.KeepOthers
	}
	spec, pid, stoppedOthers, err := launcher.LaunchDesktopWithOptions(r.Context(), &cfg, s.selfBin, body.Profile, launcher.DesktopLaunchOptions{
		ExclusiveSwitch: exclusive,
	})
	if err != nil {
		writeJSON(w, http.StatusPreconditionFailed, map[string]string{"error": err.Error()})
		return
	}
	profile.TouchLastUsed(spec.Profile)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"pid":            pid,
		"profile":        spec.Profile,
		"data_dir":       spec.DataDir,
		"stopped_others": stoppedOthers,
	})
}

func (s *Server) handleLaunchBrowser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Profile string `json:"profile"`
		URL     string `json:"url"`
	}
	_ = decodeJSON(r, &body)
	cfg := s.configSnapshot()

	targetURL := strings.TrimSpace(body.URL)
	if targetURL == "" {
		targetURL = "https://claude.ai/login"
	}
	browser, pid, _, err := launcher.LaunchLoginBrowserWithPID(r.Context(), &cfg, s.selfBin, body.Profile, targetURL, false)
	if err != nil {
		writeJSON(w, http.StatusPreconditionFailed, map[string]string{"error": err.Error()})
		return
	}
	profName := strings.TrimSpace(body.Profile)
	if profName == "" {
		profName = cfg.ActiveProfile
	}
	profile.TouchLastUsed(profName)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"browser": browser,
		"pid":     pid,
		"profile": profName,
		"url":     targetURL,
	})
}

func (s *Server) handleBrowserPAC(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled *bool `json:"enabled"`
		Enable  *bool `json:"enable"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var wantEnabled bool
	switch {
	case body.Enabled != nil:
		wantEnabled = *body.Enabled
	case body.Enable != nil:
		wantEnabled = *body.Enable
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing required boolean field: enabled"})
		return
	}
	cfg := s.configSnapshot()
	st, _, err := host.SetSystemBrowserPAC(&cfg, wantEnabled, false)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Lock()
	next := *s.cfg
	autoTrue := true
	if wantEnabled {
		next.Listen.AutoEnableBrowserPAC = &autoTrue
		next.Listen.BrowserPACPaused = false
	} else {
		next.Listen.BrowserPACPaused = true
	}
	if config.Save(&next) == nil {
		s.swapConfigLocked(next)
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UpstreamMode          string   `json:"upstream_mode"`
		UpstreamAddress       string   `json:"upstream_address"`
		UpstreamUsername      string   `json:"upstream_username"`
		UpstreamPassword      string   `json:"upstream_password"`
		ExpectedIP            string   `json:"expected_ip"`
		AllowedCountries      []string `json:"allowed_countries"`
		Language              string   `json:"language"`
		Timezone              string   `json:"timezone"`
		FailClosed            *bool    `json:"fail_closed"`
		SyncTimezone          *bool    `json:"sync_timezone"`
		TelemetryShield       *bool    `json:"telemetry_shield"`
		StrictDomainIsolation *bool    `json:"strict_domain_isolation"`
		SkipProbeDuringSSE    *bool    `json:"skip_probe_during_sse"`
		StripHostAnthropicEnv *bool    `json:"strip_host_anthropic_env"`
		AllowCorporateDirect  *bool    `json:"allow_corporate_direct"`
		ProxyAuthDomains      *bool    `json:"proxy_auth_domains"`
		VMProxyEnabled        *bool    `json:"vm_proxy_enabled"`
		AutoPeaceDesktop      *bool    `json:"auto_peace_desktop"`
		SingleActiveDesktop   *bool    `json:"single_active_desktop"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	s.mu.Lock()
	next := *s.cfg
	if body.UpstreamMode != "" {
		next.Upstream.Mode = body.UpstreamMode
	}
	if body.UpstreamAddress != "" {
		next.Upstream.Address = body.UpstreamAddress
	}
	if body.UpstreamUsername != "" {
		next.Upstream.Username = body.UpstreamUsername
	}
	if body.UpstreamPassword != "" {
		next.Upstream.Password = body.UpstreamPassword
	}
	next.Egress.ExpectedIP = strings.TrimSpace(body.ExpectedIP)
	if body.AllowedCountries != nil {
		next.Egress.AllowedCountries = body.AllowedCountries
	}
	if body.Language != "" {
		next.Identity.Language = body.Language
	}
	next.Identity.Timezone = strings.TrimSpace(body.Timezone)
	if body.FailClosed != nil {
		next.Egress.FailClosed = *body.FailClosed
	}
	if body.SyncTimezone != nil {
		next.Identity.SyncTimezone = *body.SyncTimezone
	}
	if body.TelemetryShield != nil {
		next.Shield.TelemetryShield = *body.TelemetryShield
	}
	if body.StrictDomainIsolation != nil {
		next.Egress.StrictDomainIsolation = *body.StrictDomainIsolation
	}
	if body.SkipProbeDuringSSE != nil {
		next.Egress.SkipProbeDuringSSE = *body.SkipProbeDuringSSE
	}
	if body.StripHostAnthropicEnv != nil {
		next.Identity.StripHostAnthropicEnv = *body.StripHostAnthropicEnv
	}
	if body.AllowCorporateDirect != nil {
		next.Shield.AllowCorporateDirect = *body.AllowCorporateDirect
	}
	if body.ProxyAuthDomains != nil {
		next.Shield.ProxyAuthDomains = *body.ProxyAuthDomains
	}
	if body.VMProxyEnabled != nil {
		next.Listen.VMProxyEnabled = *body.VMProxyEnabled
	}
	if body.AutoPeaceDesktop != nil {
		next.Identity.AutoPeaceDesktop = *body.AutoPeaceDesktop
	}
	if body.SingleActiveDesktop != nil {
		next.Identity.SingleActiveDesktop = *body.SingleActiveDesktop
	}

	if err := config.Save(&next); err != nil {
		s.mu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.swapConfigLocked(next)
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	res := s.gate.Check(ctx, false)
	cancel()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": next.Redacted(),
		"probe":  res,
		"gate":   s.gate.Snapshot(),
	})
}

func (s *Server) handleShutdown(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"shutting_down": true})
	go func() {
		time.Sleep(100 * time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.Shutdown(ctx)
	}()
}

func (s *Server) handlePAC(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := *s.cfg
	s.mu.RUnlock()
	mode := r.URL.Query().Get("mode")
	pac := proxy.RenderPAC(&cfg, mode)
	w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, pac)
}
