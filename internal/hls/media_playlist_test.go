package hls

import (
	"net/url"
	"strings"
	"testing"
)

const (
	mediaFixtureDurationSeconds     = 1
	mediaFixtureURL                 = "https://cdn.example.test/path/media.m3u8"
	mediaFixtureBody                = "#EXTM3U\n#EXTINF:1,\nsegment.ts\n#EXT-X-ENDLIST\n"
	mediaFixturePrefix              = "fixture"
	mediaFixtureSegment             = "segment.ts"
	mediaFixtureResolvedSegment     = "https://cdn.example.test/path/segment.ts"
	mediaFixtureErrorFormat         = "segments=%v error=%v"
	mediaFixtureAcceptedErrorFormat = "accepted invalid playlist %q"
	mediaFixtureEndMarker           = "#EXT-X-ENDLIST"
	mediaFixtureDuration            = "#EXTINF:1,"
	mediaFixtureStart               = "#EXTM3U\n"
	mediaUnencryptedTag             = "#EXT-X-KEY:METHOD=NONE\n"
)

func TestMediaPlaylistAcceptsCompleteUnencryptedInput(test *testing.T) {
	baseURL, err := url.Parse(mediaFixtureURL)
	if err != nil {
		test.Fatal(err)
	}

	fixtures := []string{
		mediaFixtureBody,
		strings.TrimSpace(mediaFixtureBody),
		strings.ReplaceAll(mediaFixtureBody, "\n", "\r\n"),
		strings.Replace(mediaFixtureBody, mediaFixtureStart, mediaFixtureStart+mediaUnencryptedTag, 1),
	}
	for _, body := range fixtures {
		segments, err := parseSegmentMetas(body, baseURL, mediaFixturePrefix)
		if err != nil || len(segments) != 1 || segments[0].link != mediaFixtureResolvedSegment || segments[0].duration != mediaFixtureDurationSeconds {
			test.Fatalf(mediaFixtureErrorFormat, segments, err)
		}
	}
}

func TestMediaPlaylistRejectsUnsupportedFeatures(test *testing.T) {
	baseURL, err := url.Parse(mediaFixtureURL)
	if err != nil {
		test.Fatal(err)
	}

	unsupportedTags := []string{
		"#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"",
		"#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"key.bin\"",
		"#EXT-X-KEY:METHOD=\"unterminated",
		"#EXT-X-BYTERANGE:100@0",
		"#EXT-X-MAP:URI=\"init.mp4\"",
		"#EXT-X-PART:DURATION=1,URI=\"part.ts\"",
		"#EXT-X-SKIP:SKIPPED-SEGMENTS=1",
		"#EXT-X-I-FRAMES-ONLY",
		"#EXT-X-STREAM-INF:BANDWIDTH=1000,RESOLUTION=1280x720",
	}
	for _, tag := range unsupportedTags {
		body := strings.Replace(mediaFixtureBody, mediaFixtureStart, mediaFixtureStart+tag+"\n", 1)
		if _, err := parseSegmentMetas(body, baseURL, mediaFixturePrefix); err == nil {
			test.Errorf(mediaFixtureAcceptedErrorFormat, body)
		}
	}
}

func TestMediaPlaylistRejectsIncompleteAndMalformedInput(test *testing.T) {
	baseURL, err := url.Parse(mediaFixtureURL)
	if err != nil {
		test.Fatal(err)
	}

	fixtures := []string{
		"", "#EXTM3U\n#EXT-X-ENDLIST\n",
		strings.ReplaceAll(mediaFixtureBody, mediaFixtureEndMarker, ""),
		strings.ReplaceAll(mediaFixtureBody, mediaFixtureSegment, ""),
		strings.ReplaceAll(mediaFixtureBody, mediaFixtureDuration, "#EXTINF:NaN,"),
		strings.ReplaceAll(mediaFixtureBody, mediaFixtureDuration, "#EXTINF:+Inf,"),
		strings.ReplaceAll(mediaFixtureBody, mediaFixtureDuration, "#EXTINF:0,"),
		strings.ReplaceAll(mediaFixtureBody, mediaFixtureDuration, "#EXTINF:-1,"),
		strings.ReplaceAll(mediaFixtureBody, mediaFixtureDuration, "#EXTINF:1"),
		strings.ReplaceAll(mediaFixtureBody, mediaFixtureSegment, "file:///tmp/segment.ts"),
		strings.ReplaceAll(mediaFixtureBody, mediaFixtureSegment, "https://user@cdn.example.test/segment.ts"),
		strings.ReplaceAll(mediaFixtureBody, mediaFixtureSegment, "%invalid"),
		strings.ReplaceAll(mediaFixtureBody, mediaFixtureEndMarker, mediaFixtureEndMarker+"\n"+mediaFixtureDuration+"\n"+mediaFixtureSegment),
	}
	for _, body := range fixtures {
		if _, err := parseSegmentMetas(body, baseURL, mediaFixturePrefix); err == nil {
			test.Errorf(mediaFixtureAcceptedErrorFormat, body)
		}
	}

	if _, err := parseSegmentMetas(mediaFixtureBody, nil, mediaFixturePrefix); err == nil {
		test.Error("accepted missing base URL")
	}
}

func FuzzMediaPlaylist(fuzzer *testing.F) {
	fuzzer.Add(mediaFixtureBody)
	fuzzer.Add("")
	fuzzer.Add(mediaFixtureStart)
	fuzzer.Fuzz(func(test *testing.T, body string) {
		baseURL, err := url.Parse(mediaFixtureURL)
		if err != nil {
			test.Fatal(err)
		}

		_, _ = parseSegmentMetas(body, baseURL, mediaFixturePrefix)
	})
}
