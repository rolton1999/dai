package cli

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type argType int

const (
	notExpected argType = iota
	singleVideo
	playlist
	rutubeHost           = "rutube.ru"
	pathSeparator        = "/"
	expectedPathParts    = 3
	videoIDExpression    = `^[a-fA-F0-9]{32}$`
	playlistIDExpression = `^[1-9][0-9]*$`
	secureScheme         = "https"
	videoPath            = "video"
	playlistPath         = "plst"
	invalidLinkError     = "invalid Rutube video or playlist URL: %q"
)

var videoIDPattern = regexp.MustCompile(videoIDExpression)
var playlistIDPattern = regexp.MustCompile(playlistIDExpression)

func parseLink(link string) (argType, string, error) {
	parsed, err := url.Parse(link)
	if err != nil || parsed.Scheme != secureScheme || !strings.EqualFold(parsed.Host, rutubeHost) || parsed.User != nil || parsed.RawPath != "" {
		return notExpected, "", fmt.Errorf(invalidLinkError, link)
	}

	parts := strings.Split(strings.TrimSuffix(parsed.Path, pathSeparator), pathSeparator)
	if len(parts) != expectedPathParts || parts[0] != "" {
		return notExpected, "", fmt.Errorf(invalidLinkError, link)
	}

	if parts[1] == videoPath && videoIDPattern.MatchString(parts[2]) {
		return singleVideo, parts[2], nil
	}

	if parts[1] == playlistPath && playlistIDPattern.MatchString(parts[2]) {
		return playlist, parts[2], nil
	}

	return notExpected, "", fmt.Errorf(invalidLinkError, link)
}
