package hls

import (
	"bytes"
	"context"
	"dai/internal/progress"
	"fmt"
	"io"
	"net/http"
	"time"
)

const playlistRequestTimeout = 2 * time.Minute

func GetVariantPlaylists(masterPlaylistLink string) ([]VariantPlaylist, error) {
	operationContext, cancel := context.WithTimeout(context.Background(), playlistRequestTimeout)
	defer cancel()

	var result []VariantPlaylist

	err := progress.Retry(operationContext, func(attemptContext context.Context) error {
		var operationError error

		result, operationError = getVariantPlaylists(attemptContext, masterPlaylistLink)

		return operationError
	})

	return result, err
}

func getVariantPlaylists(operationContext context.Context, masterPlaylistLink string) ([]VariantPlaylist, error) {
	const extM3UPrefix = "#EXTM3U"

	request, err := http.NewRequestWithContext(operationContext, http.MethodGet, masterPlaylistLink, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("cannot get m3u8: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	extM3UBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cannot read body of m3u8: %w", err)
	}

	if !bytes.HasPrefix(extM3UBytes, []byte(extM3UPrefix)) {
		return nil, fmt.Errorf("expected extM3U")
	}

	extM3UBytes = extM3UBytes[len(extM3UPrefix):]
	extM3UBytes = bytes.TrimSpace(extM3UBytes)

	var variantPlaylists []VariantPlaylist

	lines := bytes.Split(extM3UBytes, []byte{'\n'})

	for i := 0; i < len(lines); i += 2 {
		variantPlaylist, err := variantPlaylistFromBytes(lines[i], lines[i+1])
		if err != nil {
			return nil, fmt.Errorf("cannot parse variantPlaylist: %w", err)
		}

		variantPlaylists = append(variantPlaylists, variantPlaylist)
	}

	return variantPlaylists, nil
}

func GetPlaylistWithBestResolution(d []VariantPlaylist) VariantPlaylist {
	i := 0
	for j := range d {
		if d[i].resolution.Less(d[j].resolution) {
			i = j
		}
	}

	return d[i]
}
