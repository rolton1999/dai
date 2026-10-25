package rutube

import (
	"bytes"
	"context"
	"dai/internal/http/client"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

const (
	metadataRequestTimeout         = 2 * time.Minute
	videoRequestErrorFormat        = "cannot read video metadata: %w"
	metadataReadErrorFormat        = "cannot read body: %w"
	metadataDecodeErrorFormat      = "cannot unmarshal body: %w"
	videoMissingFieldsMessage      = "video metadata is missing title or playlist URL"
	videoInvalidPlaylistURLMessage = "video metadata contains an invalid HTTP(S) playlist URL"
	metadataHTTPScheme             = "http"
	metadataHTTPSScheme            = "https"
)

type VideoInfo struct {
	Title              string
	MasterPlaylistLink string // m3u8
}

func (vi VideoInfo) String() string {
	const template = "{\n\ttitle: %s,\n\tmasterPlaylistLink: %s\n}"
	return fmt.Sprintf(template, vi.Title, vi.MasterPlaylistLink)
}

func GetVideoInfo(videoID string) (VideoInfo, error) {
	httpClient := client.NewStreamingClient()
	defer httpClient.CloseIdleConnections()

	reader := client.New(httpClient)
	provider := newMetadataProvider(reader)

	return provider.Video(context.Background(), videoID)
}

func getVideoInfoWithReader(
	operationContext context.Context,
	reader client.Client,
	videoID string,
) (VideoInfo, error) {
	const videoAPI = "https://rutube.ru/api/play/options/%s/?format=json"

	link := fmt.Sprintf(videoAPI, url.PathEscape(videoID))

	response, err := reader.Read(operationContext, link)
	if err != nil {
		return VideoInfo{}, fmt.Errorf(videoRequestErrorFormat, err)
	}

	bodyReader := bytes.NewReader(response.Body)

	return parseVideoInfoFromVideoApiResponse(bodyReader)
}

func parseVideoInfoFromVideoApiResponse(bodyReader io.Reader) (VideoInfo, error) {
	readBody, err := io.ReadAll(bodyReader)
	if err != nil {
		return VideoInfo{}, fmt.Errorf(metadataReadErrorFormat, err)
	}

	type videoBalancer struct {
		M3U8 string `json:"m3u8"`
	}

	type expectedBody struct {
		Title         string        `json:"title"`
		VideoBalancer videoBalancer `json:"video_balancer"`
	}

	var metadata expectedBody

	err = json.Unmarshal(readBody, &metadata)
	if err != nil {
		return VideoInfo{}, fmt.Errorf(metadataDecodeErrorFormat, err)
	}

	if strings.TrimSpace(metadata.Title) == "" || strings.TrimSpace(metadata.VideoBalancer.M3U8) == "" {
		return VideoInfo{}, errors.New(videoMissingFieldsMessage)
	}

	playlistURL, err := url.Parse(metadata.VideoBalancer.M3U8)
	if err != nil || (playlistURL.Scheme != metadataHTTPScheme && playlistURL.Scheme != metadataHTTPSScheme) || playlistURL.Hostname() == "" || playlistURL.User != nil {
		return VideoInfo{}, errors.New(videoInvalidPlaylistURLMessage)
	}

	return VideoInfo{
		Title:              metadata.Title,
		MasterPlaylistLink: metadata.VideoBalancer.M3U8,
	}, nil
}
