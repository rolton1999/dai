package main

import (
	"fmt"
	"os"
)

var (
	withoutArgs            error = fmt.Errorf("used without args, enter some link")
	emptyLink              error = fmt.Errorf("url should not be empty")
	notExpectedContentLink error = fmt.Errorf("not expected content link: only video and playlist are supported")
)

func main() {
	if err := run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) == 0 {
		return withoutArgs
	}

	url := os.Args[1]
	if len(url) == 0 {
		return emptyLink
	}

	linkType := analyzeArgURL(url)

	switch linkType {
	case singleVideo:
		videoID, err := getVideoIDFromArgURL(url)
		if err != nil {
			return err
		}

		err = downloadSingleVideo(videoID)
		if err != nil {
			return err
		}

	case playlist:
		_, err := getPlaylistIDFromArgURL(url)

		return err
	case notExpected:
		return notExpectedContentLink
	}

	return nil
}
