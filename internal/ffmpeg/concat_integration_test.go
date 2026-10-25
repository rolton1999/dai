//go:build integration

package ffmpeg_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dai/internal/ffmpeg"
)

const (
	corruptSegmentName   = "corrupt.ts"
	corruptMiddleCase    = "middle"
	corruptLastCase      = "last"
	corruptPartialOutput = "corrupt segment published a partial output: %v"
	corruptArtifacts     = "conversion left unexpected artifacts: %v, %v"
	corruptInputCount    = 2
)

func TestConcatRejectsCorruptSegmentsAfterValidInput(test *testing.T) {
	for _, position := range []string{corruptMiddleCase, corruptLastCase} {
		test.Run(position, func(test *testing.T) {
			operationContext, cancel := context.WithTimeout(test.Context(), fixtureTimeout)
			defer cancel()

			directory := test.TempDir()
			goodSegment := filepath.Join(directory, fixtureSegmentName)
			badSegment := filepath.Join(directory, corruptSegmentName)
			output := filepath.Join(directory, fixturePlainOutput)

			arguments := append(strings.Fields(fixtureGenerateArguments), goodSegment)

			command := exec.CommandContext(operationContext, fixtureFFmpeg, arguments...)
			if diagnostics, err := command.CombinedOutput(); err != nil {
				test.Fatalf(fixtureGenerateError, err, diagnostics)
			}

			if err := os.WriteFile(badSegment, []byte(fixtureInvalidData), fixturePermissions); err != nil {
				test.Fatal(err)
			}

			segments := []string{goodSegment, badSegment}
			if position == corruptMiddleCase {
				segments = append(segments, goodSegment)
			}

			converter := ffmpeg.New(nil)

			logger := &fixtureLogger{}
			if err := converter.Convert(operationContext, logger, segments, output); err == nil {
				test.Fatal(fixtureFalseSuccessError)
			}

			if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
				test.Fatalf(corruptPartialOutput, err)
			}

			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != corruptInputCount {
				test.Fatalf(corruptArtifacts, entries, err)
			}

			for _, segment := range []string{goodSegment, badSegment} {
				if _, err := os.Stat(segment); err != nil {
					test.Fatalf(fixtureInputRemovedError, err)
				}
			}
		})
	}
}

const (
	fixtureTimeout           = 30 * time.Second
	fixtureFFmpeg            = "ffmpeg"
	fixtureFFprobe           = "ffprobe"
	fixtureSegmentName       = "segment's.ts"
	fixturePlainOutput       = "video.mp4"
	fixtureColonOutput       = "6:57 video.mp4"
	fixtureHyphenOutput      = "-video.mp4"
	fixtureInvalidData       = "invalid transport stream"
	fixturePermissions       = 0o600
	fixtureGenerateArguments = "-hide_banner -loglevel error -f lavfi -i color=c=black:s=16x16:r=1 -t 1 -c:v mpeg2video -f mpegts"
	fixtureProbeArguments    = "-v error -show_entries format=duration"
	fixtureSuccessToken      = "READY"
	fixtureGenerateError     = "cannot generate fixture: %v\n%s"
	fixtureProbeError        = "invalid output: %v\n%s"
	fixtureDiagnosticsError  = "expected FFmpeg diagnostics identifying the bad input, got %v"
	fixtureFalseSuccessError = "failed conversion reported success"
	fixtureInputRemovedError = "converter removed caller-owned input: %v"
)

type fixtureLogger struct {
	suffixes []string
}

func (*fixtureLogger) NewPrefix(string) {
}

func (*fixtureLogger) PrintNewLine() {
}

func (logger *fixtureLogger) UpdateWithSuffix(suffix string) {
	logger.suffixes = append(logger.suffixes, suffix)
}

func TestConcatAcceptsColonAndOptionLikeOutputNames(test *testing.T) {
	for _, name := range []string{
		fixturePlainOutput,
		fixtureColonOutput,
		fixtureHyphenOutput,
	} {
		test.Run(name, func(test *testing.T) {
			workspace := test.TempDir()
			test.Chdir(workspace)
			segment := filepath.Join(workspace, fixtureSegmentName)

			operationContext, cancel := context.WithTimeout(context.Background(), fixtureTimeout)
			defer cancel()

			arguments := append(strings.Fields(fixtureGenerateArguments), segment)

			command := exec.CommandContext(operationContext, fixtureFFmpeg, arguments...)
			if output, err := command.CombinedOutput(); err != nil {
				test.Fatalf(fixtureGenerateError, err, output)
			}

			converter := ffmpeg.New(nil)

			logger := &fixtureLogger{}
			if err := converter.Convert(operationContext, logger, []string{segment}, name); err != nil {
				test.Fatal(err)
			}

			if _, err := os.Stat(segment); err != nil {
				test.Fatalf(fixtureInputRemovedError, err)
			}

			probeArguments := append(strings.Fields(fixtureProbeArguments), filepath.Join(workspace, name))

			probe := exec.CommandContext(operationContext, fixtureFFprobe, probeArguments...)
			if output, err := probe.CombinedOutput(); err != nil {
				test.Fatalf(fixtureProbeError, err, output)
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

	operationContext, cancel := context.WithTimeout(context.Background(), fixtureTimeout)
	defer cancel()

	converter := ffmpeg.New(nil)

	err := converter.Convert(operationContext, logger, []string{segment}, fixturePlainOutput)
	if err == nil || !strings.Contains(err.Error(), fixtureSegmentName) {
		test.Fatalf(fixtureDiagnosticsError, err)
	}

	if _, err := os.Stat(segment); err != nil {
		test.Fatalf(fixtureInputRemovedError, err)
	}

	for _, suffix := range logger.suffixes {
		if strings.Contains(suffix, fixtureSuccessToken) {
			test.Fatal(fixtureFalseSuccessError)
		}
	}
}
