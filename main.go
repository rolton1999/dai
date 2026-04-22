package main

import (
	"bytes"
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

	streams, err := getStreams(m3u8Link)
	if err != nil {
		panic(err)
	}

	// fmt.Println("streams: ")
	// for _, s := range streams {
	// 	fmt.Println(s)
	// }
	getOnStreamLink(streams[0].link)
}

func getOnStreamLink(link string) {
	resp, err := http.Get(link)
	if err != nil {
		fmt.Println("cannot get stream link: ", err)
		return
	}

	body, err := io.ReadAll(resp.Body)
	fmt.Println("body: ", string(body))
}

func getStreams(m3u8Link string) ([]stream, error) {
	const extM3UPrefix = "#EXTM3U"

	resp, err := http.Get(m3u8Link)
	if err != nil {
		return nil, fmt.Errorf("cannot get m3u8: %w", err)
	}

	extM3UBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cannot read body of m3u8: %w", err)
	}

	if !bytes.HasPrefix(extM3UBytes, []byte(extM3UPrefix)) {
		return nil, fmt.Errorf("expected extM3U")
	}

	extM3UBytes = extM3UBytes[len(extM3UPrefix):]
	extM3UBytes = bytes.TrimSpace(extM3UBytes)

	var streams []stream

	lines := bytes.Split(extM3UBytes, []byte{'\n'})

	for i := 0; i < len(lines); i += 2 {
		stream, err := streamFromBytes(lines[i], lines[i+1])
		if err != nil {
			return nil, fmt.Errorf("cannot parse stream: %w", err)
		}
		streams = append(streams, stream)
	}

	return streams, nil
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
