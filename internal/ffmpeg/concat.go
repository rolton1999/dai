package ffmpeg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"dai/internal/progress"
)

const (
	outputPathError = "cannot resolve output path: %w"
	conversionError = "cannot exec ffmpeg: %w: %s"
)

func ConcatTSToResult(logger progress.Logger, tsFiles []string, resultName string) error {
	resultPath, err := filepath.Abs(resultName)
	if err != nil {
		return fmt.Errorf(outputPathError, err)
	}

	listPath := ".concat.txt"

	listF, err := os.Create(listPath)
	if err != nil {
		return fmt.Errorf("cannot create list file: %w", err)
	}

	defer func() {
		_ = listF.Close()

		_ = os.Remove(listPath)
	}()

	// ffmpeg concat demuxer requires: file 'path'
	for _, t := range tsFiles {
		if _, err := fmt.Fprintf(listF, "file '%s'\n", t); err != nil {
			return fmt.Errorf("cannot write list file: %w", err)
		}
	}

	if err := listF.Close(); err != nil {
		return fmt.Errorf("cannot close list file: %w", err)
	}

	cmd := exec.Command(
		"ffmpeg", "-f", "concat", "-safe", "0", "-i", listPath,
		"-c", "copy", "-bsf:a", "aac_adtstoasc", resultPath)
	stopConvertingMessage := progress.SpinningMessage(logger, " ~ converting", "")
	output, err := cmd.CombinedOutput()

	stopConvertingMessage()

	if err != nil {
		return fmt.Errorf(conversionError, err, output)
	}

	logger.UpdateWithSuffix(": READY")

	return nil
}
