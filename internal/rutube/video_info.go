package rutube

import (
	"context"
	"dai/internal/retry"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const metadataRequestTimeout = 2 * time.Minute

type VideoInfo struct {
	Title              string
	MasterPlaylistLink string // m3u8
}

func (vi VideoInfo) String() string {
	const template = "{\n\ttitle: %s,\n\tmasterPlaylistLink: %s\n}"
	return fmt.Sprintf(template, vi.Title, vi.MasterPlaylistLink)
}

func GetVideoInfo(videoID string) (VideoInfo, error) {
	operationContext, cancel := context.WithTimeout(context.Background(), metadataRequestTimeout)
	defer cancel()

	var result VideoInfo

	err := retry.Do(operationContext, func(attemptContext context.Context) error {
		var operationError error

		result, operationError = getVideoInfo(attemptContext, videoID)

		return operationError
	})

	return result, err
}

func getVideoInfo(operationContext context.Context, videoID string) (VideoInfo, error) {
	const template = "https://rutube.ru/api/play/options/%s/?format=json"

	url := fmt.Sprintf(template, videoID)

	request, err := http.NewRequestWithContext(operationContext, http.MethodGet, url, nil)
	if err != nil {
		return VideoInfo{}, err
	}

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return VideoInfo{}, fmt.Errorf("cannot get play/options: %w", err)
	}

	res, err := parseVideoInfoFromVideoApiResponse(resp.Body)
	_ = resp.Body.Close()

	return res, err
}

func parseVideoInfoFromVideoApiResponse(bodyReader io.Reader) (VideoInfo, error) {
	readBody, err := io.ReadAll(bodyReader)
	if err != nil {
		return VideoInfo{}, fmt.Errorf("cannot read body: %w", err)
	}

	type videoBalancer struct {
		M3U8 string `json:"m3u8"`
	}

	type expectedBody struct {
		Title         string        `json:"title"`
		VideoBalancer videoBalancer `json:"video_balancer"`
	}

	var eBody expectedBody

	err = json.Unmarshal(readBody, &eBody)
	if err != nil {
		return VideoInfo{}, fmt.Errorf("cannot unmarshal body: %w", err)
	}

	return VideoInfo{eBody.Title, eBody.VideoBalancer.M3U8}, nil
}
