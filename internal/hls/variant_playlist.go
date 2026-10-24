package hls

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	bandwidthAttribute   = "BANDWIDTH"
	resolutionAttribute  = "RESOLUTION"
	frameRateAttribute   = "FRAME-RATE"
	codecsAttribute      = "CODECS"
	audioAttribute       = "AUDIO"
	videoAttribute       = "VIDEO"
	subtitlesAttribute   = "SUBTITLES"
	maximumDimension     = 65535
	variantDisplayFormat = "{\n\tbandwidth: %d,\n \tframeRate: %f,\n \tcodecs: %v,\n \tresolution: %s,\n \tlink: %s,\n}"
)

const (
	invalidVariantBandwidthMessage           = "invalid variant bandwidth"
	missingOrInvalidVariantResolutionMessage = "missing or invalid variant resolution"
	invalidVariantDimensionsMessage          = "invalid variant dimensions"
	invalidFrameRateMessage                  = "invalid frame rate"
	externalMediaTracksAreUnsupportedMessage = "external media tracks are unsupported"
	variantURIIsMissingMessage               = "variant URI is missing"
	invalidHLSAttributeMessage               = "invalid HLS attribute"
	unterminatedQuotedAttributeMessage       = "unterminated quoted attribute"
	missingAttributeSeparatorMessage         = "missing attribute separator"
	duplicateHLSAttributeMessage             = "duplicate HLS attribute"
)

const streamTag = "#EXT-X-STREAM-INF:"

type VariantPlaylist struct {
	bandwidth  int
	frameRate  float64
	codecs     []string
	resolution Resolution
	Link       string
}

func (variant VariantPlaylist) String() string {
	return fmt.Sprintf(variantDisplayFormat, variant.bandwidth, variant.frameRate, variant.codecs, variant.resolution, variant.Link)
}

func variantPlaylistFromBytes(meta, link []byte) (VariantPlaylist, error) {
	attributes, err := parseAttributes(strings.TrimPrefix(string(meta), streamTag))
	if err != nil {
		return VariantPlaylist{}, err
	}

	bandwidth, err := strconv.Atoi(attributes[bandwidthAttribute])
	if err != nil || bandwidth <= 0 {
		return VariantPlaylist{}, errors.New(invalidVariantBandwidthMessage)
	}

	dimensions := strings.Split(attributes[resolutionAttribute], "x")
	if len(dimensions) != 2 {
		return VariantPlaylist{}, errors.New(missingOrInvalidVariantResolutionMessage)
	}

	width, widthErr := strconv.Atoi(dimensions[0])

	height, heightErr := strconv.Atoi(dimensions[1])
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 || width > maximumDimension || height > maximumDimension {
		return VariantPlaylist{}, errors.New(invalidVariantDimensionsMessage)
	}

	frameRate := float64(0)
	if value, exists := attributes[frameRateAttribute]; exists {
		frameRate, err = strconv.ParseFloat(value, 64)
		if err != nil || frameRate <= 0 || math.IsNaN(frameRate) || math.IsInf(frameRate, 0) {
			return VariantPlaylist{}, errors.New(invalidFrameRateMessage)
		}
	}

	if attributes[audioAttribute] != "" || attributes[videoAttribute] != "" || attributes[subtitlesAttribute] != "" {
		return VariantPlaylist{}, errors.New(externalMediaTracksAreUnsupportedMessage)
	}

	uri := strings.TrimSpace(string(link))
	if uri == "" || strings.HasPrefix(uri, "#") {
		return VariantPlaylist{}, errors.New(variantURIIsMissingMessage)
	}

	var codecs []string

	if value := attributes[codecsAttribute]; value != "" {
		for _, codec := range strings.Split(value, ",") {
			codecs = append(codecs, strings.TrimSpace(codec))
		}
	}

	return VariantPlaylist{bandwidth: bandwidth, frameRate: frameRate, codecs: codecs, resolution: Resolution{width: width, height: height}, Link: uri}, nil
}

func parseAttributes(value string) (map[string]string, error) {
	if strings.HasSuffix(strings.TrimSpace(value), ",") {
		return nil, errors.New(invalidHLSAttributeMessage)
	}

	attributes := make(map[string]string)

	for value != "" {
		key, remainder, exists := strings.Cut(value, "=")

		key = strings.TrimSpace(key)
		if !exists || key == "" || strings.Contains(key, ",") {
			return nil, errors.New(invalidHLSAttributeMessage)
		}

		var attribute string

		if strings.HasPrefix(remainder, "\"") {
			ending := strings.Index(remainder[1:], "\"")
			if ending < 0 {
				return nil, errors.New(unterminatedQuotedAttributeMessage)
			}

			attribute = remainder[1 : ending+1]

			value = remainder[ending+2:]
			if value != "" {
				if !strings.HasPrefix(value, ",") {
					return nil, errors.New(missingAttributeSeparatorMessage)
				}

				value = value[1:]
			}
		} else {
			attribute, value, _ = strings.Cut(remainder, ",")
			attribute = strings.TrimSpace(attribute)
		}

		if _, exists := attributes[key]; exists {
			return nil, errors.New(duplicateHLSAttributeMessage)
		}

		attributes[key] = attribute
	}

	return attributes, nil
}
