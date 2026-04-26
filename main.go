package main

import (
	"fmt"
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
		fatalIfError(err)

		err = downloadSingleVideo(videoID)
		fatalIfError(err)

	case playlist:
		panic("not implemented")
	}
}

func fatalIfError(err error) {
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
