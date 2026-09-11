//go:build !windows

package main

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func isWakeLockBusy(err error) bool { return errors.Is(err, unix.EWOULDBLOCK) }

func lockInbox(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
func unlockInbox(f *os.File) { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }
