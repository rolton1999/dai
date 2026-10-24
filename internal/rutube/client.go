package rutube

import (
	"context"
	"dai/internal/httpclient"
	"dai/internal/retry"
)

type Client interface {
	Video(context.Context, string) (VideoInfo, error)
	Playlist(context.Context, string) (PlaylistInfo, error)
}

func New(reader httpclient.Client) Client {
	if reader == nil {
		reader = httpclient.New(nil)
	}

	return &metadataClient{reader: reader}
}

type metadataClient struct {
	reader httpclient.Client
}

func (client *metadataClient) Video(operationContext context.Context, videoID string) (VideoInfo, error) {
	operationContext, cancel := context.WithTimeout(operationContext, metadataRequestTimeout)
	defer cancel()

	var result VideoInfo

	err := retry.Do(operationContext, func(attemptContext context.Context) error {
		var err error

		result, err = getVideoInfoWithReader(attemptContext, client.reader, videoID)

		return err
	})

	return result, err
}

func (client *metadataClient) Playlist(operationContext context.Context, playlistID string) (PlaylistInfo, error) {
	operationContext, cancel := context.WithTimeout(operationContext, metadataRequestTimeout)
	defer cancel()

	var result PlaylistInfo

	err := retry.Do(operationContext, func(attemptContext context.Context) error {
		var err error

		result, err = getPlaylistInfoWithReader(attemptContext, client.reader, playlistID)

		return err
	})

	return result, err
}
