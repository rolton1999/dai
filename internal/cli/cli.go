package cli

import (
	"dai/internal/download"
	"dai/internal/progress"
	"fmt"
)

var (
	errWithoutArgs            error = fmt.Errorf("used without args, enter some link")
	errEmptyLink              error = fmt.Errorf("url should not be empty")
	errNotExpectedContentLink error = fmt.Errorf("not expected content link: only video and playlist are supported")
)

func Run(arguments []string) error {
	if len(arguments) == 0 {
		return errWithoutArgs
	}

	url := arguments[0]
	if len(url) == 0 {
		return errEmptyLink
	}

	linkType := analyzeArgURL(url)

	logger := progress.NewLogger()

	switch linkType {
	case singleVideo:
		videoID, err := getVideoIDFromArgURL(url)
		if err != nil {
			return err
		}

		return download.DownloadSingleVideo(logger, videoID)

	case playlist:
		playlistID, err := getPlaylistIDFromArgURL(url)
		if err != nil {
			return err
		}

		return download.DownloadPlaylist(logger, playlistID)

	case notExpected:
		return errNotExpectedContentLink
	}

	return nil
}
