package download

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

const (
	publicationFixtureSource    = "staging.mp4"
	publicationFixtureTarget    = "video.mp4"
	publicationUnexpectedResult = "publication result: %v"
	publicationUnexpectedRename = "exclusive rename called unexpectedly"
	publicationChangedOutput    = "output changed: %q, %v"
)

func TestPublicationFallsBackOnlyWhenLinksAreUnsupported(test *testing.T) {
	for _, failure := range []error{syscall.EOPNOTSUPP, syscall.EPERM, syscall.ENOSYS, os.ErrExist, os.ErrPermission} {
		test.Run(failure.Error(), func(test *testing.T) {
			renameCalled := false
			link := func(string, string) error {
				return failure
			}
			rename := func(string, string) error {
				renameCalled = true
				return nil
			}

			err := publishWithFallback(publicationFixtureSource, publicationFixtureTarget, link, rename)

			unsupported := errors.Is(failure, syscall.EOPNOTSUPP) || errors.Is(failure, syscall.EPERM) || errors.Is(failure, syscall.ENOSYS)
			if unsupported {
				if err != nil || !renameCalled {
					test.Fatalf(publicationUnexpectedResult, err)
				}
			} else if !errors.Is(err, failure) || renameCalled {
				test.Fatalf(publicationUnexpectedResult, err)
			}
		})
	}
}

func TestPublicationPreservesExistingOutput(test *testing.T) {
	directory := test.TempDir()
	source := filepath.Join(directory, publicationFixtureSource)
	target := filepath.Join(directory, publicationFixtureTarget)

	for _, path := range []string{source, target} {
		if err := os.WriteFile(path, []byte(testContents), testPermissions); err != nil {
			test.Fatal(err)
		}
	}

	rename := func(string, string) error {
		test.Fatal(publicationUnexpectedRename)
		return nil
	}

	if err := publishWithFallback(source, target, os.Link, rename); !errors.Is(err, os.ErrExist) {
		test.Fatalf(publicationUnexpectedResult, err)
	}

	body, err := os.ReadFile(target)
	if err != nil || string(body) != testContents {
		test.Fatalf(publicationChangedOutput, body, err)
	}
}

func TestPublicationProbeCleansUp(test *testing.T) {
	directory := test.TempDir()
	if err := checkPublicationSupport(directory); err != nil {
		test.Fatal(err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		test.Fatalf(outputTestPublicationCleanup, entries, err)
	}
}
