//go:build windows

package launcher

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

func setDetachedProcessAttr(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func signalTermPID(pid int) error {
	if pid <= 1 {
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

func isPIDAlive(pid int) bool {
	if pid <= 1 {
		return false
	}
	_, err := os.FindProcess(pid)
	return err == nil
}

func terminatePIDNumeric(pids []int, _ time.Duration) int {
	for _, pid := range pids {
		_ = signalTermPID(pid)
	}
	return len(pids)
}

func execReplaceOrRun(cliBin string, argv []string, env []string) error {
	cmd := exec.Command(cliBin, argv[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func openExternalTarget(target string) error {
	if target == "" {
		return nil
	}
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
}

func openAppTarget(appPath string) error {
	if appPath == "" {
		return nil
	}
	return exec.Command("cmd", "/C", "start", "", appPath).Start()
}

func revealPathInFileManager(path string) error {
	if path == "" {
		return nil
	}
	return exec.Command("explorer", "/select,", path).Start()
}
