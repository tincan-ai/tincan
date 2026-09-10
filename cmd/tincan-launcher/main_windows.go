//go:build windows

// The small x86 dispatcher runs on both AMD64 and ARM64 Windows through the
// operating system's built-in compatibility support, then starts the native CLI.
package main

import (
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
)

func nativeTarget() (string, error) {
	var processMachine, nativeMachine uint16
	if err := windows.IsWow64Process2(windows.CurrentProcess(), &processMachine, &nativeMachine); err != nil {
		return "", fmt.Errorf("detect Windows architecture: %w", err)
	}
	switch nativeMachine {
	case 0x8664:
		return "windows-amd64", nil
	case 0xaa64:
		return "windows-arm64", nil
	default:
		return "", fmt.Errorf("unsupported Windows architecture: %#x", nativeMachine)
	}
}

func run() int {
	target, err := nativeTarget()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Tincan:", err)
		return 126
	}
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Tincan:", err)
		return 126
	}
	binary := filepath.Join(filepath.Dir(self), target, "tincan.exe")
	child := exec.Command(binary, os.Args[1:]...)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Windows delivers console control events to both processes. Let the native
	// child handle them and remain alive to collect its exit status.
	handler := windows.NewCallback(func(event uint32) uintptr {
		if event == 0 || event == 1 {
			return 1
		}
		return 0
	})
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("SetConsoleCtrlHandler")
	proc.Call(handler, 1)
	defer proc.Call(handler, 0)
	if err := child.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "Tincan could not start its bundled executable. Reinstall the plugin from the Tincan marketplace:", err)
		return 126
	}
	return 0
}

func main() { os.Exit(run()) }
