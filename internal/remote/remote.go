package remote

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"peaceclaude/internal/config"
)

const (
	PairTTL          = 2 * time.Minute
	maxPairFailCount = 5
)

type HostAddress struct {
	Interface string `json:"interface"`
	IP        string `json:"ip"`
	Kind      string `json:"kind"` // tailscale | lan
}

type PairTicket struct {
	Code      string    `json:"code"`
	URL       string    `json:"url"`
	SVG       string    `json:"svg"`
	ExpiresAt time.Time `json:"expires_at"`
	Mode      string    `json:"mode"`
	Warning   string    `json:"warning,omitempty"`
}

type RemoteStatus struct {
	Enabled        bool                  `json:"enabled"`
	RelayMode      string                `json:"relay_mode"`
	CustomRelayURL string                `json:"custom_relay_url,omitempty"`
	HasAdminToken  bool                  `json:"has_admin_token"`
	MobilePort     int                   `json:"mobile_port"`
	ListenAddr     string                `json:"listen_addr,omitempty"`
	HostAddresses  []HostAddress         `json:"host_addresses"`
	PairedDevices  []config.PairedDevice `json:"paired_devices"`
	ActivePair     *PairTicket           `json:"active_pair,omitempty"`
	RelayCheck     *RelayHealth          `json:"relay_check,omitempty"`
}

type RelayHealth struct {
	OK        bool      `json:"ok"`
	URL       string    `json:"url"`
	Detail    string    `json:"detail"`
	CheckedAt time.Time `json:"checked_at"`
}

type Manager struct {
	mu            sync.RWMutex
	pendingCode   string
	pendingURL    string
	pendingSVG    string
	pendingMode   string
	pendingWarn   string
	pendingExp    time.Time
	pendingFails  int
	lastPersistAt time.Time
	devices       []config.PairedDevice
	activeTokens  map[string]config.PairedDevice // tokenHash -> device
	lastHealth    *RelayHealth
}

func NewManager() *Manager {
	m := &Manager{
		activeTokens: map[string]config.PairedDevice{},
	}
	m.ReloadState()
	return m
}

func stateFilePath() string {
	return filepath.Join(config.StateDir(), "remote_devices.json")
}

// Note: TokenHash in config.PairedDevice uses json:"-"; keep dedicated JSON carrier for persisted state.
type persistedDevice struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	OS         string    `json:"os"`
	TokenHash  string    `json:"token_hash"`
	PairedAt   time.Time `json:"paired_at"`
	LastSeenAt time.Time `json:"last_seen_at,omitempty"`
}

type persistedFile struct {
	Devices []persistedDevice `json:"devices"`
}

func (m *Manager) saveStateLocked() {
	_ = config.EnsureDirs()
	pf := persistedFile{Devices: make([]persistedDevice, 0, len(m.devices))}
	for _, d := range m.devices {
		pf.Devices = append(pf.Devices, persistedDevice{
			ID:         d.ID,
			Name:       d.Name,
			OS:         d.OS,
			TokenHash:  d.TokenHash,
			PairedAt:   d.PairedAt,
			LastSeenAt: d.LastSeenAt,
		})
	}
	raw, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return
	}
	tmp := stateFilePath() + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, stateFilePath())
	}
}

func (m *Manager) ReloadState() {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, err := os.ReadFile(stateFilePath())
	if err != nil {
		return
	}
	var pf persistedFile
	if json.Unmarshal(raw, &pf) != nil {
		return
	}
	m.devices = make([]config.PairedDevice, 0, len(pf.Devices))
	m.activeTokens = make(map[string]config.PairedDevice, len(pf.Devices))
	for _, pd := range pf.Devices {
		d := config.PairedDevice{
			ID:         pd.ID,
			Name:       pd.Name,
			OS:         pd.OS,
			TokenHash:  pd.TokenHash,
			PairedAt:   pd.PairedAt,
			LastSeenAt: pd.LastSeenAt,
		}
		m.devices = append(m.devices, d)
		if d.TokenHash != "" {
			m.activeTokens[d.TokenHash] = d
		}
	}
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

func randomSecret(nbytes int) (string, error) {
	buf := make([]byte, nbytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// DiscoverHostAddresses returns non-loopback IPv4 addresses reachable from LAN/Tailscale peers.
func DiscoverHostAddresses() []HostAddress {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []HostAddress
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			ip4 := ip.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}
			kind := "lan"
			// Tailscale CGNAT 100.64.0.0/10
			if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
				kind = "tailscale"
			}
			out = append(out, HostAddress{
				Interface: ifc.Name,
				IP:        ip4.String(),
				Kind:      kind,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == "tailscale"
		}
		return out[i].Interface < out[j].Interface
	})
	return out
}

func isForbiddenRelayIP(ip netip.Addr) bool {
	ip = ip.Unmap().WithZone("")
	return ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

func isLoopbackHostname(host string) bool {
	h := strings.ToLower(strings.Trim(host, "[]"))
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

func ValidateRelayURL(raw string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return "", nil
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("中继地址格式无效，请使用纯 HTTPS 源地址（如 https://relay.example.com）")
	}
	host := strings.Trim(u.Hostname(), "[]")
	isLoop := isLoopbackHostname(host)
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoop) {
		return "", errors.New("中继地址必须使用 HTTPS（仅回环调试允许 HTTP）")
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if !isLoop && (addr.IsLoopback() || isForbiddenRelayIP(addr)) {
			return "", errors.New("出于防 SSRF 安全限制，自建中继地址禁止指向内网或云元数据 IP")
		}
	}
	return u.String(), nil
}

// CheckRelayHealth probes GET <relayURL>/healthz using a direct non-proxied HTTP transport
// while blocking DNS rebinding or redirects into private/link-local/metadata IP ranges.
func (m *Manager) CheckRelayHealth(ctx context.Context, rawURL string) RelayHealth {
	norm, err := ValidateRelayURL(rawURL)
	if err != nil || norm == "" {
		msg := "请先配置有效的自建中继地址"
		if err != nil {
			msg = err.Error()
		}
		res := RelayHealth{OK: false, URL: rawURL, Detail: msg, CheckedAt: time.Now().UTC()}
		m.mu.Lock()
		m.lastHealth = &res
		m.mu.Unlock()
		return res
	}
	parsedURL, _ := url.Parse(norm)
	allowLoop := isLoopbackHostname(parsedURL.Hostname())

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, norm+"/healthz", nil)
	if err != nil {
		res := RelayHealth{OK: false, URL: norm, Detail: err.Error(), CheckedAt: time.Now().UTC()}
		m.mu.Lock()
		m.lastHealth = &res
		m.mu.Unlock()
		return res
	}
	safeDial := &net.Dialer{Timeout: 5 * time.Second}
	tr := &http.Transport{
		Proxy:             nil,
		DisableKeepAlives: true,
		DialContext: func(dctx context.Context, network, addr string) (net.Conn, error) {
			h, p, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(dctx, h)
			if err != nil {
				return nil, err
			}
			for _, ipAddr := range ips {
				na, ok := netip.AddrFromSlice(ipAddr.IP)
				if !ok {
					continue
				}
				na = na.Unmap().WithZone("")
				if na.IsLoopback() {
					if !allowLoop {
						return nil, errors.New("中继域名解析至回环地址被拒绝 (SSRF guard)")
					}
				} else if isForbiddenRelayIP(na) {
					return nil, fmt.Errorf("中继域名解析至受限内网 IP %s 被拒绝 (SSRF guard)", na.String())
				}
				return safeDial.DialContext(dctx, network, net.JoinHostPort(na.String(), p))
			}
			return nil, fmt.Errorf("未解析到可用的安全 IP: %s", h)
		},
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{
		Transport: tr,
		Timeout:   5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		res := RelayHealth{OK: false, URL: norm, Detail: "连接失败: " + err.Error(), CheckedAt: time.Now().UTC()}
		m.mu.Lock()
		m.lastHealth = &res
		m.mu.Unlock()
		return res
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		res := RelayHealth{OK: true, URL: norm, Detail: fmt.Sprintf("中继就绪 (HTTP %d)", resp.StatusCode), CheckedAt: time.Now().UTC()}
		m.mu.Lock()
		m.lastHealth = &res
		m.mu.Unlock()
		return res
	}
	res := RelayHealth{OK: false, URL: norm, Detail: fmt.Sprintf("中继返回异常状态 HTTP %d", resp.StatusCode), CheckedAt: time.Now().UTC()}
	m.mu.Lock()
	m.lastHealth = &res
	m.mu.Unlock()
	return res
}

func (m *Manager) Status(cfg *config.Config, listenAddr string) RemoteStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var pair *PairTicket
	if m.pendingCode != "" && time.Now().Before(m.pendingExp) {
		pair = &PairTicket{
			Code:      m.pendingCode,
			URL:       m.pendingURL,
			SVG:       m.pendingSVG,
			ExpiresAt: m.pendingExp,
			Mode:      m.pendingMode,
			Warning:   m.pendingWarn,
		}
	}
	devs := make([]config.PairedDevice, len(m.devices))
	copy(devs, m.devices)
	var healthCopy *RelayHealth
	if m.lastHealth != nil {
		hc := *m.lastHealth
		healthCopy = &hc
	}
	return RemoteStatus{
		Enabled:        cfg.Remote.Enabled,
		RelayMode:      cfg.Remote.RelayMode,
		CustomRelayURL: cfg.Remote.CustomRelayURL,
		HasAdminToken:  cfg.Remote.RelayAdminToken != "",
		MobilePort:     cfg.Remote.MobilePort,
		ListenAddr:     listenAddr,
		HostAddresses:  DiscoverHostAddresses(),
		PairedDevices:  devs,
		ActivePair:     pair,
		RelayCheck:     healthCopy,
	}
}

// CreatePairTicket issues a single-use 120-second pairing code & scannable SVG QR payload.
func (m *Manager) CreatePairTicket(ctx context.Context, cfg *config.Config, hostOverride string) (*PairTicket, error) {
	if cfg.Remote.RelayMode == "official" {
		return nil, errors.New("PeaceClaude 为开源零虚拟机本地网关，尚无托管商业官方公网中继；请选用「本机局域网 / Tailscale 直连」或配置「自建兼容中继」")
	}

	secret, err := randomSecret(24)
	if err != nil {
		return nil, err
	}
	pairCode := "PEACE-" + secret
	var qrPayload, warn string

	if cfg.Remote.RelayMode == "custom" {
		norm, verr := ValidateRelayURL(cfg.Remote.CustomRelayURL)
		if verr != nil || norm == "" {
			if verr != nil {
				return nil, verr
			}
			return nil, errors.New("请先填写自建兼容中继地址")
		}
		h := m.CheckRelayHealth(ctx, norm)
		if !h.OK {
			return nil, fmt.Errorf("自建中继不可达（%s），已拒绝生成虚假配对码", h.Detail)
		}
		qrPayload = fmt.Sprintf(`PEACE1:{"r":%q,"c":%q}`, norm, pairCode)
		warn = "已通过自建中继健康校验（注：完整 WebSocket/mTLS 手机原生端接入依赖自建终端或配套手机客户端）。"
	} else {
		// LAN / Tailscale direct mobile web controller URL
		host := strings.TrimSpace(hostOverride)
		if host == "" {
			addrs := DiscoverHostAddresses()
			if len(addrs) > 0 {
				host = addrs[0].IP
			}
		}
		if host == "" {
			host = "127.0.0.1"
			warn = "未检测到局域网或 Tailscale IPv4，当前二维码仅限本机测试，请连接 Wi-Fi 或开启 Tailscale 后刷新。"
		}
		port := cfg.Remote.MobilePort
		if port <= 0 {
			port = 18900
		}
		qrPayload = fmt.Sprintf("http://%s:%d/mobile#pair=%s", host, port, pairCode)
	}

	svg, err := RenderQRSVG(qrPayload)
	if err != nil {
		return nil, err
	}
	exp := time.Now().UTC().Add(PairTTL)

	m.mu.Lock()
	m.pendingCode = pairCode
	m.pendingURL = qrPayload
	m.pendingSVG = svg
	m.pendingMode = cfg.Remote.RelayMode
	m.pendingWarn = warn
	m.pendingExp = exp
	m.pendingFails = 0
	m.mu.Unlock()

	return &PairTicket{
		Code:      pairCode,
		URL:       qrPayload,
		SVG:       svg,
		ExpiresAt: exp,
		Mode:      cfg.Remote.RelayMode,
		Warning:   warn,
	}, nil
}

// RedeemPair verifies a one-time pairing code and issues a revocable 256-bit bearer token.
func (m *Manager) RedeemPair(code, deviceName, osName string) (string, *config.PairedDevice, error) {
	code = strings.TrimSpace(code)
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pendingCode == "" || !time.Now().Before(m.pendingExp) {
		m.pendingCode = ""
		m.pendingSVG = ""
		m.pendingURL = ""
		m.pendingFails = 0
		return "", nil, errors.New("配对码无效或已过期，请在电脑端重新生成")
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(m.pendingCode)) != 1 {
		m.pendingFails++
		if m.pendingFails >= maxPairFailCount {
			m.pendingCode = ""
			m.pendingSVG = ""
			m.pendingURL = ""
			m.pendingFails = 0
			return "", nil, errors.New("配对码连续输错次数过多，已自动作废，请在电脑端重新生成")
		}
		return "", nil, errors.New("配对码不匹配")
	}
	// Burn one-time pairing code immediately
	m.pendingCode = ""
	m.pendingSVG = ""
	m.pendingURL = ""
	m.pendingFails = 0

	bearer, err := randomSecret(32)
	if err != nil {
		return "", nil, err
	}
	devID, err := randomSecret(9)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(deviceName) == "" {
		deviceName = "手机浏览器"
	}
	if strings.TrimSpace(osName) == "" {
		osName = "Mobile Web"
	}
	now := time.Now().UTC()
	dev := config.PairedDevice{
		ID:         devID,
		Name:       strings.TrimSpace(deviceName),
		OS:         strings.TrimSpace(osName),
		TokenHash:  hashToken(bearer),
		PairedAt:   now,
		LastSeenAt: now,
	}
	m.devices = append(m.devices, dev)
	m.activeTokens[dev.TokenHash] = dev
	m.saveStateLocked()
	return bearer, &dev, nil
}

// AuthenticateBearer validates a mobile bearer token using constant-time SHA-256 matching.
func (m *Manager) AuthenticateBearer(token string) (config.PairedDevice, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return config.PairedDevice{}, false
	}
	h := hashToken(token)
	m.mu.Lock()
	defer m.mu.Unlock()
	dev, ok := m.activeTokens[h]
	if !ok {
		return config.PairedDevice{}, false
	}
	now := time.Now().UTC()
	dev.LastSeenAt = now
	m.activeTokens[h] = dev
	for i := range m.devices {
		if m.devices[i].ID == dev.ID {
			m.devices[i].LastSeenAt = now
			break
		}
	}
	if now.Sub(m.lastPersistAt) >= 30*time.Second {
		m.lastPersistAt = now
		m.saveStateLocked()
	}
	return dev, true
}

// RevokeAll clears all pending pairing tickets and paired mobile sessions immediately.
func (m *Manager) RevokeAll() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.devices)
	m.pendingCode = ""
	m.pendingURL = ""
	m.pendingSVG = ""
	m.devices = nil
	m.activeTokens = map[string]config.PairedDevice{}
	m.saveStateLocked()
	return n
}

func (m *Manager) RevokeDevice(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, d := range m.devices {
		if d.ID == id {
			delete(m.activeTokens, d.TokenHash)
			m.devices = append(m.devices[:i], m.devices[i+1:]...)
			m.saveStateLocked()
			return true
		}
	}
	return false
}
