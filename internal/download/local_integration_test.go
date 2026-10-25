//go:build integration

package download_test

import (
	"context"
	"dai/internal/download"
	"dai/internal/ffmpeg"
	"dai/internal/hls"
	"dai/internal/http/client"
	"dai/internal/progress"
	"dai/internal/rutube"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	localTemporaryKey = "TMPDIR"
	localFFmpeg       = "ffmpeg"
	localTimeout      = 30 * time.Second
	localTitle        = "local video"
	localID           = "0123456789abcdef0123456789abcdef"
	localSegmentName  = "segment.ts"
	localGeneration   = "-hide_banner -loglevel error -f lavfi -i color=c=black:s=16x16:r=1 -t 8 -c:v mpeg2video -f mpegts"
	localMaster       = "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000,RESOLUTION=16x16\nmedia.m3u8\n"
	localMedia        = "#EXTM3U\n#EXTINF:8,\nsegment.ts\n#EXT-X-ENDLIST\n"
)

const (
	localIntegrationTestRutubeRu                  = "rutube.ru"
	localIntegrationTestGenerateFixture           = "generate fixture: %v %s"
	localIntegrationTestApiPlayOptions            = "/api/play/options/"
	localIntegrationTestFilenameCharacters        = "/"
	localIntegrationTestTitleVideoBalancerM3u8    = `{"title":%q,"video_balancer":{"m3u8":%q}}`
	localIntegrationTestHttp                      = "http://"
	localIntegrationTestMasterM3u8                = "/master.m3u8"
	localIntegrationTestMasterMasterM3u8          = "/master/master.m3u8"
	localIntegrationTestMasterMediaM3u8           = "/master/media.m3u8"
	localIntegrationTestMediaMediaM3u8            = "/media/media.m3u8"
	localIntegrationTestMediaSegmentTs            = "/media/segment.ts"
	localIntegrationTestMp4                       = ".mp4"
	localIntegrationTestFfprobe                   = "ffprobe"
	localIntegrationTestV                         = "-v"
	localIntegrationTestError                     = "error"
	localIntegrationTestShowEntries               = "-show_entries"
	localIntegrationTestFormatDuration            = "format=duration"
	localIntegrationTestProbe                     = "probe: %v %s"
	localIntegrationTestTemporarySegmentsRemain   = "temporary segments remain: %v %v"
	localIntegrationTestUnexpectedOutputArtifacts = "unexpected output artifacts: %v %v"
)

type localDoer struct {
	serverURL string
	client    *http.Client
}

func (doer *localDoer) Do(request *http.Request) (*http.Response, error) {
	if request.URL.Host == localIntegrationTestRutubeRu {
		redirected, err := http.NewRequestWithContext(request.Context(), request.Method, doer.serverURL+request.URL.RequestURI(), nil)
		if err != nil {
			return nil, err
		}

		return doer.client.Do(redirected)
	}

	return doer.client.Do(request)
}

func (doer *localDoer) Close() {
	doer.client.CloseIdleConnections()
}

func TestLocalHTTPDownloadAndConversion(test *testing.T) {
	test.Run("complete", func(test *testing.T) {
		testLocalHTTPDownloadAndConversion(test, false)
	})
	test.Run("truncated segment", func(test *testing.T) {
		testLocalHTTPDownloadAndConversion(test, true)
	})
}

func testLocalHTTPDownloadAndConversion(test *testing.T, truncated bool) {
	test.Helper()

	const (
		transportPacketBytes       = 188
		truncationDivisor          = 2
		incompleteError            = "incomplete MP4"
		unexpectedTruncationResult = "truncated segment result: %v"
	)

	operationContext, cancel := context.WithTimeout(context.Background(), localTimeout)
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

	if truncated {
		segmentBody = segmentBody[:len(segmentBody)/(transportPacketBytes*truncationDivisor)*transportPacketBytes]
	}

	handler := http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case localIntegrationTestApiPlayOptions + localID + localIntegrationTestFilenameCharacters:
			_, _ = fmt.Fprintf(responseWriter, localIntegrationTestTitleVideoBalancerM3u8, localTitle, localIntegrationTestHttp+request.Host+localIntegrationTestMasterM3u8)
		case localIntegrationTestMasterM3u8:
			http.Redirect(responseWriter, request, localIntegrationTestMasterMasterM3u8, http.StatusFound)
		case localIntegrationTestMasterMasterM3u8:
			_, _ = fmt.Fprint(responseWriter, localMaster)
		case localIntegrationTestMasterMediaM3u8:
			http.Redirect(responseWriter, request, localIntegrationTestMediaMediaM3u8, http.StatusFound)
		case localIntegrationTestMediaMediaM3u8:
			_, _ = fmt.Fprint(responseWriter, localMedia)
		case localIntegrationTestMediaSegmentTs:
			_, _ = responseWriter.Write(segmentBody)
		default:
			http.NotFound(responseWriter, request)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	httpClient := server.Client()

	doer := &localDoer{serverURL: server.URL, client: httpClient}
	defer doer.Close()

	reader, readerConstructorError := client.New(doer)
	if readerConstructorError != nil {
		test.Fatal(readerConstructorError)
	}

	temporaryRoot := test.TempDir()
	outputDirectory := test.TempDir()

	test.Setenv(localTemporaryKey, temporaryRoot)

	provider, providerConstructorError := rutube.New(reader)
	if providerConstructorError != nil {
		test.Fatal(providerConstructorError)
	}

	streams, streamsConstructorError := hls.New(reader, doer)
	if streamsConstructorError != nil {
		test.Fatal(streamsConstructorError)
	}

	converter := ffmpeg.New(nil)
	logger := progress.NewLogger()

	service, err := download.New(provider, streams, converter, logger)
	if err != nil {
		test.Fatal(err)
	}

	err = service.Video(operationContext, localID, download.Options{OutputDirectory: outputDirectory})
	if truncated {
		if err == nil || !strings.Contains(err.Error(), incompleteError) {
			test.Fatalf(unexpectedTruncationResult, err)
		}
	} else if err != nil {
		test.Fatal(err)
	}

	target := filepath.Join(outputDirectory, localTitle+localIntegrationTestMp4)

	if truncated {
		if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
			test.Fatalf(unexpectedTruncationResult, err)
		}
	} else {
		probe := exec.CommandContext(operationContext, localIntegrationTestFfprobe, localIntegrationTestV, localIntegrationTestError, localIntegrationTestShowEntries, localIntegrationTestFormatDuration, target)
		if output, err := probe.CombinedOutput(); err != nil {
			test.Fatalf(localIntegrationTestProbe, err, output)
		}
	}

	entries, err := os.ReadDir(temporaryRoot)
	if err != nil || len(entries) != 0 {
		test.Fatalf(localIntegrationTestTemporarySegmentsRemain, entries, err)
	}

	expectedOutputs := 1
	if truncated {
		expectedOutputs = 0
	}

	entries, err = os.ReadDir(outputDirectory)
	if err != nil || len(entries) != expectedOutputs {
		test.Fatalf(localIntegrationTestUnexpectedOutputArtifacts, entries, err)
	}
}
