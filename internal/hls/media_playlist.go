package hls

import (
	"context"
	"dai/internal/retry"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	mediaPlaylistHeader               = "#EXTM3U"
	mediaSegmentDurationTag           = "#EXTINF:"
	mediaCommentPrefix                = "#"
	mediaSegmentExtension             = ".ts"
	mediaSegmentSeparator             = "_"
	mediaPlaylistRequestError         = "cannot get media playlist: %w"
	mediaPlaylistStatusError          = "cannot get media playlist: HTTP %d"
	mediaPlaylistReadError            = "cannot read media playlist: %w"
	mediaPlaylistBaseURLError         = "cannot parse media playlist URL: %w"
	mediaPlaylistHeaderError          = "expected media playlist header"
	mediaPlaylistMissingURIError      = "media segment is missing its URI"
	mediaPlaylistMissingDurationError = "media segment URI is missing EXTINF"
	mediaPlaylistSegmentURLError      = "cannot parse segment URI: %w"
	mediaPlaylistEmptyError           = "media playlist has no segments"
)

func collectSegmentMetas(playlist VariantPlaylist, directoryName, resultName string) ([]segmentMeta, error) {
	operationContext, cancel := context.WithTimeout(context.Background(), playlistRequestTimeout)
	defer cancel()

	var result []segmentMeta

	err := retry.Do(operationContext, func(attemptContext context.Context) error {
		var operationError error

		result, operationError = collectSegmentMetasOnce(attemptContext, playlist, directoryName, resultName)

		return operationError
	})

	return result, err
}

func collectSegmentMetasOnce(
	operationContext context.Context,
	playlist VariantPlaylist,
	directoryName string,
	resultName string,
) ([]segmentMeta, error) {
	request, err := http.NewRequestWithContext(operationContext, http.MethodGet, playlist.Link, nil)
	if err != nil {
		return nil, err
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf(mediaPlaylistRequestError, err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(mediaPlaylistStatusError, response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf(mediaPlaylistReadError, err)
	}

	baseURL, err := url.Parse(playlist.Link)
	if err != nil {
		return nil, fmt.Errorf(mediaPlaylistBaseURLError, err)
	}

	if response.Request != nil && response.Request.URL != nil {
		baseURL = response.Request.URL
	}

	resultName = strings.ReplaceAll(resultName, " ", mediaSegmentSeparator)
	resultName = strings.ReplaceAll(resultName, "/", "")
	filePrefix := filepath.Join(directoryName, resultName)

	return parseSegmentMetas(string(body), baseURL, filePrefix)
}

/*
Rutube media playlists typically contain:

#EXTM3U
#EXT-X-TARGETDURATION:7
#EXT-X-ALLOW-CACHE:YES
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-VERSION:3
#EXT-X-MEDIA-SEQUENCE:1
#EXTINF:3.937,
video.mp4/segment-1-v1-a1.ts
#EXTINF:0.133,
video.mp4/segment-2-v1-a1.ts
#EXT-X-ENDLIST

Header tags and blank lines can vary. Each EXTINF describes the following
segment URI; line positions and the trailing newline do not identify segments.
Segment URIs are resolved against the final playlist URL after redirects.
*/
func parseSegmentMetas(body string, baseURL *url.URL, filePrefix string) ([]segmentMeta, error) {
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if strings.TrimSpace(lines[0]) != mediaPlaylistHeader {
		return nil, errors.New(mediaPlaylistHeaderError)
	}

	var segments []segmentMeta

	waitingForURI := false

	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, mediaSegmentDurationTag) {
			if waitingForURI {
				return nil, errors.New(mediaPlaylistMissingURIError)
			}

			waitingForURI = true

			continue
		}

		if strings.HasPrefix(line, mediaCommentPrefix) {
			continue
		}

		if !waitingForURI {
			return nil, errors.New(mediaPlaylistMissingDurationError)
		}

		segmentURL, err := url.Parse(line)
		if err != nil {
			return nil, fmt.Errorf(mediaPlaylistSegmentURLError, err)
		}

		segmentIndex := len(segments)
		segments = append(segments, segmentMeta{
			index:    segmentIndex,
			link:     baseURL.ResolveReference(segmentURL).String(),
			fileName: filePrefix + mediaSegmentSeparator + strconv.Itoa(segmentIndex) + mediaSegmentExtension,
		})
		waitingForURI = false
	}

	if waitingForURI {
		return nil, errors.New(mediaPlaylistMissingURIError)
	}

	if len(segments) == 0 {
		return nil, errors.New(mediaPlaylistEmptyError)
	}

	return segments, nil
}
