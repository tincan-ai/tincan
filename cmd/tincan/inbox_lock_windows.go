package main

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
)

func isWakeLockBusy(err error) bool { return errors.Is(err, windows.ERROR_LOCK_VIOLATION) }

func lockInbox(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
func unlockInbox(f *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{})
	_ = f.Close()
}
