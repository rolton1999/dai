//go:build darwin || linux

package download

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
)

func TestConcurrentFallbackPublicationPreservesWinner(test *testing.T) {
	const (
		competitors       = 2
		firstSource       = "first.mp4"
		secondSource      = "second.mp4"
		unexpectedWinners = "successful publications: %d"
	)

	directory := test.TempDir()
	target := filepath.Join(directory, publicationFixtureTarget)
	results := make(chan error, competitors)
	start := make(chan struct{})

	var workers sync.WaitGroup

	for _, name := range []string{firstSource, secondSource} {
		source := filepath.Join(directory, name)
		if err := os.WriteFile(source, []byte(name), testPermissions); err != nil {
			test.Fatal(err)
		}
	}

	for _, name := range []string{firstSource, secondSource} {
		source := filepath.Join(directory, name)

		workers.Go(func() {
			<-start

			unsupportedLink := func(string, string) error {
				return syscall.EOPNOTSUPP
			}

			results <- publishWithFallback(source, target, unsupportedLink, renameExclusive)
		})
	}

	close(start)
	workers.Wait()
	close(results)

	winners := 0

	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, os.ErrExist) {
			test.Fatalf(publicationUnexpectedResult, err)
		}
	}

	if winners != 1 {
		test.Fatalf(unexpectedWinners, winners)
	}

	body, err := os.ReadFile(target)
	if err != nil || (string(body) != firstSource && string(body) != secondSource) {
		test.Fatalf(publicationChangedOutput, body, err)
	}
}
