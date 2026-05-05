package run

import (
	"fmt"
	"strings"
)

type argType int

const (
	notExpected argType = iota
	singleVideo
	playlist
)

// playlist link: https://rutube.ru/plst/742811/
// single video link: https://rutube.ru/video/9ca71e90182fa8649d1db2c9e5e043fd/
func analyzeArgURL(link string) argType {
	const prefix = "https://rutube.ru/"

	if !strings.HasPrefix(link, prefix) {
		return notExpected
	}

	const (
		playlistLink = "plst"
		videoLink    = "video"
	)

	afterPrefix := link[len(prefix):]
	switch {
	case strings.HasPrefix(afterPrefix, playlistLink):
		return playlist
	case strings.HasPrefix(afterPrefix, videoLink):
		return singleVideo
	}

	return notExpected
}

func getVideoIDFromArgURL(url string) (string, error) {
	url = strings.TrimSuffix(url, "/")

	i := strings.LastIndex(url, "/")
	if i == -1 {
		return "", fmt.Errorf("cannot find the video id in ulr: %s", url)
	}

	return url[i+1:], nil
}

func getPlaylistIDFromArgURL(url string) (string, error) {
	url = strings.TrimSuffix(url, "/")

	i := strings.LastIndex(url, "/")
	if i == -1 {
		return "", fmt.Errorf("cannot find the playlist id in ulr: %s", url)
	}

	return url[i+1:], nil
}
