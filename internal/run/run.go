package run

import (
	"dai/internal/download"
	"dai/internal/util"
	"fmt"
	"os"
)

var (
	withoutArgs            error = fmt.Errorf("used without args, enter some link")
	emptyLink              error = fmt.Errorf("url should not be empty")
	notExpectedContentLink error = fmt.Errorf("not expected content link: only video and playlist are supported")
)

func Run() error {
	if len(os.Args) == 0 {
		return withoutArgs
	}

	url := os.Args[1]
	if len(url) == 0 {
		return emptyLink
	}

	linkType := analyzeArgURL(url)

	logger := util.NewLogger()

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
		return notExpectedContentLink
	}

	return nil
}
