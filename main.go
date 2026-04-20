package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func main() {
	if len(os.Args) == 0 {
		fmt.Println("enter some url")
		os.Exit(1)
	}

	url := os.Args[1]
	if len(url) == 0 {
		panic("url should not be empty")
	}

	videoID, err := getVideoID(url)
	if err != nil {
		panic(err)
	}

	m3u8Link, err := getM3U8Link(videoID)
	if err != nil {
		panic(err)
	}

	fmt.Println("link to download: ", m3u8Link)
}

func getVideoID(url string) (string, error) {
	url = strings.TrimSuffix(url, "/")

	i := strings.LastIndex(url, "/")
	if i == -1 {
		return "", fmt.Errorf("cannot find the video id in ulr: %s", url)
	}

	return url[i+1:], nil
}

func getM3U8Link(videoID string) (string, error) {
	const template = "https://rutube.ru/api/play/options/%s/?format=json"

	url := fmt.Sprintf(template, videoID)

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("cannot get play/options: %w", err)
	}

	return getM3U8FromResponse(resp)
}

func getM3U8FromResponse(resp *http.Response) (string, error) {
	type videoBalancer struct {
		M3U8 string `json:"m3u8"`
	}

	type expectedBody struct {
		VideoBalancer videoBalancer `json:"video_balancer"`
	}

	var eBody expectedBody

	readBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("cannot read body: %w", err)
	}

	err = json.Unmarshal(readBody, &eBody)
	if err != nil {
		return "", fmt.Errorf("cannot unmarshal body: %w", err)
	}

	return eBody.VideoBalancer.M3U8, nil
}
