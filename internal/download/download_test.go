package download

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dai/internal/hls"
	"dai/internal/http/client"
	"dai/internal/progress"
	"dai/internal/rutube"
)

const (
	fixtureVideoID            = "fixture-video"
	fixtureVideoPath          = "/api/play/options/fixture-video/"
	fixtureMasterPath         = "/master.m3u8"
	fixtureMediaPath          = "/media.m3u8"
	fixtureSegmentPath        = "/segment.ts"
	fixtureVideoBody          = `{"title":"video","video_balancer":{"m3u8":"https://example.test/master.m3u8"}}`
	fixtureMasterBody         = "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000,RESOLUTION=1280x720\nmedia.m3u8\n"
	fixtureMediaBody          = "#EXTM3U\n#EXTINF:1,\nsegment.ts\n#EXT-X-ENDLIST\n"
	fixtureSegmentBody        = "complete segment"
	fixtureTemporaryKey       = "TMPDIR"
	fixturePermissions        = 0o600
	fixtureUnrelatedName      = "unrelated.ts"
	fixtureConversionExtra    = "conversion.tmp"
	fixtureConversionFailure  = "conversion failed"
	fixtureDeadline           = 5 * time.Second
	unexpectedResultFormat    = "error=%v conversions=%d newlines=%d"
	unexpectedInputFormat     = "input=%q error=%v"
	unexpectedFilesFormat     = "files=%v"
	unexpectedOutputFormat    = "output=%q"
	unexpectedArtifactsFormat = "temporary artifacts=%v error=%v"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (transport fixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type fixtureLogger struct {
	suffixes []string
	newlines int
}

func (*fixtureLogger) NewPrefix(string) {
}

func (logger *fixtureLogger) UpdateWithSuffix(suffix string) {
	logger.suffixes = append(logger.suffixes, suffix)
}

func (logger *fixtureLogger) PrintNewLine() {
	logger.newlines++
}

func TestDownloadOwnsTemporaryFilesThroughConversion(test *testing.T) {
	conversionFailure := errors.New(fixtureConversionFailure)

	fixtures := []struct {
		name            string
		failedPath      string
		conversionError error
		conversions     int
		newlines        int
	}{
		{name: "success", conversions: 1, newlines: 1},
		{name: "conversion failure", conversionError: conversionFailure, conversions: 1},
		{name: "metadata failure", failedPath: fixtureVideoPath},
		{name: "master failure", failedPath: fixtureMasterPath},
		{name: "media failure", failedPath: fixtureMediaPath},
		{name: "segment failure", failedPath: fixtureSegmentPath},
	}
	for _, fixture := range fixtures {
		test.Run(fixture.name, func(test *testing.T) {
			directory := test.TempDir()
			test.Setenv(fixtureTemporaryKey, directory)

			unrelatedPath := filepath.Join(directory, fixtureUnrelatedName)
			if err := os.WriteFile(unrelatedPath, []byte(fixtureSegmentBody), fixturePermissions); err != nil {
				test.Fatal(err)
			}

			originalClient := http.DefaultClient

			test.Cleanup(func() {
				http.DefaultClient = originalClient
			})

			transport := fixtureTransport(func(request *http.Request) (*http.Response, error) {
				bodies := map[string]string{
					fixtureVideoPath:   fixtureVideoBody,
					fixtureMasterPath:  fixtureMasterBody,
					fixtureMediaPath:   fixtureMediaBody,
					fixtureSegmentPath: fixtureSegmentBody,
				}
				status := http.StatusOK

				body, exists := bodies[request.URL.Path]
				if !exists || request.URL.Path == fixture.failedPath {
					status = http.StatusNotFound
				}

				reader := strings.NewReader(body)
				responseBody := io.NopCloser(reader)
				response := &http.Response{
					StatusCode: status,
					Body:       responseBody,
					Request:    request,
				}

				return response, nil
			})
			http.DefaultClient = &http.Client{Transport: transport}

			conversions := 0
			convert := func(
				_ context.Context,
				_ progress.Logger,
				files []string,
				output string,
			) error {
				conversions++

				if len(files) != 1 {
					test.Fatalf(unexpectedFilesFormat, files)
				}

				if filepath.Base(output) != stagingFilename {
					test.Fatalf(unexpectedOutputFormat, output)
				}

				body, err := os.ReadFile(files[0])
				if err != nil || string(body) != fixtureSegmentBody {
					test.Fatalf(unexpectedInputFormat, body, err)
				}

				extraPath := filepath.Join(filepath.Dir(files[0]), fixtureConversionExtra)
				if err := os.WriteFile(extraPath, []byte(fixtureSegmentBody), fixturePermissions); err != nil {
					test.Fatal(err)
				}

				if fixture.conversionError != nil {
					return fixture.conversionError
				}

				return os.WriteFile(output, []byte(fixtureSegmentBody), fixturePermissions)
			}
			logger := &fixtureLogger{}

			operationContext, cancel := context.WithTimeout(context.Background(), fixtureDeadline)
			defer cancel()

			network := client.NewStreamingClient(nil)
			defer network.Close()

			reader, readerConstructorError := client.New(network)
			if readerConstructorError != nil {
				test.Fatal(readerConstructorError)
			}

			provider, providerConstructorError := rutube.New(reader)
			if providerConstructorError != nil {
				test.Fatal(providerConstructorError)
			}

			streams, streamsConstructorError := hls.New(reader, network)
			if streamsConstructorError != nil {
				test.Fatal(streamsConstructorError)
			}

			converter := conversionAdapter(convert)

			service, err := New(provider, streams, converter, logger)
			if err != nil {
				test.Fatal(err)
			}

			outputDirectory := test.TempDir()
			err = service.Video(operationContext, fixtureVideoID, Options{OutputDirectory: outputDirectory})

			expectedFailure := fixture.failedPath != "" || fixture.conversionError != nil
			if (err != nil) != expectedFailure || conversions != fixture.conversions || logger.newlines != fixture.newlines {
				test.Fatalf(unexpectedResultFormat, err, conversions, logger.newlines)
			}

			if fixture.conversionError != nil && !errors.Is(err, fixture.conversionError) {
				test.Fatal(err)
			}

			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 1 || entries[0].Name() != fixtureUnrelatedName {
				test.Fatalf(unexpectedArtifactsFormat, entries, err)
			}

			body, err := os.ReadFile(unrelatedPath)
			if err != nil || string(body) != fixtureSegmentBody {
				test.Fatalf(unexpectedInputFormat, body, err)
			}
		})
	}
}

type conversionAdapter func(
	context.Context,
	progress.Logger,
	[]string,
	string,
) error

func (convert conversionAdapter) Convert(
	operationContext context.Context,
	logger progress.Logger,
	files []string,
	output string,
) error {
	return convert(operationContext, logger, files, output)
}
