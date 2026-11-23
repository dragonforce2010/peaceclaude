//go:build windows

package host

import "syscall"

// notifyWindowsProxyChanged notifies WinINet (Chrome/Edge/system HTTP stack) immediately
// so AutoConfigURL registry edits take effect without restarting browsers or waiting.
func notifyWindowsProxyChanged() {
	wininet := syscall.NewLazyDLL("wininet.dll")
	proc := wininet.NewProc("InternetSetOptionW")
	const (
		internetOptionRefresh         = 37
		internetOptionSettingsChanged = 39
	)
	_, _, _ = proc.Call(0, internetOptionSettingsChanged, 0, 0)
	_, _, _ = proc.Call(0, internetOptionRefresh, 0, 0)
}
