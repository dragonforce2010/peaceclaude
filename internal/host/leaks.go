package host

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"time"

	"peaceclaude/internal/policy"
)

type DirectSocketHit struct {
	Process  string `json:"process"`
	PID      string `json:"pid"`
	Protocol string `json:"protocol"`
	DestIP   string `json:"dest_ip"`
	DestPort string `json:"dest_port"`
}

// ScanDirectAnthropicSockets inspects host ESTABLISHED TCP and UDP sockets via lsof
// for any process bypassing the proxy and talking directly to Anthropic CIDRs.
// Intentionally excludes SYN_SENT sockets (which indicate pf killswitch is blocking them).
func ScanDirectAnthropicSockets(expectedProxyIP string) []DirectSocketHit {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	var hits []DirectSocketHit
	seen := map[string]bool{}

	for _, args := range [][]string{
		{"-nP", "-iTCP", "-sTCP:ESTABLISHED"},
		{"-nP", "-iUDP"},
	} {
		out, err := exec.CommandContext(ctx, "lsof", args...).Output()
		if err != nil && len(out) == 0 {
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			hit, ok := parseLsofDirectHit(line, expectedProxyIP)
			if !ok {
				continue
			}
			key := hit.PID + ":" + hit.DestIP + ":" + hit.DestPort
			if !seen[key] {
				seen[key] = true
				hits = append(hits, hit)
			}
		}
	}
	return hits
}

func parseLsofDirectHit(line, expectedProxyIP string) (DirectSocketHit, bool) {
	fields := strings.Fields(line)
	if len(fields) < 9 {
		return DirectSocketHit{}, false
	}
	comm := fields[0]
	pid := fields[1]
	proto := fields[7]
	name := strings.Join(fields[8:], " ")
	_, right, ok := strings.Cut(name, "->")
	if !ok {
		return DirectSocketHit{}, false
	}
	right = strings.Fields(right)[0]
	host, port, err := net.SplitHostPort(right)
	if err != nil {
		return DirectSocketHit{}, false
	}
	host = strings.Trim(host, "[]")
	if host == "" || host == "127.0.0.1" || host == "::1" || host == expectedProxyIP {
		return DirectSocketHit{}, false
	}
	if strings.HasPrefix(host, "127.") || strings.HasPrefix(host, "10.255.") {
		return DirectSocketHit{}, false
	}
	if !policy.IsAnthropicIP(host) {
		return DirectSocketHit{}, false
	}
	return DirectSocketHit{
		Process:  comm,
		PID:      pid,
		Protocol: proto,
		DestIP:   host,
		DestPort: port,
	}, true
}
