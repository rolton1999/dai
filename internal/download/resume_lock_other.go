//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package download

import "errors"

const resumeUnsupportedPlatform = "playlist resume locking is unsupported on this platform"

func newResumeLock(string) (*resumeLock, error) {
	return nil, errors.New(resumeUnsupportedPlatform)
}

type resumeLock struct{}

func (*resumeLock) Close() error {
	return nil
}

func syncResumeDirectory(string) error {
	return errors.New(resumeUnsupportedPlatform)
}
