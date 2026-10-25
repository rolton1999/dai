package ffmpeg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

const (
	fixtureSegment    = "segment's.ts"
	fixtureResult     = "result.mp4"
	fixtureData       = "media"
	validProbe        = `{"format":{"format_name":"mov,mp4","duration":"1"},"streams":[{"codec_type":"video"}]}`
	fixturePermission = 0o600
)

const (
	concatTestBadProbe                             = "bad probe"
	concatTestEmptyProbe                           = `{}`
	concatTestI                                    = "-i"
	concatTestProcessFailure                       = "process failure"
	concatTestFixtureDiagnostic                    = "fixture diagnostic"
	concatTestFixtureExit                          = "fixture exit"
	concatTestNoOutput                             = "no output"
	concatTestSuccess                              = "success"
	concatTestUnexpectedConversionResult           = "unexpected conversion result: %v"
	concatTestMissingDiagnostic                    = "missing diagnostic: %v"
	concatTestInputWasRemoved                      = "input was removed"
	concatTestManifestRemained                     = "manifest remained: %v"
	concatTestEscapedQuote                         = `'\''`
	concatTestUnescapedManifest                    = "unescaped manifest: %q"
	concatTestPartialOutputRemained                = "partial output remained: %v"
	concatTestAcceptedExistingOutput               = "accepted existing output"
	concatTestExistingOutputChanged                = "existing output changed: %q %v"
	concatTestStartedConversionForExistingOutput   = "started conversion for existing output"
	concatTestResultMp4                            = "result-%d.mp4"
	concatTestSharedManifest                       = "shared manifest %q"
	concatTestDaiFixtureExecutableThatDoesNotExist = "dai-fixture-executable-that-does-not-exist"
	concatTestMissingExecutableSucceeded           = "missing executable succeeded"
)

const (
	conversionModeEmptyOutput     = "empty output"
	conversionModeMalformedProbe  = "malformed probe"
	conversionModeProbeFailure    = "probe failure"
	conversionModeTimeout         = "timeout"
	conversionFixtureTimeout      = 5 * time.Second
	conversionInvalidProbe        = "invalid JSON"
	conversionFalseFormatProbe    = `{"format":{"format_name":"notmp4","duration":"1"},"streams":[{"codec_type":"video"}]}`
	conversionZeroDurationProbe   = `{"format":{"format_name":"mp4","duration":"0"},"streams":[{"codec_type":"video"}]}`
	conversionAudioOnlyProbe      = `{"format":{"format_name":"mp4","duration":"1"},"streams":[{"codec_type":"audio"}]}`
	conversionInvalidProbeMessage = "invalid probe accepted: %v"
	conversionTimeoutMessage      = "conversion timeout lost: %v"
	conversionArtifactsMessage    = "unexpected conversion artifacts: %v, %v"
	conversionPreCanceledMessage  = "canceled conversion started a process: %v"
)

type fixtureRunner struct {
	mode         string
	manifest     string
	manifestBody string
}

func (runner *fixtureRunner) Run(operationContext context.Context, name string, arguments []string) ([]byte, error) {
	if runner.mode == conversionModeTimeout {
		<-operationContext.Done()
		return nil, operationContext.Err()
	}

	if name == ffprobeName {
		switch runner.mode {
		case conversionModeMalformedProbe:
			return []byte(conversionInvalidProbe), nil
		case conversionModeProbeFailure:
			return nil, errors.New(concatTestFixtureExit)
		case conversionFalseFormatProbe, conversionZeroDurationProbe, conversionAudioOnlyProbe:
			return []byte(runner.mode), nil
		}

		if runner.mode == concatTestBadProbe {
			return []byte(concatTestEmptyProbe), nil
		}

		return []byte(validProbe), nil
	}

	for index, argument := range arguments {
		if argument == concatTestI {
			runner.manifest = arguments[index+1]

			body, err := os.ReadFile(runner.manifest)
			if err != nil {
				return nil, err
			}

			runner.manifestBody = string(body)
		}
	}

	if runner.mode == concatTestProcessFailure {
		return []byte(concatTestFixtureDiagnostic), errors.New(concatTestFixtureExit)
	}

	if runner.mode == concatTestNoOutput {
		return nil, nil
	}

	body := []byte(fixtureData)
	if runner.mode == conversionModeEmptyOutput {
		body = nil
	}

	return nil, os.WriteFile(arguments[len(arguments)-1], body, fixturePermission)
}

func TestConversionFailureAndOwnership(test *testing.T) {
	for _, mode := range []string{concatTestSuccess, concatTestProcessFailure, concatTestNoOutput, concatTestBadProbe} {
		test.Run(mode, func(test *testing.T) {
			directory := test.TempDir()
			segment := filepath.Join(directory, fixtureSegment)
			result := filepath.Join(directory, fixtureResult)

			if err := os.WriteFile(segment, []byte(fixtureData), fixturePermission); err != nil {
				test.Fatal(err)
			}

			runner := &fixtureRunner{mode: mode}

			converter := New(runner)
			logger := &conversionLogger{}

			err := converter.Convert(context.Background(), logger, []string{segment}, result)
			if (err == nil) != (mode == concatTestSuccess) {
				test.Fatalf(concatTestUnexpectedConversionResult, err)
			}

			if mode == concatTestProcessFailure && !strings.Contains(err.Error(), concatTestFixtureDiagnostic) {
				test.Fatalf(concatTestMissingDiagnostic, err)
			}

			if _, err := os.Stat(segment); err != nil {
				test.Fatal(concatTestInputWasRemoved, err)
			}

			if _, err := os.Stat(runner.manifest); !errors.Is(err, os.ErrNotExist) {
				test.Fatalf(concatTestManifestRemained, err)
			}

			if !strings.Contains(runner.manifestBody, concatTestEscapedQuote) {
				test.Fatalf(concatTestUnescapedManifest, runner.manifestBody)
			}

			_, statErr := os.Stat(result)
			if mode != concatTestSuccess && !errors.Is(statErr, os.ErrNotExist) {
				test.Fatalf(concatTestPartialOutputRemained, statErr)
			}
		})
	}
}

func TestConversionPreservesExistingOutput(test *testing.T) {
	result := filepath.Join(test.TempDir(), fixtureResult)
	if err := os.WriteFile(result, []byte(fixtureData), fixturePermission); err != nil {
		test.Fatal(err)
	}

	runner := &fixtureRunner{}
	converter := New(runner)

	logger := &conversionLogger{}
	if err := converter.Convert(context.Background(), logger, []string{result}, result); err == nil {
		test.Fatal(concatTestAcceptedExistingOutput)
	}

	body, err := os.ReadFile(result)
	if err != nil || string(body) != fixtureData {
		test.Fatalf(concatTestExistingOutputChanged, body, err)
	}

	if runner.manifest != "" {
		test.Fatal(concatTestStartedConversionForExistingOutput)
	}
}

func TestConcurrentConversionsUseSeparateManifests(test *testing.T) {
	const parallelConversions = 8

	directory := test.TempDir()

	segment := filepath.Join(directory, fixtureSegment)
	if err := os.WriteFile(segment, []byte(fixtureData), fixturePermission); err != nil {
		test.Fatal(err)
	}

	results := make(chan error, parallelConversions)

	manifests := make(chan string, parallelConversions)
	for index := range parallelConversions {
		go func() {
			runner := &fixtureRunner{mode: concatTestSuccess}
			result := filepath.Join(directory, fmt.Sprintf(concatTestResultMp4, index))

			converter := New(runner)
			logger := &conversionLogger{}
			err := converter.Convert(context.Background(), logger, []string{segment}, result)

			manifests <- runner.manifest

			results <- err
		}()
	}

	for range parallelConversions {
		if err := <-results; err != nil {
			test.Error(err)
		}
	}

	seen := make(map[string]bool)

	for range parallelConversions {
		name := <-manifests
		if seen[name] {
			test.Errorf(concatTestSharedManifest, name)
		}

		seen[name] = true
	}
}

func TestRunnerMissingExecutable(test *testing.T) {
	const missingExecutable = concatTestDaiFixtureExecutableThatDoesNotExist

	_, err := (&execRunner{}).Run(context.Background(), missingExecutable, nil)
	if err == nil {
		test.Fatal(concatTestMissingExecutableSucceeded)
	}
}

type conversionLogger struct{}

func (*conversionLogger) NewPrefix(string) {
}

func (*conversionLogger) UpdateWithSuffix(string) {
}

func (*conversionLogger) PrintNewLine() {
}

func TestConversionRejectsInvalidOutputs(test *testing.T) {
	for _, mode := range []string{conversionModeEmptyOutput, conversionModeMalformedProbe, conversionModeProbeFailure, conversionFalseFormatProbe, conversionZeroDurationProbe, conversionAudioOnlyProbe} {
		test.Run(mode, func(test *testing.T) {
			directory := test.TempDir()
			segment := filepath.Join(directory, fixtureSegment)
			result := filepath.Join(directory, fixtureResult)

			if err := os.WriteFile(segment, []byte(fixtureData), fixturePermission); err != nil {
				test.Fatal(err)
			}

			runner := &fixtureRunner{mode: mode}
			converter := New(runner)

			logger := &conversionLogger{}
			if err := converter.Convert(test.Context(), logger, []string{segment}, result); err == nil {
				test.Fatalf(conversionInvalidProbeMessage, err)
			}

			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 1 || entries[0].Name() != fixtureSegment {
				test.Fatalf(conversionArtifactsMessage, entries, err)
			}
		})
	}
}

func TestConversionHonorsDeadline(test *testing.T) {
	synctest.Test(test, func(test *testing.T) {
		directory := test.TempDir()
		segment := filepath.Join(directory, fixtureSegment)
		result := filepath.Join(directory, fixtureResult)

		if err := os.WriteFile(segment, []byte(fixtureData), fixturePermission); err != nil {
			test.Fatal(err)
		}

		runner := &fixtureRunner{mode: conversionModeTimeout}
		converter := New(runner)
		logger := &conversionLogger{}

		operationContext, cancel := context.WithTimeout(test.Context(), conversionFixtureTimeout)
		defer cancel()

		if err := converter.Convert(operationContext, logger, []string{segment}, result); !errors.Is(err, context.DeadlineExceeded) {
			test.Fatalf(conversionTimeoutMessage, err)
		}

		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 1 {
			test.Fatalf(conversionArtifactsMessage, entries, err)
		}
	})
}

func TestConversionCanceledBeforeStarting(test *testing.T) {
	operationContext, cancel := context.WithCancel(test.Context())
	cancel()

	runner := &fixtureRunner{}
	converter := New(runner)
	logger := &conversionLogger{}

	result := filepath.Join(test.TempDir(), fixtureResult)
	if err := converter.Convert(operationContext, logger, nil, result); !errors.Is(err, context.Canceled) || runner.manifest != "" {
		test.Fatalf(conversionPreCanceledMessage, err)
	}
}
