package ffmpeg

import (
	"context"
	"dai/internal/progress"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	noStdinFlag          = "-nostdin"
	overwriteStagingFlag = "-y"
	failOnErrorFlag      = "-xerror"
	inputFormatFlag      = "-f"
	concatFormat         = "concat"
	concatSafetyFlag     = "-safe"
	allowAbsolutePaths   = "0"
	inputFlag            = "-i"
	codecFlag            = "-c"
	copyCodec            = "copy"
	audioFilterFlag      = "-bsf:a"
	aacBitstreamFilter   = "aac_adtstoasc"
	verbosityFlag        = "-v"
	errorVerbosity       = "error"
	probeEntriesFlag     = "-show_entries"
	probeEntries         = "format=format_name,duration:stream=codec_type"
	probeFormatFlag      = "-of"
	probeJSONFormat      = "json"
	manifestEntryFormat  = "file '%s'\n"
	lineBreaks           = "\r\n"
	concatQuote          = "'"
	escapedConcatQuote   = "'\\''"
)

const (
	conversionFilePermissions = 0o666
	expectedFormat            = "mp4"
	formatSeparator           = ","
	probeNumberBitSize        = 64
	videoCodecType            = "video"
)

const (
	cannotConvertAnEmptySegmentListMessage    = "cannot convert an empty segment list"
	conversionDestinationAlreadyExistsMessage = "conversion destination already exists"
	missingOrEmptySegmentMessage              = "missing or empty segment"
	segmentPathContainsANewlineMessage        = "segment path contains a newline"
	ffmpegDidNotProduceOutputFormat           = "FFmpeg did not produce output: %w"
	ffmpegProducedAnEmptyOutputMessage        = "FFmpeg produced an empty output"
	cannotValidateMP4Format                   = "cannot validate MP4: %w: %s"
	invalidMP4OutputMessage                   = "invalid MP4 output"
	cannotExecFFmpegFormat                    = "cannot exec FFmpeg: %w: %s"
)

const (
	conversionTimeout = 3 * time.Minute
	probeTimeout      = 30 * time.Second
	manifestPattern   = ".concat-*.txt"
	conversionMessage = " ~ converting"
	ffmpegName        = "ffmpeg"
	ffprobeName       = "ffprobe"
)

type processRunner interface {
	Run(context.Context, string, []string) ([]byte, error)
}

// Convert writes a validated MP4 to a new staging path in a caller-owned
// directory. The caller publishes the file only after Convert succeeds.
type Converter interface {
	Convert(
		context.Context,
		progress.Logger,
		[]string,
		string,
	) error
}

func New(runner processRunner) Converter {
	if runner == nil {
		runner = &execRunner{}
	}

	return &converterImpl{runner: runner}
}

type converterImpl struct {
	runner processRunner
}

func (converter *converterImpl) Convert(
	operationContext context.Context,
	logger progress.Logger,
	segments []string,
	resultName string,
) (resultError error) {
	if err := operationContext.Err(); err != nil {
		return err
	}

	operationContext, cancel := context.WithTimeout(operationContext, conversionTimeout)
	defer cancel()

	resultPath, err := validateConversionInputs(segments, resultName)
	if err != nil {
		return err
	}

	staging, err := os.OpenFile(resultPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, conversionFilePermissions)
	if err != nil {
		return err
	}

	converted := false
	defer func() {
		if !converted {
			resultError = errors.Join(resultError, os.Remove(resultPath))
		}
	}()

	if err := staging.Close(); err != nil {
		return err
	}

	manifestPath, err := createConcatManifest(segments)
	if err != nil {
		return err
	}

	defer func() {
		resultError = errors.Join(resultError, os.Remove(manifestPath))
	}()

	arguments := conversionArguments(manifestPath, resultPath)
	if err := converter.convert(operationContext, logger, arguments); err != nil {
		return err
	}

	if err := converter.validateOutput(operationContext, resultPath); err != nil {
		return err
	}

	if err := operationContext.Err(); err != nil {
		return err
	}

	converted = true

	return nil
}

func (converter *converterImpl) validateOutput(operationContext context.Context, stagedPath string) error {
	info, err := os.Stat(stagedPath)
	if err != nil {
		return fmt.Errorf(ffmpegDidNotProduceOutputFormat, err)
	}

	if !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New(ffmpegProducedAnEmptyOutputMessage)
	}

	probeContext, probeCancel := context.WithTimeout(operationContext, probeTimeout)
	defer probeCancel()

	output, err := converter.runner.Run(probeContext, ffprobeName, []string{verbosityFlag, errorVerbosity, probeEntriesFlag, probeEntries, probeFormatFlag, probeJSONFormat, stagedPath})
	if err != nil {
		return fmt.Errorf(cannotValidateMP4Format, err, output)
	}

	var probe struct {
		Format struct {
			FormatName string `json:"format_name"`
			Duration   string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output, &probe); err != nil {
		return err
	}

	duration, err := strconv.ParseFloat(probe.Format.Duration, probeNumberBitSize)

	hasVideo := false
	for _, stream := range probe.Streams {
		hasVideo = hasVideo || stream.CodecType == videoCodecType
	}

	if err != nil || duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) || !hasVideo || !containsMP4Format(probe.Format.FormatName) {
		return errors.New(invalidMP4OutputMessage)
	}

	return nil
}

func (converter *converterImpl) convert(operationContext context.Context, logger progress.Logger, arguments []string) error {
	stop := progress.SpinningMessageContext(operationContext, logger, conversionMessage, "")
	defer stop()

	output, err := converter.runner.Run(operationContext, ffmpegName, arguments)
	if err != nil {
		return fmt.Errorf(cannotExecFFmpegFormat, err, output)
	}

	return nil
}

func containsMP4Format(formats string) bool {
	for _, format := range strings.Split(formats, formatSeparator) {
		if format == expectedFormat {
			return true
		}
	}

	return false
}

func validateConversionInputs(segments []string, resultName string) (string, error) {
	if len(segments) == 0 {
		return "", errors.New(cannotConvertAnEmptySegmentListMessage)
	}

	resultPath, err := filepath.Abs(resultName)
	if err != nil {
		return "", err
	}

	if _, err := os.Lstat(resultPath); err == nil {
		return "", errors.New(conversionDestinationAlreadyExistsMessage)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	for _, segment := range segments {
		info, err := os.Stat(segment)
		if err != nil {
			return "", err
		}

		if !info.Mode().IsRegular() || info.Size() == 0 {
			return "", errors.New(missingOrEmptySegmentMessage)
		}
	}

	return resultPath, nil
}

func createConcatManifest(segments []string) (manifestPath string, resultError error) {
	manifest, err := os.CreateTemp(filepath.Dir(segments[0]), manifestPattern)
	if err != nil {
		return "", err
	}

	defer func() {
		_ = manifest.Close()
		if resultError != nil {
			resultError = errors.Join(resultError, os.Remove(manifest.Name()))
		}
	}()

	if err := writeConcatManifest(manifest, segments); err != nil {
		return "", err
	}

	return manifest.Name(), nil
}

func writeConcatManifest(manifest *os.File, segments []string) error {
	for _, segment := range segments {
		absolute, err := filepath.Abs(segment)
		if err != nil {
			return err
		}

		if strings.ContainsAny(absolute, lineBreaks) {
			return errors.New(segmentPathContainsANewlineMessage)
		}

		escaped := strings.ReplaceAll(absolute, concatQuote, escapedConcatQuote)
		if _, err := fmt.Fprintf(manifest, manifestEntryFormat, escaped); err != nil {
			return err
		}
	}

	if err := manifest.Close(); err != nil {
		return err
	}

	return nil
}

func conversionArguments(manifestPath string, stagedPath string) []string {
	return []string{
		noStdinFlag, overwriteStagingFlag, failOnErrorFlag,
		inputFormatFlag, concatFormat,
		concatSafetyFlag, allowAbsolutePaths,
		inputFlag, manifestPath,
		codecFlag, copyCodec,
		audioFilterFlag, aacBitstreamFilter,
		stagedPath,
	}
}
