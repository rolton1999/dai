package hls

import (
	"context"
	"dai/internal/http/client"
	"dai/internal/progress"
)

type Client interface {
	Variants(context.Context, string) ([]VariantPlaylist, error)
	Download(context.Context, progress.Logger, VariantPlaylist) (Artifact, error)
}

// The caller owns the supplied reader and network client and closes them after use.
func New(reader client.Client, network client.StreamingClient) Client {
	return &hlsClient{reader: reader, network: network}
}

type hlsClient struct {
	reader  client.Client
	network client.StreamingClient
}

func (streams *hlsClient) Variants(operationContext context.Context, link string) ([]VariantPlaylist, error) {
	provider := newPlaylistClient(streams.reader)
	return provider.Variants(operationContext, link)
}

func (streams *hlsClient) Download(operationContext context.Context, logger progress.Logger, playlist VariantPlaylist) (Artifact, error) {
	return downloadTSFiles(operationContext, logger, playlist, streams.reader, streams.network)
}
