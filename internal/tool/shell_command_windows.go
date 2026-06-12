//go:build windows

package tool

import (
	"context"
	"encoding/base64"
	"os/exec"
	"sync"
	"unicode/utf16"
)

var (
	winShellPath     string
	winShellPathOnce sync.Once
	winShellArgs     []string // pre-built args for the selected shell
)

func getWindowsShellPath() string {
	winShellPathOnce.Do(func() {
		// Prefer PowerShell 7, then Windows PowerShell, then fall back to cmd.
		if p, err := exec.LookPath("pwsh.exe"); err == nil {
			winShellPath = p
			winShellArgs = []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand"}
			return
		}
		if p, err := exec.LookPath("powershell.exe"); err == nil {
			winShellPath = p
			winShellArgs = []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand"}
			return
		}
		// Neither pwsh nor powershell available — fall back to cmd.exe.
		// cmd /c does NOT use -EncodedCommand; we pass the raw command.
		if p, err := exec.LookPath("cmd.exe"); err == nil {
			winShellPath = p
			winShellArgs = []string{"/c"}
			return
		}
		// Absolute last resort — let exec.Command fail with a clear error.
		winShellPath = "cmd.exe"
		winShellArgs = []string{"/c"}
	})
	return winShellPath
}

func encodePSCommand(command string) string {
	u16 := utf16.Encode([]rune(command))
	b := make([]byte, len(u16)*2)
	for i, r := range u16 {
		b[i*2] = byte(r)
		b[i*2+1] = byte(r >> 8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func newShellCommand(ctx context.Context, command string) *exec.Cmd {
	shell := getWindowsShellPath()

	// PowerShell path: use -EncodedCommand for safe argument passing.
	if len(winShellArgs) > 0 && winShellArgs[0] == "-NoProfile" {
		encoded := encodePSCommand(command)
		args := make([]string, 0, len(winShellArgs)+1)
		args = append(args, winShellArgs...)
		args = append(args, encoded)
		return exec.CommandContext(ctx, shell, args...)
	}

	// cmd.exe fallback: pass the raw command after /c.
	args := []string{"/c", command}
	return exec.CommandContext(ctx, shell, args...)
}
