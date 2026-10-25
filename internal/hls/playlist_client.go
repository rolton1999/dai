package hls

import (
	"context"
	"dai/internal/http/client"
	"dai/internal/retry"
)

type variantProvider interface {
	Variants(context.Context, string) ([]VariantPlaylist, error)
}

func newPlaylistClient(reader client.Client) variantProvider {
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
