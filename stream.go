package main

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

type variantPlaylist struct {
	bandwidth  int
	frameRate  float64
	codecs     []string
	resolution string
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
	var s variantPlaylist
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
				return s, fmt.Errorf("cannot parse bandwidth")
			}
			s.bandwidth = bandwidth
		case 1:
			frameRate, e := strconv.ParseFloat(got, 63)
			if e != nil {
				return s, fmt.Errorf("cannot parse frameRate")
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

	s.link = string(link)

	return s, nil
}

func (s *variantPlaylist) getPlayListLinkPrefix() string {
	// s.link:
	// https://river-4-439.rtbcdn.ru/hls-vod/jTj56pP38b8KlsNyOPuxkw/1777581612/3034/0x5000c500e9cf2c6e/435b723b2f89465bbfd0db7ccbf33bfd.mp4.m3u8?i=256x144_156
	// need prefix:
	// https://river-4-439.rtbcdn.ru/hls-vod/h33Jy-1bQMVb3cz8CzX0UQ/1777581666/3034/0x5000c500e9cf2c6e

	li := strings.LastIndex(s.link, "/")
	return s.link[:li]
}
