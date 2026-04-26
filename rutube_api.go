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

	res, err := parseVideoInfoFromVideoApiResponse(resp.Body)
	resp.Body.Close()

	return res, err
}

func parseVideoInfoFromVideoApiResponse(bodyReader io.Reader) (videoInfo, error) {
	readBody, err := io.ReadAll(bodyReader)
	if err != nil {
		return videoInfo{}, fmt.Errorf("cannot read body: %w", err)
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
		return videoInfo{}, fmt.Errorf("cannot unmarshal body: %w", err)
	}

	return videoInfo{eBody.Title, eBody.VideoBalancer.M3U8}, nil
}

type playlistInfo struct {
	title    string
	videoIDs []string
}

func (pi playlistInfo) String() string {
	const template = "{\n\ttitle: %s,\n\tvideoIDs: %v\n}"
	return fmt.Sprintf(template, pi.title, pi.videoIDs)
}

func getPlaylistInfo(playlistID string) (playlistInfo, error) {
	const playlistApiURL = "https://rutube.ru/api/playlist/custom/%s/?format=json"
	resp, err := http.Get(fmt.Sprintf(playlistApiURL, playlistID))
	if err != nil {
		return playlistInfo{}, fmt.Errorf("cannot get playlist info: %w", err)
	}
	defer resp.Body.Close()

	// can get title from here
	// const template = "https://rutube.ru/api/playlist/custom/%s/?format=json"
	title, err := parseTitleFromPlaylistApiResponse(resp.Body)
	if err != nil {
		return playlistInfo{}, fmt.Errorf("cannot parse playlist title from api reponse: %w", err)
	}

	const playlistApiDetailsURL = "https://rutube.ru/api/playlist/custom/%s/videos/?format=json"
	resp, err = http.Get(fmt.Sprintf(playlistApiDetailsURL, playlistID))
	if err != nil {
		return playlistInfo{}, fmt.Errorf("cannot get playlist details: %w", err)
	}
	defer resp.Body.Close()

	ids, err := parseVideoIDsFromPlaylistApiResponse(resp.Body)

	if err != nil {
		return playlistInfo{}, fmt.Errorf("cannot parse video IDs from api reponse: %w", err)
	}

	return playlistInfo{title, ids}, nil
}

func parseTitleFromPlaylistApiResponse(bodyReader io.Reader) (string, error) {
	body, err := io.ReadAll(bodyReader)

	type expectedBody struct {
		Title string `json:"title"`
	}

	var eBody expectedBody

	err = json.Unmarshal(body, &eBody)
	if err != nil {
		return "", fmt.Errorf("cannot unmarshal body: %w", err)
	}

	return eBody.Title, nil
}

func parseVideoIDsFromPlaylistApiResponse(bodyReader io.Reader) ([]string, error) {
	body, err := io.ReadAll(bodyReader)

	type result struct {
		ID string `json:"id"`
	}
	type expectedBody struct {
		Results []result `json:"results"`
	}
	var eBody expectedBody

	err = json.Unmarshal(body, &eBody)
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshal body: %w", err)
	}

	ids := make([]string, 0, len(eBody.Results))
	for _, r := range eBody.Results {
		ids = append(ids, r.ID)
	}

	return ids, nil
}
