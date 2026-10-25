package cli

import (
	"context"
	"dai/internal/download"
	"errors"
	"testing"
)

const (
	cliTestExpectedMissingArgumentErrorGot = "expected missing argument error, got %v"
	cliTestExpectedEmptyURLErrorGot        = "expected empty URL error, got %v"
	cliTestOverwrite                       = "--overwrite"
	cliTestOutput                          = "--output"
	cliTestHttpsRutubeRuVideo              = "https://rutube.ru/video/"
	cliTestShare1Fragment                  = "/?share=1#fragment"
	cliTestBadDispatch                     = "bad dispatch: %+v"
)

type fixtureService struct {
	kind    argType
	id      string
	options download.Options
}

func (service *fixtureService) Video(_ context.Context, id string, options download.Options) error {
	service.kind = singleVideo
	service.id = id
	service.options = options

	return nil
}

func (service *fixtureService) Playlist(_ context.Context, id string, options download.Options) error {
	service.kind = playlist
	service.id = id
	service.options = options

	return nil
}

func TestRunWithoutURL(test *testing.T) {
	service := &fixtureService{}

	command, _ := New(service)
	if err := command.Run(context.Background(), nil); !errors.Is(err, errWithoutArgs) {
		test.Fatalf(cliTestExpectedMissingArgumentErrorGot, err)
	}
}

func TestRunWithEmptyURL(test *testing.T) {
	service := &fixtureService{}

	command, _ := New(service)
	if err := command.Run(context.Background(), []string{""}); !errors.Is(err, errEmptyLink) {
		test.Fatalf(cliTestExpectedEmptyURLErrorGot, err)
	}
}

func TestRunDispatchesOptions(test *testing.T) {
	service := &fixtureService{}

	command, _ := New(service)
	if err := command.Run(context.Background(), []string{cliTestOverwrite, cliTestOutput, test.TempDir(), cliTestHttpsRutubeRuVideo + fixtureVideoID + cliTestShare1Fragment}); err != nil {
		test.Fatal(err)
	}

	if service.kind != singleVideo || service.id != fixtureVideoID || !service.options.Overwrite || service.options.OutputDirectory == "" {
		test.Fatalf(cliTestBadDispatch, service)
	}
}

const (
	cliJobsFlag            = "--jobs"
	cliJobsSingle          = "1"
	cliJobsInvalid         = "invalid"
	cliJobsZero            = "0"
	cliJobsNegative        = "-1"
	cliPlaylistURL         = "https://rutube.ru/plst/1840659/"
	cliJobsUnexpected      = "jobs=%d, expected=%d; dispatch=%v"
	cliJobsAcceptedInvalid = "accepted invalid jobs value %q or dispatched a download"
)

func TestPlaylistJobsOption(test *testing.T) {
	for _, explicit := range []bool{false, true} {
		test.Run(map[bool]string{false: "default", true: "explicit"}[explicit], func(test *testing.T) {
			service := &fixtureService{}

			command, err := New(service)
			if err != nil {
				test.Fatal(err)
			}

			arguments := []string{cliPlaylistURL}
			expected := download.DefaultJobs

			if explicit {
				arguments = []string{cliJobsFlag, cliJobsSingle, cliPlaylistURL}
				expected = 1
			}

			if err := command.Run(test.Context(), arguments); err != nil {
				test.Fatal(err)
			}

			if service.kind != playlist || service.options.Jobs != expected {
				test.Fatalf(cliJobsUnexpected, service.options.Jobs, expected, service.kind)
			}
		})
	}
}

func TestInvalidJobsNeverDispatches(test *testing.T) {
	for _, value := range []string{cliJobsZero, cliJobsNegative, cliJobsInvalid} {
		test.Run(value, func(test *testing.T) {
			service := &fixtureService{}

			command, err := New(service)
			if err != nil {
				test.Fatal(err)
			}

			if err := command.Run(test.Context(), []string{cliJobsFlag, value, cliPlaylistURL}); err == nil || service.kind != notExpected {
				test.Fatalf(cliJobsAcceptedInvalid, value)
			}
		})
	}
}
