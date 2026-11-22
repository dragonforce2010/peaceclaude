//go:build !darwin

package upstream

import "syscall"

func detectPhysicalOutboundInterface() string { return "" }

// DetectPhysicalOutboundInterface returns the auto-detected physical outbound interface when supported.
func DetectPhysicalOutboundInterface() string { return "" }

// ParsePhysicalDefaultForTest returns empty on non-darwin targets.
func ParsePhysicalDefaultForTest(string, func(string) bool) string { return "" }

func bindSocketToInterface(string) func(network, address string, c syscall.RawConn) error {
	return nil
}
