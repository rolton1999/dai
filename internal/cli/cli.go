package cli

import (
	"dai/internal/download"
	"dai/internal/progress"
	"fmt"
)

var (
	errWithoutArgs error = fmt.Errorf("used without args, enter some link")
	errEmptyLink   error = fmt.Errorf("url should not be empty")
)

func Run(arguments []string) error {
	if len(arguments) == 0 {
		return errWithoutArgs
	}

	link := arguments[0]
	if link == "" {
		return errEmptyLink
	}

	linkType, contentID, err := parseLink(link)
	if err != nil {
		return err
	}

	logger := progress.NewLogger()
	if linkType == singleVideo {
		return download.DownloadSingleVideo(logger, contentID)
	}

	return download.DownloadPlaylist(logger, contentID)
}
