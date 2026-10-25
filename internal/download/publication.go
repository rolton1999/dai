package download

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const (
	exclusiveRenameOperation     = "rename-exclusive"
	publicationProbePattern      = ".dai-publication-*"
	publicationProbeSource       = "source"
	publicationProbeTarget       = "target"
	publicationProbePermissions  = 0o600
	publicationUnsupportedFormat = "output directory does not support safe publication: %w"
)

func publishNewFile(staging string, target string) error {
	return publishWithFallback(staging, target, os.Link, renameExclusive)
}

func publishWithFallback(
	staging string,
	target string,
	link func(string, string) error,
	rename func(string, string) error,
) error {
	linkError := link(staging, target)
	if linkError == nil {
		return nil
	}

	if !errors.Is(linkError, syscall.EOPNOTSUPP) && !errors.Is(linkError, syscall.EPERM) && !errors.Is(linkError, syscall.ENOSYS) {
		return linkError
	}

	// Exclusive rename preserves collision protection on filesystems without links.
	return rename(staging, target)
}

func checkPublicationSupport(directory string) (resultError error) {
	workspace, err := os.MkdirTemp(directory, publicationProbePattern)
	if err != nil {
		return err
	}

	defer func() {
		resultError = errors.Join(resultError, os.RemoveAll(workspace))
	}()

	source := filepath.Join(workspace, publicationProbeSource)
	target := filepath.Join(workspace, publicationProbeTarget)

	if err := os.WriteFile(source, nil, publicationProbePermissions); err != nil {
		return err
	}

	if err := publishNewFile(source, target); err != nil {
		return fmt.Errorf(publicationUnsupportedFormat, err)
	}

	return nil
}
