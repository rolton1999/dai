package hls

import (
	"context"
	"dai/internal/retry"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const playlistRequestTimeout = 2 * time.Minute

const (
	mediaTag      = "#EXT-X-MEDIA:"
	sessionKeyTag = "#EXT-X-SESSION-KEY:"
)

const (
	invalidMasterPlaylistHeaderMessage         = "invalid master playlist header"
	unsupportedMasterPlaylistFeatureMessage    = "unsupported master playlist feature"
	unexpectedMasterPlaylistURIMessage         = "unexpected master playlist URI"
	invalidVariantURLMessage                   = "invalid variant URL"
	masterPlaylistHasNoCompleteVariantsMessage = "master playlist has no complete variants"
	noVariantsMessage                          = "no variants"
	invalidVariantResolutionMessage            = "invalid variant resolution"
)

const (
	masterHeader                        = "#EXTM3U"
	iFramesOnlyTag                      = "#EXT-X-I-FRAMES-ONLY"
	playlistResponseURLIsMissingMessage = "playlist response URL is missing"
	masterRequestErrorFormat            = "cannot get m3u8: %w"
	masterReadErrorFormat               = "cannot read body of m3u8: %w"
	masterHTTPStatusErrorFormat         = "cannot get master playlist: HTTP %d"
)

func GetVariantPlaylists(masterPlaylistLink string) ([]VariantPlaylist, error) {
	operationContext, cancel := context.WithTimeout(context.Background(), playlistRequestTimeout)
	defer cancel()

	var result []VariantPlaylist

	err := retry.Do(operationContext, func(attemptContext context.Context) error {
		var operationError error

		result, operationError = getVariantPlaylists(attemptContext, masterPlaylistLink)

		return operationError
	})

	return result, err
}

func getVariantPlaylists(operationContext context.Context, masterPlaylistLink string) ([]VariantPlaylist, error) {
	request, err := http.NewRequestWithContext(operationContext, http.MethodGet, masterPlaylistLink, nil)
	if err != nil {
		return nil, err
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf(masterRequestErrorFormat, err)
	}

	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(masterHTTPStatusErrorFormat, response.StatusCode)
	}

	extM3UBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf(masterReadErrorFormat, err)
	}

	baseURL := request.URL
	if response.Request != nil && response.Request.URL != nil {
		baseURL = response.Request.URL
	}

	return parseMasterPlaylist(string(extM3UBytes), baseURL)
}

func parseMasterPlaylist(body string, baseURL *url.URL) ([]VariantPlaylist, error) {
	if baseURL == nil {
		return nil, errors.New(playlistResponseURLIsMissingMessage)
	}

	lines := strings.Split(strings.TrimSpace(body), "\n")
	if strings.TrimSpace(lines[0]) != masterHeader {
		return nil, errors.New(invalidMasterPlaylistHeaderMessage)
	}

	var result []VariantPlaylist

	pending := ""

	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, streamTag) {
			if pending != "" {
				return nil, errors.New(variantURIIsMissingMessage)
			}

			pending = line

			continue
		}

		if strings.HasPrefix(line, mediaTag) || strings.HasPrefix(line, sessionKeyTag) || strings.HasPrefix(line, iFramesOnlyTag) {
			return nil, errors.New(unsupportedMasterPlaylistFeatureMessage)
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		if pending == "" {
			return nil, errors.New(unexpectedMasterPlaylistURIMessage)
		}

		variant, err := variantPlaylistFromBytes([]byte(pending), []byte(line))
		if err != nil {
			return nil, err
		}

		reference, err := url.Parse(variant.Link)
		if err != nil {
			return nil, err
		}

		resolved := baseURL.ResolveReference(reference)
		if (resolved.Scheme != "http" && resolved.Scheme != "https") || resolved.Host == "" || resolved.User != nil {
			return nil, errors.New(invalidVariantURLMessage)
		}

		variant.Link = resolved.String()
		result = append(result, variant)
		pending = ""
	}

	if pending != "" || len(result) == 0 {
		return nil, errors.New(masterPlaylistHasNoCompleteVariantsMessage)
	}

	return result, nil
}

func GetPlaylistWithBestResolution(variants []VariantPlaylist) (VariantPlaylist, error) {
	if len(variants) == 0 {
		return VariantPlaylist{}, errors.New(noVariantsMessage)
	}

	best := variants[0]
	for _, variant := range variants {
		if variant.resolution.width <= 0 || variant.resolution.height <= 0 || variant.resolution.width > maximumDimension || variant.resolution.height > maximumDimension {
			return VariantPlaylist{}, errors.New(invalidVariantResolutionMessage)
		}

		if best.resolution.Less(variant.resolution) || (best.resolution == variant.resolution && best.bandwidth < variant.bandwidth) {
			best = variant
		}
	}

	return best, nil
}
