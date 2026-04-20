package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	if len(os.Args) == 0 {
		fmt.Println("enter some url")
		os.Exit(1)
	}

	url := os.Args[1]
	videoID := getVideoID(url)
	if videoID == "" {
		fmt.Println("wrong wideo url")
		os.Exit(1)
	}

	// apiURL := "https://rutube.ru/api/video/" + videoID
	apiURL := "https://rutube.ru/api/play/options/" + videoID + "/?format=json"

	resp, err := http.Get(apiURL)
	if err != nil {
		fmt.Println("err API usage: ", err)
		os.Exit(1)
	}

	m3u8 := getM3U8FromResponse(resp)

	fmt.Println("link to download: ", m3u8)
}

func getVideoID(url string) string {
	j := len(url)
	i := len(url) - 1
	if url[i] == '/' {
		j -= 1
		i -= 1
	}
	for i >= 0 && url[i] != '/' {
		i -= 1
	}
	return url[i+1 : j]
}

func getM3U8FromResponse(resp *http.Response) string {
	type VideoBalancer struct {
		M3U8 string `json:"m3u8"`
	}

	type expectedBody struct {
		VideoBalancer VideoBalancer `json:"video_balancer"`
	}
	var eBody expectedBody

	readBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("cannot read api body: ", err)
		os.Exit(1)
	}

	err = json.Unmarshal(readBody, &eBody)
	if err != nil {
		fmt.Println("err api body: ", err)
		os.Exit(1)
	}

	return eBody.VideoBalancer.M3U8
}
