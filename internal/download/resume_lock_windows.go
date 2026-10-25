package download

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

const resumeLockLength = 1

func newResumeLock(path string) (*resumeLock, error) {
	file, err := openResumeLockFile(path)
	if err != nil {
		return nil, err
	}

	var overlapped windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, resumeLockLength, 0, &overlapped); err != nil {
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

func syncResumeDirectory(string) error {
	// Windows does not support syncing a directory handle through os.File.Sync.
	return nil
}
