package hls

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

func GetVariantPlaylists(masterPlaylistLink string) ([]VariantPlaylist, error) {
	const extM3UPrefix = "#EXTM3U"

	resp, err := http.Get(masterPlaylistLink)
	if err != nil {
		return nil, fmt.Errorf("cannot get m3u8: %w", err)
	}

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
