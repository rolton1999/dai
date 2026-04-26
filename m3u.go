package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

func getVariantPlaylists(masterPlaylistLink string) ([]variantPlaylist, error) {
	const extM3UPrefix = "#EXTM3U"

	resp, err := http.Get(masterPlaylistLink)
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

type resolution struct {
	w, h int
}

func (r resolution) Less(other resolution) bool {
	if r.w == other.w {
		return r.h < other.h
	}
	return r.w < other.w
}

func (r resolution) String() string {
	const template = "%dx%d"
	return fmt.Sprintf(template, r.w, r.h)
}

type variantPlaylist struct {
	bandwidth  int
	frameRate  float64
	codecs     []string
	resolution resolution
	link       string
}

func (s variantPlaylist) String() string {
	return fmt.Sprintf(
		"{\n\tbandwidth: %d,\n \tframeRate: %f,\n \tcodecs: %v,\n \tresolution: %s,\n \tlink: %s,\n}",
		s.bandwidth,
		s.frameRate,
		s.codecs,
		s.resolution,
		s.link,
	)
}

// meta: #EXT-X-STREAM-INF:PROGRAM-ID=1,BANDWIDTH=156000,FRAME-RATE=29.97,CODECS="avc1.4d400c, mp4a.40.2",RESOLUTION=256x144
func variantPlaylistFromBytes(meta []byte, link []byte) (variantPlaylist, error) {
	var playList variantPlaylist

	// skip #EXT-X-STREAM-INF:PROGRAM-ID=1
	firstComma := bytes.Index(meta, []byte{','})
	bytes := meta[firstComma+1:]

	field := 0
	for i := 0; i < len(bytes); {
		equalSignIndex := i
		for ; bytes[equalSignIndex] != '='; equalSignIndex++ {
		}

		commaI := equalSignIndex + 1
		var toFind byte = ','
		inc := 1
		// if we have quoted "codec1, codec2" instead of direct unquoted value
		if commaI < len(bytes) && bytes[commaI] == '"' {
			equalSignIndex += 1
			commaI += 1
			toFind = '"'
			inc += 1
		}

		for ; commaI != len(bytes) && bytes[commaI] != toFind; commaI++ {
		}

		got := string(bytes[equalSignIndex+1 : commaI])

		switch field {
		case 0:
			bandwidth, e := strconv.Atoi(got)
			if e != nil {
				return playList, fmt.Errorf("cannot parse bandwidth: %s", got)
			}
			playList.bandwidth = bandwidth
		case 1:
			frameRate, e := strconv.ParseFloat(got, 63)
			if e != nil {
				return playList, fmt.Errorf("cannot parse frameRate: %s", got)
			}
			playList.frameRate = frameRate
		case 2:
			codecs := strings.Split(got, ",")
			for _, c := range codecs {
				playList.codecs = append(playList.codecs, strings.TrimSpace(c))
			}
		case 3:
			split := strings.Split(got, "x")
			if len(split) != 2 {
				return playList, fmt.Errorf("cannot parse resolution: %s", got)
			}
			x, _ := strconv.Atoi(split[0])
			y, _ := strconv.Atoi(split[1])
			playList.resolution = resolution{x, y}
		default:
			return variantPlaylist{}, fmt.Errorf("cannot parse EXTM3U, unknown field: %s", got)
		}

		i = commaI + inc
		field += 1
	}

	playList.link = string(link)

	return playList, nil
}

func getPlaylistWithBestResolution(d []variantPlaylist) variantPlaylist {
	i := 0
	for j := range d {
		if d[i].resolution.Less(d[j].resolution) {
			i = j
		}
	}
	return d[i]
}

func (s *variantPlaylist) getPlayListLinkPrefix() string {
	// s.link:
	// https://river-4-439.rtbcdn.ru/hls-vod/jTj56pP38b8KlsNyOPuxkw/1777581612/3034/0x5000c500e9cf2c6e/435b723b2f89465bbfd0db7ccbf33bfd.mp4.m3u8?i=256x144_156
	// need prefix:
	// https://river-4-439.rtbcdn.ru/hls-vod/h33Jy-1bQMVb3cz8CzX0UQ/1777581666/3034/0x5000c500e9cf2c6e

	li := strings.LastIndex(s.link, "/")
	return s.link[:li]
}

func downloadTSFilesToDir(dirName string, namePrefix string, playlist variantPlaylist) ([]string, error) {
	resp, err := http.Get(playlist.link)
	if err != nil {
		return nil, fmt.Errorf("cannot get variantPlaylist link: %w", err)
	}

	const readWriteMode = 0777
	err = os.Mkdir(dirName, readWriteMode)
	if err != nil {
		return nil, fmt.Errorf("cannot create temp dir: %w", err)
	}

	// #EXTM3U
	// #EXT-X-TARGETDURATION:7
	// #EXT-X-ALLOW-CACHE:YES
	// #EXT-X-PLAYLIST-TYPE:VOD
	// #EXT-X-VERSION:3
	// #EXT-X-MEDIA-SEQUENCE:1
	// #EXTINF:3.937,
	// e742a021efe5453586f12d4d08a836fe.mp4/segment-1-v1-a1.ts <- 7th line
	// #EXTINF:0.133,
	// e742a021efe5453586f12d4d08a836fe.mp4/segment-2-v1-a1.ts
	// #EXTINF:6.607,
	// e742a021efe5453586f12d4d08a836fe.mp4/segment-3-v1-a1.ts
	// #EXTINF:5.339,
	// ....
	// e742a021efe5453586f12d4d08a836fe.mp4/segment-1157-v1-a1.ts
	// #EXT-X-ENDLIST

	body, err := io.ReadAll(resp.Body)
	// fmt.Println("body: ", string(body))
	// os.Exit(0)
	resp.Body.Close()

	prefix := playlist.getPlayListLinkPrefix()

	split := bytes.Split(body, []byte{'\n'})

	namePrefix = strings.ReplaceAll(namePrefix, " ", "_")
	namePrefix = strings.ReplaceAll(namePrefix, "/", "")

	howMany := (len(split) - 6 + 1) / 2
	listOfFiles := make([]string, 0, howMany)
	for segmentIndex, i := 0, 7; i < len(split); segmentIndex, i = segmentIndex+1, i+2 {
		segment := string(split[i])
		link := prefix + "/" + segment
		resp, err = http.Get(link)
		if err != nil {
			return nil, fmt.Errorf("cannot get first segment link: %w", err)
		}

		fileName := dirName + "/" + namePrefix + "_" + strconv.Itoa(segmentIndex) + ".ts"
		file, err := os.Create(fileName)
		if err != nil {
			return nil, fmt.Errorf("cannot create file for ts-data: %w", err)
		}
		defer func() { _ = file.Close() }()

		io.Copy(file, resp.Body)

		listOfFiles = append(listOfFiles, fileName)

		fmt.Printf("\rLoaded: %d%%", 100*(segmentIndex+1)/howMany)
	}
	fmt.Println()

	return listOfFiles, nil
}
