//go:build integration

package download_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"dai/internal/download"
	"dai/internal/ffmpeg"
	"dai/internal/hls"
	"dai/internal/http/client"
	"dai/internal/progress"
	"dai/internal/rutube"
)

const (
	recoveryIntegrationPlaylistID      = "123"
	recoveryIntegrationSecondID        = "fedcba9876543210fedcba9876543210"
	recoveryIntegrationFirstTitle      = "first video"
	recoveryIntegrationSecondTitle     = "second video"
	recoveryIntegrationPlaylistPath    = "/api/playlist/custom/123/"
	recoveryIntegrationVideosPath      = "/api/playlist/custom/123/videos/"
	recoveryIntegrationPlaylistBody    = `{"title":"local playlist"}`
	recoveryIntegrationVideosBody      = `{"results":[{"id":"0123456789abcdef0123456789abcdef"},{"id":"fedcba9876543210fedcba9876543210"}],"has_next":false,"next":null}`
	recoveryIntegrationMediaPath       = "/media.m3u8"
	recoveryIntegrationSegmentPath     = "/segment.ts"
	recoveryIntegrationAttempts        = 2
	recoveryIntegrationUnexpectedCalls = "unexpected resume requests: %d; segment downloads: %d"
	recoveryIntegrationReplacedOutput  = "resume replaced output: %s"
)

func TestLocalPlaylistResumeReusesConvertedMP4s(test *testing.T) {
	operationContext, cancel := context.WithTimeout(test.Context(), localTimeout)
	defer cancel()

	segment := filepath.Join(test.TempDir(), localSegmentName)
	arguments := append(strings.Fields(localGeneration), segment)

	command := exec.CommandContext(operationContext, localFFmpeg, arguments...)
	if output, err := command.CombinedOutput(); err != nil {
		test.Fatalf(localIntegrationTestGenerateFixture, err, output)
	}

	segmentBody, err := os.ReadFile(segment)
	if err != nil {
		test.Fatal(err)
	}

	var (
		resumed    atomic.Bool
		unexpected atomic.Int32
		segments   atomic.Int32
	)

	handler := http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case recoveryIntegrationPlaylistPath:
			_, _ = fmt.Fprint(responseWriter, recoveryIntegrationPlaylistBody)
			return
		case recoveryIntegrationVideosPath:
			_, _ = fmt.Fprint(responseWriter, recoveryIntegrationVideosBody)
			return
		}

		if resumed.Load() {
			unexpected.Add(1)
			http.NotFound(responseWriter, request)

			return
		}

		switch request.URL.Path {
		case localIntegrationTestApiPlayOptions + localID + localIntegrationTestFilenameCharacters:
			_, _ = fmt.Fprintf(responseWriter, localIntegrationTestTitleVideoBalancerM3u8, recoveryIntegrationFirstTitle, localIntegrationTestHttp+request.Host+localIntegrationTestMasterM3u8)
		case localIntegrationTestApiPlayOptions + recoveryIntegrationSecondID + localIntegrationTestFilenameCharacters:
			_, _ = fmt.Fprintf(responseWriter, localIntegrationTestTitleVideoBalancerM3u8, recoveryIntegrationSecondTitle, localIntegrationTestHttp+request.Host+localIntegrationTestMasterM3u8)
		case localIntegrationTestMasterM3u8:
			_, _ = fmt.Fprint(responseWriter, localMaster)
		case recoveryIntegrationMediaPath:
			_, _ = fmt.Fprint(responseWriter, localMedia)
		case recoveryIntegrationSegmentPath:
			segments.Add(1)

			_, _ = responseWriter.Write(segmentBody)
		default:
			http.NotFound(responseWriter, request)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	directory := test.TempDir()
	temporaryRoot := test.TempDir()
	test.Setenv(localTemporaryKey, temporaryRoot)

	before := make(map[string]os.FileInfo)

	for attempt := range recoveryIntegrationAttempts {
		network := server.Client()
		doer := &localDoer{serverURL: server.URL, client: network}
		reader := client.New(doer)
		provider := rutube.New(reader)
		streams := hls.New(reader, doer)
		converter := ffmpeg.New(nil)
		logger := progress.NewLogger()

		service, err := download.New(provider, streams, converter, logger)
		if err != nil {
			doer.Close()
			test.Fatal(err)
		}

		options := download.Options{OutputDirectory: directory, Resume: true}
		err = service.Playlist(operationContext, recoveryIntegrationPlaylistID, options)

		doer.Close()

		if err != nil {
			test.Fatal(err)
		}

		for _, title := range []string{recoveryIntegrationFirstTitle, recoveryIntegrationSecondTitle} {
			target := filepath.Join(directory, title+localIntegrationTestMp4)

			information, err := os.Stat(target)
			if err != nil {
				test.Fatal(err)
			}

			if attempt == 0 {
				before[target] = information
			} else if !os.SameFile(before[target], information) || !before[target].ModTime().Equal(information.ModTime()) {
				test.Fatalf(recoveryIntegrationReplacedOutput, target)
			}
		}

		resumed.Store(true)
	}

	if unexpected.Load() != 0 || segments.Load() != recoveryIntegrationAttempts {
		test.Fatalf(recoveryIntegrationUnexpectedCalls, unexpected.Load(), segments.Load())
	}

	entries, err := os.ReadDir(temporaryRoot)
	if err != nil || len(entries) != 0 {
		test.Fatalf(localIntegrationTestTemporarySegmentsRemain, entries, err)
	}
}
