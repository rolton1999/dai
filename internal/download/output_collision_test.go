package download

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dai/internal/rutube"
)

const (
	collisionCaseName         = "case"
	collisionUnicodeName      = "unicode normalization"
	collisionUpperTitle       = "Review"
	collisionLowerTitle       = "review"
	collisionComposedTitle    = "é"
	collisionDecomposedTitle  = "e\u0301"
	collisionPlainMode        = "plain"
	collisionOverwriteMode    = "overwrite"
	collisionResumeMode       = "resume"
	collisionErrorPrefix      = "duplicate playlist output:"
	collisionUnexpectedResult = "collision result=%v downloads=%d"
	collisionUnexpectedOutput = "output=%q error=%v, expected %q"
)

type collisionProvider struct {
	provider *parallelProvider
	titles   map[string]string
}

func (provider *collisionProvider) Playlist(operationContext context.Context, identifier string) (rutube.PlaylistInfo, error) {
	return provider.provider.Playlist(operationContext, identifier)
}

func (provider *collisionProvider) Video(operationContext context.Context, identifier string) (rutube.VideoInfo, error) {
	info, err := provider.provider.Video(operationContext, identifier)
	info.Title = provider.titles[identifier]

	return info, err
}

func TestPlaylistDetectsFilesystemFilenameCollisions(test *testing.T) {
	fixtures := []struct {
		name        string
		firstTitle  string
		secondTitle string
	}{
		{collisionCaseName, collisionUpperTitle, collisionLowerTitle},
		{collisionUnicodeName, collisionComposedTitle, collisionDecomposedTitle},
	}
	for _, fixture := range fixtures {
		test.Run(fixture.name, func(test *testing.T) {
			probeDirectory := test.TempDir()
			firstProbe := filepath.Join(probeDirectory, safeFilename(fixture.firstTitle))
			secondProbe := filepath.Join(probeDirectory, safeFilename(fixture.secondTitle))

			if err := os.WriteFile(firstProbe, []byte(testContents), testPermissions); err != nil {
				test.Fatal(err)
			}

			_, probeError := os.Stat(secondProbe)
			if probeError != nil && !errors.Is(probeError, os.ErrNotExist) {
				test.Fatal(probeError)
			}

			aliases := probeError == nil

			for _, mode := range []string{collisionPlainMode, collisionOverwriteMode, collisionResumeMode} {
				test.Run(mode, func(test *testing.T) {
					directory := test.TempDir()
					provider, streams, converter := newParallelFixture(test, resumeTestCount)
					titles := map[string]string{
						provider.identifiers[0]: fixture.firstTitle,
						provider.identifiers[1]: fixture.secondTitle,
					}
					titledProvider := &collisionProvider{provider: provider, titles: titles}

					for _, release := range converter.releases {
						release <- nil
					}

					logger := &fixtureLogger{}

					service, err := New(titledProvider, streams, converter, logger)
					if err != nil {
						test.Fatal(err)
					}

					options := Options{
						OutputDirectory: directory,
						Overwrite:       mode == collisionOverwriteMode,
						Resume:          mode == collisionResumeMode,
					}

					err = service.Playlist(test.Context(), parallelPlaylistID, options)
					if aliases {
						if err == nil || !strings.Contains(err.Error(), collisionErrorPrefix) || streams.started.Load() != 0 {
							test.Fatalf(collisionUnexpectedResult, err, streams.started.Load())
						}
					} else {
						if err != nil {
							test.Fatal(err)
						}

						for identifier, title := range titles {
							body, err := os.ReadFile(filepath.Join(directory, safeFilename(title)))
							if err != nil || string(body) != identifier {
								test.Fatalf(collisionUnexpectedOutput, body, err, identifier)
							}
						}
					}

					plans, err := filepath.Glob(filepath.Join(directory, outputPlanWorkspacePattern))
					if err != nil || len(plans) != 0 {
						test.Fatalf(parallelUnexpectedFiles, plans, err)
					}
				})
			}
		})
	}
}

func TestPlaylistFilenameCheckPreservesExistingFiles(test *testing.T) {
	directory := test.TempDir()

	target := filepath.Join(directory, testTitle+outputExtension)
	if err := os.WriteFile(target, []byte(testContents), testPermissions); err != nil {
		test.Fatal(err)
	}

	err := checkPlaylistFilenames(test.Context(), directory, []downloadJob{{target: target}, {target: target}})
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf(duplicatePlaylistOutputFormat, target)) {
		test.Fatalf(collisionUnexpectedResult, err, 0)
	}

	body, err := os.ReadFile(target)
	if err != nil || string(body) != testContents {
		test.Fatalf(collisionUnexpectedOutput, body, err, testContents)
	}
}
