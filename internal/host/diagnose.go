package host

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"peaceclaude/internal/config"
	"peaceclaude/internal/egress"
	"peaceclaude/internal/upstream"
)

type EndpointProbe struct {
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	LatencyMs  int64  `json:"latency_ms"`
	Error      string `json:"error,omitempty"`
}

type DiagnoseReport struct {
	GeneratedAt    time.Time       `json:"generated_at"`
	Classification string          `json:"classification"` // healthy | network | auth | config | unknown
	Summary        string          `json:"summary"`
	Recommendation string          `json:"recommendation"`
	Egress         egress.ProbeResult `json:"egress"`
	Endpoints      []EndpointProbe `json:"endpoints"`
	Evidence       []string        `json:"evidence"`
	LogsScanned    []string        `json:"logs_scanned"`
}

// Diagnose inspects recent Claude Desktop / CLI / PeaceClaude logs + live endpoint reachability
// and classifies 403/auth/network issues into healthy|network|auth|config|unknown (modeled on claude-code-diagnose).
func Diagnose(ctx context.Context, cfg *config.Config, gateSnap egress.GateSnapshot) DiagnoseReport {
	rep := DiagnoseReport{
		GeneratedAt: time.Now().UTC(),
		Egress:      gateSnap.LastProbe,
	}

	// 1. Live probe of 3 Anthropic endpoints strictly via configured upstream.
	dialer := upstream.FromConfig(cfg.Upstream)
	client := dialer.NewHTTPClient(8 * time.Second)
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	urls := []string{
		"https://api.anthropic.com/",
		"https://claude.ai/cdn-cgi/trace",
		"https://platform.claude.com/oauth/authorize",
	}
	reachableCount := 0
	for _, u := range urls {
		start := time.Now()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0")
		resp, err := client.Do(req)
		ep := EndpointProbe{URL: u, LatencyMs: time.Since(start).Milliseconds()}
		if err != nil {
			ep.Error = err.Error()
		} else {
			ep.StatusCode = resp.StatusCode
			_ = resp.Body.Close()
			if resp.StatusCode > 0 {
				reachableCount++
			}
		}
		rep.Endpoints = append(rep.Endpoints, ep)
	}

	// 2. Scan recent lines of Claude & PeaceClaude logs for real evidence.
	home, _ := os.UserHomeDir()
	logFiles := []string{
		config.DaemonLogPath(),
		filepath.Join(home, "Library/Logs/Claude/main.log"),
		filepath.Join(home, "Library/Logs/Claude/daemon.log"),
		filepath.Join(home, "Library/Logs/Claude/claude.ai-web.log"),
		filepath.Join(home, "Library/Logs/Claude/mcp.log"),
	}
	netSignals := []string{"ERR_INTERNET_DISCONNECTED", "isOnline() is false", "ECONNRESET", "ETIMEDOUT", "ECONNREFUSED", "fail-closed", "egress IP mismatch"}
	authSignals := []string{"invalid_grant", "proactive refresh failed", "token expired", "token revoked", "[oauth] fail", "refresh_token"}
	unknownSignals := []string{"403", "Request not allowed", "Authentication failed", "Failed to fetch"}

	var netHits, authHits, unknownHits []string
	for _, lf := range logFiles {
		lines := tailLines(lf, 80)
		if len(lines) == 0 {
			continue
		}
		rep.LogsScanned = append(rep.LogsScanned, lf)
		base := filepath.Base(lf)
		for _, ln := range lines {
			low := strings.ToLower(ln)
			for _, sig := range authSignals {
				if strings.Contains(low, strings.ToLower(sig)) {
					authHits = append(authHits, fmt.Sprintf("[%s] %s", base, truncateLine(ln, 180)))
					break
				}
			}
			for _, sig := range netSignals {
				if strings.Contains(low, strings.ToLower(sig)) {
					netHits = append(netHits, fmt.Sprintf("[%s] %s", base, truncateLine(ln, 180)))
					break
				}
			}
			for _, sig := range unknownSignals {
				if strings.Contains(low, strings.ToLower(sig)) {
					unknownHits = append(unknownHits, fmt.Sprintf("[%s] %s", base, truncateLine(ln, 180)))
					break
				}
			}
		}
	}

	// 3. Classify using evidence priority.
	hostBaseURL := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL"))
	if strings.Contains(hostBaseURL, "127.0.0.1") || strings.Contains(hostBaseURL, "localhost") {
		rep.Evidence = append(rep.Evidence, "当前 Shell 存在 ANTHROPIC_BASE_URL="+hostBaseURL+"（请使用 `peaceclaude cli` 自动剥离）")
	}

	switch {
	case reachableCount == 0 || !gateSnap.Open:
		rep.Classification = "network"
		rep.Summary = "出口门禁锁定或上游代理不可达，属于网络/代理侧问题。"
		rep.Recommendation = "请勿退出登录（Sign Out 伤 Token）！检查 Webshare 静态 IP / Clash Verge 代理节点是否正常，然后运行 `peaceclaude service restart` 或等待 30 秒重试。"
		rep.Evidence = append(rep.Evidence, takeLast(netHits, 6)...)
		if gateSnap.LockReason != "" {
			rep.Evidence = append(rep.Evidence, "Egress Gate LockReason: "+gateSnap.LockReason)
		}
	case len(authHits) > 0:
		rep.Classification = "auth"
		rep.Summary = "日志中出现明确的 OAuth / Token 刷新失败信号。"
		rep.Recommendation = "确认 `peaceclaude status` 绿灯后，在隔离的 Claude Desktop / `peaceclaude cli` 内执行 Sign Out -> Sign In 重新走隔离浏览器 OAuth 授权。"
		rep.Evidence = append(rep.Evidence, takeLast(authHits, 6)...)
	case len(netHits) > 0:
		rep.Classification = "network"
		rep.Summary = "近期日志存在网络断开或连接重置信号，但当前核心端点已恢复可达。"
		rep.Recommendation = "多为代理瞬时抖动导致，无需退出登录；直接重试当前对话即可。"
		rep.Evidence = append(rep.Evidence, takeLast(netHits, 6)...)
	case len(unknownHits) > 0:
		rep.Classification = "unknown"
		rep.Summary = "存在 403 / Failed to fetch 记录，但无明确断网或 OAuth 失效证据。"
		rep.Recommendation = "先开启 `peaceclaude capture on` 复现一次请求，再执行 `peaceclaude capture list` 查看具体域名与返回码定位原因。"
		rep.Evidence = append(rep.Evidence, takeLast(unknownHits, 6)...)
	default:
		rep.Classification = "healthy"
		rep.Summary = fmt.Sprintf("三大 Anthropic 核心端点全部可达 (%d/3)，出口 IP=%s (%s)，未发现近期异常信号。",
			reachableCount, gateSnap.LastProbe.EgressIP, gateSnap.LastProbe.Country)
		rep.Recommendation = "当前链路健康，可直接通过 `peaceclaude cli` 或 `PeaceClaude.app` 安心使用。"
	}
	return rep
}

func tailLines(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var buf []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 512*1024)
	for sc.Scan() {
		buf = append(buf, sc.Text())
		if len(buf) > n*2 {
			buf = buf[len(buf)-n:]
		}
	}
	if len(buf) > n {
		buf = buf[len(buf)-n:]
	}
	return buf
}

func takeLast(items []string, n int) []string {
	if len(items) <= n {
		return items
	}
	return items[len(items)-n:]
}

func truncateLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
