package main

import (
	"os"
)

func main() {
	if len(os.Args) == 0 {
		panic("enter some url")
	}

	url := os.Args[1]
	if len(url) == 0 {
		panic("url should not be empty")
	}

	linkType := analyzeArgURL(url)
	switch linkType {
	case singleVideo:
		videoID, err := getVideoIDFromArgURL(url)
		if err != nil {
			panic(err)
		}
		downloadSingleVideo(videoID)
	case playlist:
		panic("not implemented")
	}
}
