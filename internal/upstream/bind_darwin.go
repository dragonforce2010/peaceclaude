//go:build darwin

package upstream

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	darwinIPBoundIF   = 25  // IP_BOUND_IF from <netinet/in.h>
	darwinIPv6BoundIF = 125 // IPV6_BOUND_IF from <netinet6/in6.h>
)

var virtualIfacePrefixes = []string{
	"utun", "tun", "tap", "ppp", "ipsec", "lo",
	"awdl", "llw", "anpi", "bridge", "gif", "stf",
	"feth", "vmnet", "vmenet",
}

func isVirtualOrTunnelInterface(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return true
	}
	for _, p := range virtualIfacePrefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

func isInterfaceUsableForEgress(name string) bool {
	if isVirtualOrTunnelInterface(name) {
		return false
	}
	iface, err := net.InterfaceByName(name)
	if err != nil || (iface.Flags&net.FlagUp) == 0 || (iface.Flags&net.FlagLoopback) != 0 {
		return false
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP == nil {
			continue
		}
		v4 := ipNet.IP.To4()
		if v4 != nil && !v4.IsLoopback() && !v4.IsLinkLocalUnicast() {
			return true
		}
	}
	return false
}

// parsePhysicalDefaultInterface picks the first usable non-VPN physical interface
// carrying an IPv4 default gateway in `netstat -rn -f inet` output.
func parsePhysicalDefaultInterface(netstatOut string, isUsable func(string) bool) string {
	for _, rawLine := range strings.Split(netstatOut, "\n") {
		fields := strings.Fields(rawLine)
		if len(fields) < 4 || fields[0] != "default" {
			continue
		}
		gw := fields[1]
		if strings.HasPrefix(gw, "link#") {
			continue
		}
		iface := fields[3]
		if isVirtualOrTunnelInterface(iface) {
			continue
		}
		if isUsable == nil || isUsable(iface) {
			return iface
		}
	}
	return ""
}

var physIfaceCache struct {
	mu      sync.Mutex
	iface   string
	expires time.Time
}

func detectPhysicalOutboundInterface() string {
	now := time.Now()
	physIfaceCache.mu.Lock()
	if now.Before(physIfaceCache.expires) && physIfaceCache.iface != "" {
		cached := physIfaceCache.iface
		physIfaceCache.mu.Unlock()
		return cached
	}
	physIfaceCache.mu.Unlock()

	var chosen string
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	out, err := exec.CommandContext(ctx, "netstat", "-rn", "-f", "inet").Output()
	cancel()
	if err == nil {
		chosen = parsePhysicalDefaultInterface(string(out), isInterfaceUsableForEgress)
	}
	if chosen == "" {
		if ifaces, err := net.Interfaces(); err == nil {
			for _, iface := range ifaces {
				if strings.HasPrefix(iface.Name, "en") && isInterfaceUsableForEgress(iface.Name) {
					chosen = iface.Name
					break
				}
			}
		}
	}

	physIfaceCache.mu.Lock()
	physIfaceCache.iface = chosen
	if chosen != "" {
		physIfaceCache.expires = time.Now().Add(15 * time.Second)
	} else {
		physIfaceCache.expires = time.Now().Add(3 * time.Second)
	}
	physIfaceCache.mu.Unlock()
	return chosen
}

// DetectPhysicalOutboundInterface returns the auto-detected physical macOS outbound interface (e.g. en0).
func DetectPhysicalOutboundInterface() string {
	return detectPhysicalOutboundInterface()
}

// ParsePhysicalDefaultForTest exposes default-route parsing for cross-package or unit tests.
func ParsePhysicalDefaultForTest(netstatOut string, isUsable func(string) bool) string {
	return parsePhysicalDefaultInterface(netstatOut, isUsable)
}

func bindSocketToInterface(ifaceName string) func(network, address string, c syscall.RawConn) error {
	name := strings.TrimSpace(ifaceName)
	if name == "" {
		return nil
	}
	iface, err := net.InterfaceByName(name)
	if err != nil || iface.Index <= 0 {
		return nil
	}
	ifIndex := iface.Index

	return func(network, address string, c syscall.RawConn) error {
		// Double guard: never bind loopback or private LAN addresses to physical egress interface.
		if isLoopbackOrPrivateHost(address) {
			return nil
		}
		var sockErr error
		if cerr := c.Control(func(fd uintptr) {
			host, _, splitErr := net.SplitHostPort(address)
			if splitErr != nil {
				host = address
			}
			ip := net.ParseIP(strings.Trim(host, "[]"))
			if network == "tcp6" || (ip != nil && ip.To4() == nil) {
				sockErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, darwinIPv6BoundIF, ifIndex)
			} else {
				sockErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, darwinIPBoundIF, ifIndex)
			}
		}); cerr != nil {
			return cerr
		}
		return sockErr
	}
}
