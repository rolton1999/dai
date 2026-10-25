//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package download

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func newResumeLock(path string) (*resumeLock, error) {
	file, err := openResumeLockFile(path)
	if err != nil {
		return nil, err
	}

	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.Join(fmt.Errorf(resumeLockBusy, err), file.Close())
	}

	return &resumeLock{file: file}, nil
}

type resumeLock struct {
	file *os.File
}

func (lock *resumeLock) Close() error {
	return lock.file.Close()
}

func syncResumeDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}

	return errors.Join(directory.Sync(), directory.Close())
}
