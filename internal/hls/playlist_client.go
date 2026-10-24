package hls

import (
	"context"
	"dai/internal/http/client"
	"dai/internal/retry"
)

type PlaylistClient interface {
	Variants(context.Context, string) ([]VariantPlaylist, error)
}

func NewPlaylistClient(reader client.Client) PlaylistClient {
	if reader == nil {
		reader = client.New(nil)
	}

	return &playlistClient{reader: reader}
}

type playlistClient struct {
	reader client.Client
}

func (client *playlistClient) Variants(operationContext context.Context, link string) ([]VariantPlaylist, error) {
	operationContext, cancel := context.WithTimeout(operationContext, playlistRequestTimeout)
	defer cancel()

	var result []VariantPlaylist

	err := retry.Do(operationContext, func(attemptContext context.Context) error {
		var err error

		result, err = getVariantPlaylistsWithReader(attemptContext, client.reader, link)

		return err
	})

	return result, err
}
