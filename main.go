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

// how to load m3u8
// https://gist.github.com/devinschumacher/aad315b92661fdaad316b949c33c94c7

func main() {
	if len(os.Args) == 0 {
		panic("enter some url")
	}

	url := os.Args[1]
	if len(url) == 0 {
		panic("url should not be empty")
	}

	videoID, err := getVideoID(url)
	if err != nil {
		panic(err)
	}

	masterLink, err := getM3U8Link(videoID)
	if err != nil {
		panic(err)
	}

	variantPlaylists, err := getVariantPlaylists(masterLink)
	if err != nil {
		panic(err)
	}

	downloadTSFiles(variantPlaylists[0])
}

func downloadTSFiles(s variantPlaylist) {
	resp, err := http.Get(s.link)
	if err != nil {
		fmt.Println("cannot get variantPlaylist link: ", err)
		return
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()

	prefix := s.getPlayListLinkPrefix()

	sp := bytes.Split(body, []byte{'\n'})

	firstSegment := string(sp[7])

	link := prefix + "/" + firstSegment
	fmt.Println("link: ", link)
	resp, err = http.Get(link)
	if err != nil {
		fmt.Println("cannot get first segment link: ", err)
		return
	}

	file, err := os.Create("inputt.ts")
	if err != nil {
		panic(err)
	}

	defer file.Close()

	io.Copy(file, resp.Body)
}

func getVariantPlaylists(m3u8Link string) ([]variantPlaylist, error) {
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

	var variantPlaylists []variantPlaylist

	lines := bytes.Split(extM3UBytes, []byte{'\n'})

	for i := 0; i < len(lines); i += 2 {
		variantPlaylist, err := variantPlaylistFromBytes(lines[i], lines[i+1])
		if err != nil {
			return nil, fmt.Errorf("cannot parse variantPlaylist: %w", err)
		}
		variantPlaylists = append(variantPlaylists, variantPlaylist)
	}

	return variantPlaylists, nil
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
