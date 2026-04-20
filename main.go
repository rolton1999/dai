package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
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

	fmt.Println("streams: ")
	for _, s := range streams {
		fmt.Println(s)
	}
}

type stream struct {
	bandwidth  int
	frameRate  float64
	codecs     []string
	resolution string
	link       string
}

func (s stream) String() string {
	return fmt.Sprintf(
		"{\n\tbandwidth: %d,\n \tframeRate: %f,\n \tcodecs: %v,\n \tresolution: %s,\n \tlink: %s,\n}",
		s.bandwidth,
		s.frameRate,
		s.codecs,
		s.resolution,
		s.link,
	)
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
	// lines := strings.Split(string(extM3U), "\n")
	// fmt.Println("lines: ", lines)

	for i := 0; i < len(lines); i += 2 {
		// fmt.Println("line: ", string(lines[i]))
		// fmt.Println("line: ", string(lines[i+1]))

		var s stream

		firstComma := bytes.Index(lines[i], []byte{','})
		bytes := lines[i][firstComma+1:]

		field := 0
		for i := 0; i < len(bytes); {
			eqI := i
			for ; bytes[eqI] != '='; eqI++ {
			}

			commaI := eqI + 1
			var toFind byte = ','
			inc := 1
			if commaI < len(bytes) && bytes[commaI] == '"' {
				eqI += 1
				commaI += 1
				toFind = '"'
				inc += 1
			}

			for ; commaI != len(bytes) && bytes[commaI] != toFind; commaI++ {
			}

			got := string(bytes[eqI+1 : commaI])

			switch field {
			case 0:
				bandwidth, e := strconv.Atoi(got)
				if e != nil {
					return nil, fmt.Errorf("cannot parse bandwidth")
				}
				s.bandwidth = bandwidth
			case 1:
				frameRate, e := strconv.ParseFloat(got, 63)
				if e != nil {
					return nil, fmt.Errorf("cannot parse frameRate")
				}
				s.frameRate = frameRate
			case 2:
				codecs := strings.Split(got, ",")
				for _, c := range codecs {
					s.codecs = append(s.codecs, strings.TrimSpace(c))
				}
			case 3:
				s.resolution = got
			default:
				panic("unknown stream field" + got + s.String())
			}

			i = commaI + inc
			field += 1
		}

		s.link = string(lines[i+1])

		streams = append(streams, s)
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
