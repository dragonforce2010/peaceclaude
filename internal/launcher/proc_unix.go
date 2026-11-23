//go:build !windows

package launcher

import (
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

func setDetachedProcessAttr(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func signalTermPID(pid int) error {
	if pid <= 1 {
		return nil
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}

func isPIDAlive(pid int) bool {
	if pid <= 1 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func terminatePIDNumeric(pids []int, wait time.Duration) int {
	if len(pids) == 0 {
		return 0
	}
	if wait <= 0 {
		wait = 4 * time.Second
	}
	for _, pid := range pids {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		alive := false
		for _, pid := range pids {
			if syscall.Kill(pid, 0) == nil {
				alive = true
				break
			}
		}
		if !alive {
			return len(pids)
		}
		time.Sleep(150 * time.Millisecond)
	}
	for _, pid := range pids {
		if syscall.Kill(pid, 0) == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	time.Sleep(250 * time.Millisecond)
	return len(pids)
}

func execReplaceOrRun(cliBin string, argv []string, env []string) error {
	if err := syscall.Exec(cliBin, argv, env); err != nil {
		cmd := exec.Command(cliBin, argv[1:]...)
		cmd.Env = env
		return cmd.Run()
	}
	return nil
}

func openExternalTarget(target string) error {
	if target == "" {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target).Run()
	default:
		return exec.Command("xdg-open", target).Run()
	}
}

func openAppTarget(appPath string) error {
	if appPath == "" {
		return nil
	}
	if runtime.GOOS == "darwin" {
		return exec.Command("open", "-a", appPath).Run()
	}
	return exec.Command(appPath).Start()
}

func revealPathInFileManager(path string) error {
	if path == "" {
		return nil
	}
	if runtime.GOOS == "darwin" {
		return exec.Command("open", "-R", path).Run()
	}
	return exec.Command("xdg-open", path).Run()
}
