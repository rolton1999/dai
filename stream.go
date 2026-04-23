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
