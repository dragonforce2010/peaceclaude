package host

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type ClashStatus struct {
	Available          bool     `json:"available"`
	SocketPath         string   `json:"socket_path,omitempty"`
	MixedPort          int      `json:"mixed_port,omitempty"`
	Mode               string   `json:"mode,omitempty"`
	IPv6               bool     `json:"ipv6"`
	TUNEnabled         bool     `json:"tun_enabled"`
	AnthropicGroupType string   `json:"anthropic_group_type,omitempty"`
	AnthropicNow       string   `json:"anthropic_now,omitempty"`
	AnthropicChoices   []string `json:"anthropic_choices,omitempty"`
	Error              string   `json:"error,omitempty"`
}

func InspectClash(configuredSock string) ClashStatus {
	candidates := []string{}
	if strings.TrimSpace(configuredSock) != "" {
		candidates = append(candidates, strings.TrimSpace(configuredSock))
	}
	uid := strconv.Itoa(os.Getuid())
	candidates = append(candidates,
		fmt.Sprintf("/var/run/clash-verge-service/users/%s/verge-mihomo.sock", uid),
		"/tmp/verge/verge-mihomo.sock",
	)

	var sock string
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c] {
			continue
		}
		seen[c] = true
		if fi, err := os.Stat(c); err == nil && (fi.Mode()&os.ModeSocket != 0) {
			sock = c
			break
		}
	}
	if sock == "" {
		return ClashStatus{Available: false, Error: "mihomo unix socket not found"}
	}

	st := ClashStatus{SocketPath: sock}
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sock)
			},
		},
	}

	// GET /configs
	resp, err := client.Get("http://localhost/configs")
	if err != nil {
		st.Error = err.Error()
		return st
	}
	rawCfg, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		st.Error = fmt.Sprintf("/configs HTTP %d", resp.StatusCode)
		return st
	}
	var cfgObj struct {
		MixedPort int    `json:"mixed-port"`
		Mode      string `json:"mode"`
		IPv6      bool   `json:"ipv6"`
		TUN       struct {
			Enable bool `json:"enable"`
		} `json:"tun"`
	}
	if err := json.Unmarshal(rawCfg, &cfgObj); err == nil {
		st.Available = true
		st.MixedPort = cfgObj.MixedPort
		st.Mode = cfgObj.Mode
		st.IPv6 = cfgObj.IPv6
		st.TUNEnabled = cfgObj.TUN.Enable
	}

	// GET /proxies
	resp2, err := client.Get("http://localhost/proxies")
	if err == nil {
		rawP, _ := io.ReadAll(io.LimitReader(resp2.Body, 1024*1024))
		_ = resp2.Body.Close()
		var pObj struct {
			Proxies map[string]struct {
				Type string   `json:"type"`
				Now  string   `json:"now"`
				All  []string `json:"all"`
			} `json:"proxies"`
		}
		if err := json.Unmarshal(rawP, &pObj); err == nil {
			if g, ok := pObj.Proxies["Anthropic"]; ok {
				st.AnthropicGroupType = g.Type
				st.AnthropicNow = g.Now
				st.AnthropicChoices = g.All
			}
		}
	}
	return st
}
