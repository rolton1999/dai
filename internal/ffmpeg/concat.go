package ffmpeg

import (
	"fmt"
	"os"
	"os/exec"

	"dai/internal/util"
)

func ConcatTSToResult(logger util.Logger, tsFiles []string, resultName string) error {
	defer func() {
		for _, tsFile := range tsFiles {
			_ = os.RemoveAll(tsFile)
		}
	}()

	stopConvertingMessage := util.SpinningMessage(logger, " ~ converting", ": READY")

	// write concat list file for ffmpeg concat demuxer
	listPath := ".concat.txt"

	listF, err := os.Create(listPath)
	if err != nil {
		return fmt.Errorf("cannot create list file: %w", err)
	}
	defer func() {
		listF.Close()
		_ = os.Remove(listPath)
	}()

	// ffmpeg concat demuxer requires: file 'path'
	for _, t := range tsFiles {
		fmt.Fprintf(listF, "file '%s'\n", t)
	}

	cmd := exec.Command(
		"ffmpeg", "-f", "concat", "-safe", "0", "-i", listPath,
		"-c", "copy", "-bsf:a", "aac_adtstoasc", resultName)
	// cmd.Stdout = os.Stdout
	// cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cannot exec ffmpeg: %w", err)
	}

	stopConvertingMessage()

	return nil
}
