//go:build integration

package ffmpeg_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dai/internal/ffmpeg"
)

const (
	fixtureTimeout      = 30 * time.Second
	fixtureFFmpeg       = "ffmpeg"
	fixtureFFprobe      = "ffprobe"
	fixtureSegmentName  = "segment.ts"
	fixturePlainOutput  = "video.mp4"
	fixtureColonOutput  = "6:57 video.mp4"
	fixtureHyphenOutput = "-video.mp4"
	fixtureInvalidData  = "invalid transport stream"
	fixturePermissions  = 0o600
)

type fixtureLogger struct {
	suffixes []string
}

func (*fixtureLogger) NewPrefix(string) {}
func (*fixtureLogger) PrintNewLine()    {}
func (logger *fixtureLogger) UpdateWithSuffix(suffix string) {
	logger.suffixes = append(logger.suffixes, suffix)
}

func TestConcatAcceptsColonAndOptionLikeOutputNames(test *testing.T) {
	for _, name := range []string{fixturePlainOutput, fixtureColonOutput, fixtureHyphenOutput} {
		test.Run(name, func(test *testing.T) {
			workspace := test.TempDir()
			test.Chdir(workspace)
			segment := filepath.Join(workspace, fixtureSegmentName)

			operationContext, cancel := context.WithTimeout(context.Background(), fixtureTimeout)
			defer cancel()

			command := exec.CommandContext(operationContext, fixtureFFmpeg,
				"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=black:s=16x16:r=1",
				"-t", "1", "-c:v", "mpeg2video", "-f", "mpegts", segment)
			if output, err := command.CombinedOutput(); err != nil {
				test.Fatalf("cannot generate fixture: %v\n%s", err, output)
			}

			if err := ffmpeg.ConcatTSToResult(&fixtureLogger{}, []string{segment}, name); err != nil {
				test.Fatal(err)
			}

			probe := exec.CommandContext(operationContext, fixtureFFprobe, "-v", "error", "-show_entries", "format=duration", filepath.Join(workspace, name))
			if output, err := probe.CombinedOutput(); err != nil {
				test.Fatalf("invalid output: %v\n%s", err, output)
			}
		})
	}
}

func TestConcatIncludesDiagnosticsAndDoesNotReportSuccessOnFailure(test *testing.T) {
	workspace := test.TempDir()
	test.Chdir(workspace)

	segment := filepath.Join(workspace, fixtureSegmentName)
	if err := os.WriteFile(segment, []byte(fixtureInvalidData), fixturePermissions); err != nil {
		test.Fatal(err)
	}

	logger := &fixtureLogger{}

	err := ffmpeg.ConcatTSToResult(logger, []string{segment}, fixturePlainOutput)
	if err == nil || !strings.Contains(err.Error(), fixtureSegmentName) {
		test.Fatalf("expected FFmpeg diagnostics identifying the bad input, got %v", err)
	}

	for _, suffix := range logger.suffixes {
		if strings.Contains(suffix, "READY") {
			test.Fatal("failed conversion reported success")
		}
	}
}
