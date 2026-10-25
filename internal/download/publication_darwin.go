package download

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameExclusive(source string, target string) error {
	if err := unix.RenameatxNp(unix.AT_FDCWD, source, unix.AT_FDCWD, target, unix.RENAME_EXCL); err != nil {
		return &os.LinkError{Op: exclusiveRenameOperation, Old: source, New: target, Err: err}
	}

	return nil
}
