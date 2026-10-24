package hls

import (
	"context"
	"dai/internal/retry"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	endListTag        = "#EXT-X-ENDLIST"
	keyTag            = "#EXT-X-KEY:"
	methodAttribute   = "METHOD"
	unencryptedMethod = "NONE"
	byteRangeTag      = "#EXT-X-BYTERANGE:"
	mapTag            = "#EXT-X-MAP:"
	partTag           = "#EXT-X-PART:"
	skipTag           = "#EXT-X-SKIP:"
)

const (
	mediaPlaylistHeaderError               = "expected media playlist header"
	invalidSegmentOrderMessage             = "invalid segment order"
	invalidSegmentDurationMessage          = "invalid segment duration"
	mediaPlaylistMissingURIError           = "media segment is missing its URI"
	encryptedHLSIsUnsupportedMessage       = "encrypted HLS is unsupported"
	unsupportedMediaPlaylistFeatureMessage = "unsupported media playlist feature"
	mediaPlaylistMissingDurationError      = "media segment URI is missing EXTINF"
	invalidSegmentURLMessage               = "invalid segment URL"
	incompleteOrLiveMediaPlaylistMessage   = "incomplete or live media playlist"
)

const (
	mediaPlaylistHeader          = "#EXTM3U"
	mediaSegmentDurationTag      = "#EXTINF:"
	mediaCommentPrefix           = "#"
	mediaSegmentExtension        = ".ts"
	mediaSegmentSeparator        = "_"
	mediaPlaylistRequestError    = "cannot get media playlist: %w"
	mediaPlaylistStatusError     = "cannot get media playlist: HTTP %d"
	mediaPlaylistReadError       = "cannot read media playlist: %w"
	mediaPlaylistBaseURLError    = "cannot parse media playlist URL: %w"
	mediaPlaylistSegmentURLError = "cannot parse segment URI: %w"
	mediaPlaylistEmptyError      = "media playlist has no segments"
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
	if baseURL == nil {
		return nil, errors.New(playlistResponseURLIsMissingMessage)
	}

	lines := strings.Split(strings.TrimSpace(body), "\n")
	if strings.TrimSpace(lines[0]) != mediaPlaylistHeader {
		return nil, errors.New(mediaPlaylistHeaderError)
	}

	var segments []segmentMeta

	waitingForURI, ended := false, false

	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, mediaSegmentDurationTag) {
			if waitingForURI || ended {
				return nil, errors.New(invalidSegmentOrderMessage)
			}

			durationText, _, hasSeparator := strings.Cut(strings.TrimPrefix(line, mediaSegmentDurationTag), ",")

			duration, err := strconv.ParseFloat(durationText, 64)
			if !hasSeparator || err != nil || duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
				return nil, errors.New(invalidSegmentDurationMessage)
			}

			waitingForURI = true

			continue
		}

		if line == endListTag {
			if waitingForURI {
				return nil, errors.New(mediaPlaylistMissingURIError)
			}

			ended = true

			continue
		}

		if strings.HasPrefix(line, keyTag) {
			attributes, err := parseAttributes(strings.TrimPrefix(line, keyTag))
			if err != nil || attributes[methodAttribute] != unencryptedMethod {
				return nil, errors.New(encryptedHLSIsUnsupportedMessage)
			}

			continue
		}

		for _, unsupported := range []string{byteRangeTag, mapTag, partTag, skipTag, streamTag, iFramesOnlyTag} {
			if strings.HasPrefix(line, unsupported) {
				return nil, errors.New(unsupportedMediaPlaylistFeatureMessage)
			}
		}

		if strings.HasPrefix(line, mediaCommentPrefix) {
			continue
		}

		if !waitingForURI || ended {
			return nil, errors.New(mediaPlaylistMissingDurationError)
		}

		reference, err := url.Parse(line)
		if err != nil {
			return nil, fmt.Errorf(mediaPlaylistSegmentURLError, err)
		}

		resolved := baseURL.ResolveReference(reference)
		if (resolved.Scheme != "http" && resolved.Scheme != "https") || resolved.Host == "" || resolved.User != nil {
			return nil, errors.New(invalidSegmentURLMessage)
		}

		segmentIndex := len(segments)
		segments = append(segments, segmentMeta{index: segmentIndex, link: resolved.String(), fileName: filePrefix + mediaSegmentSeparator + strconv.Itoa(segmentIndex) + mediaSegmentExtension})
		waitingForURI = false
	}

	if waitingForURI || !ended {
		return nil, errors.New(incompleteOrLiveMediaPlaylistMessage)
	}

	if len(segments) == 0 {
		return nil, errors.New(mediaPlaylistEmptyError)
	}

	return segments, nil
}
