package m3u

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type Resolution struct {
	w, h int
}

func (r Resolution) Less(other Resolution) bool {
	if r.w == other.w {
		return r.h < other.h
	}
	return r.w < other.w
}

func (r Resolution) String() string {
	const template = "%dx%d"
	return fmt.Sprintf(template, r.w, r.h)
}

type VariantPlaylist struct {
	bandwidth  int
	frameRate  float64
	codecs     []string
	resolution Resolution
	Link       string
}

func (s VariantPlaylist) String() string {
	return fmt.Sprintf(
		"{\n\tbandwidth: %d,\n \tframeRate: %f,\n \tcodecs: %v,\n \tresolution: %s,\n \tlink: %s,\n}",
		s.bandwidth,
		s.frameRate,
		s.codecs,
		s.resolution,
		s.Link,
	)
}

func (s VariantPlaylist) getPlayListLinkPrefix() string {
	// s.link:
	// https://river-4-439.rtbcdn.ru/hls-vod/jTj56pP38b8KlsNyOPuxkw/1777581612/3034/0x5000c500e9cf2c6e/435b723b2f89465bbfd0db7ccbf33bfd.mp4.m3u8?i=256x144_156
	// need prefix:
	// https://river-4-439.rtbcdn.ru/hls-vod/h33Jy-1bQMVb3cz8CzX0UQ/1777581666/3034/0x5000c500e9cf2c6e

	li := strings.LastIndex(s.Link, "/")
	return s.Link[:li]
}

func GetVariantPlaylists(masterPlaylistLink string) ([]VariantPlaylist, error) {
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

	var variantPlaylists []VariantPlaylist

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

// meta: #EXT-X-STREAM-INF:PROGRAM-ID=1,BANDWIDTH=156000,FRAME-RATE=29.97,CODECS="avc1.4d400c, mp4a.40.2",RESOLUTION=256x144
func variantPlaylistFromBytes(meta []byte, link []byte) (VariantPlaylist, error) {
	var playList VariantPlaylist

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
			playList.resolution = Resolution{x, y}
		default:
			return VariantPlaylist{}, fmt.Errorf("cannot parse EXTM3U, unknown field: %s", got)
		}

		i = commaI + inc
		field += 1
	}

	playList.Link = string(link)

	return playList, nil
}

func GetPlaylistWithBestResolution(d []VariantPlaylist) VariantPlaylist {
	i := 0
	for j := range d {
		if d[i].resolution.Less(d[j].resolution) {
			i = j
		}
	}
	return d[i]
}
