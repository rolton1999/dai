package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type videoInfo struct {
	title              string
	masterPlaylistLink string // m3u8
}

func (vi videoInfo) String() string {
	const template = "{\n\ttitle: %s,\n\tmasterPlaylistLink: %s\n}"
	return fmt.Sprintf(template, vi.title, vi.masterPlaylistLink)
}

func getVideoInfo(videoID string) (videoInfo, error) {
	const template = "https://rutube.ru/api/play/options/%s/?format=json"

	url := fmt.Sprintf(template, videoID)

	resp, err := http.Get(url)
	if err != nil {
		return videoInfo{}, fmt.Errorf("cannot get play/options: %w", err)
	}

	return parseVideoInfoFromApiResponse(resp)
}

func parseVideoInfoFromApiResponse(resp *http.Response) (videoInfo, error) {
	type videoBalancer struct {
		M3U8 string `json:"m3u8"`
	}

	type expectedBody struct {
		Title         string        `json:"title"`
		VideoBalancer videoBalancer `json:"video_balancer"`
	}

	var eBody expectedBody

	readBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return videoInfo{}, fmt.Errorf("cannot read body: %w", err)
	}

	err = json.Unmarshal(readBody, &eBody)
	if err != nil {
		return videoInfo{}, fmt.Errorf("cannot unmarshal body: %w", err)
	}

	return videoInfo{eBody.Title, eBody.VideoBalancer.M3U8}, nil
}
