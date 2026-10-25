package hls

import (
	"context"
	"dai/internal/http/client"
	"dai/internal/progress"
	"dai/internal/retry"
	"errors"
)

const missingStreamDependenciesMessage = "HLS reader and streaming HTTP client are required"

type Client interface {
	Variants(context.Context, string) ([]VariantPlaylist, error)
	Download(context.Context, progress.Logger, VariantPlaylist) (Artifact, error)
}

// The caller owns the supplied reader and network client and closes them after use.
func New(reader client.Client, network client.StreamingClient) (Client, error) {
	if reader == nil || network == nil {
		return nil, errors.New(missingStreamDependenciesMessage)
	}

	return &hlsClient{reader: reader, network: network}, nil
}

type hlsClient struct {
	reader  client.Client
	network client.StreamingClient
}

func (streams *hlsClient) Variants(operationContext context.Context, link string) ([]VariantPlaylist, error) {
	operationContext, cancel := context.WithTimeout(operationContext, playlistRequestTimeout)
	defer cancel()

	var result []VariantPlaylist

	err := retry.Do(operationContext, func(attemptContext context.Context) error {
		var err error

		result, err = getVariantPlaylistsWithReader(attemptContext, streams.reader, link)

		return err
	})

	return result, err
}

func (streams *hlsClient) Download(operationContext context.Context, logger progress.Logger, playlist VariantPlaylist) (Artifact, error) {
	return downloadTSFiles(operationContext, logger, playlist, streams.reader, streams.network)
}
