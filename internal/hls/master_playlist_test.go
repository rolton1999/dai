package hls

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const (
	masterFixtureURL            = "https://cdn.example.test/path/master.m3u8"
	masterResolvedVariant       = "https://cdn.example.test/media.m3u8"
	masterFixtureBody           = "#EXTM3U\n#EXT-X-VERSION:3\n# comment\n#EXT-X-STREAM-INF:RESOLUTION=1280x720,CODECS=\"avc1, mp4a\",BANDWIDTH=1000\n../media.m3u8\n"
	masterMinimalBody           = "#EXTM3U\n#EXT-X-STREAM-INF:RESOLUTION=1280x720,BANDWIDTH=1000\n../media.m3u8\n"
	masterInvalidResultFormat   = "unexpected variants: %v, error: %v"
	masterAcceptedInvalidFormat = "accepted invalid master: %q"
	masterFirstCodec            = "avc1"
	masterSecondCodec           = "mp4a"
	masterWidth                 = 1280
	masterHeight                = 720
	masterBandwidth             = 1000
	masterCodecCount            = 2
)

func TestMasterPlaylistParsesFlexibleHeaders(test *testing.T) {
	baseURL, err := url.Parse(masterFixtureURL)
	if err != nil {
		test.Fatal(err)
	}

	for _, body := range []string{
		masterFixtureBody,
		strings.ReplaceAll(masterFixtureBody, "\n", "\r\n"),
		strings.TrimSpace(masterFixtureBody),
		strings.Replace(masterFixtureBody, "../media.m3u8", "\n# variant comment\n../media.m3u8", 1),
		masterMinimalBody,
	} {
		variants, err := parseMasterPlaylist(body, baseURL)
		if err != nil || len(variants) != 1 {
			test.Fatalf(masterInvalidResultFormat, variants, err)
		}

		variant := variants[0]
		if variant.Link != masterResolvedVariant || variant.resolution != (Resolution{width: masterWidth, height: masterHeight}) || variant.bandwidth != masterBandwidth {
			test.Fatalf(masterInvalidResultFormat, variants, err)
		}

		if body != masterMinimalBody && (len(variant.codecs) != masterCodecCount || variant.codecs[0] != masterFirstCodec || variant.codecs[1] != masterSecondCodec) {
			test.Fatalf(masterInvalidResultFormat, variants, err)
		}
	}
}

func TestMasterPlaylistRejectsMalformedOrUnsupportedInput(test *testing.T) {
	baseURL, err := url.Parse(masterFixtureURL)
	if err != nil {
		test.Fatal(err)
	}

	bodies := []string{
		"", "#EXTM3U", "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000", "#EXTM3U\norphan.m3u8",
		strings.ReplaceAll(masterFixtureBody, "1280x720", "0x720"),
		strings.ReplaceAll(masterFixtureBody, "1280x720", "badx720"),
		strings.ReplaceAll(masterFixtureBody, "1280x720", "65536x720"),
		strings.ReplaceAll(masterFixtureBody, "BANDWIDTH=1000", "BANDWIDTH=0"),
		strings.ReplaceAll(masterFixtureBody, "BANDWIDTH=1000", "BANDWIDTH=1000,BANDWIDTH=2000"),
		strings.ReplaceAll(masterFixtureBody, "BANDWIDTH=1000", "BANDWIDTH=1000,FRAME-RATE=NaN"),
		strings.ReplaceAll(masterFixtureBody, "BANDWIDTH=1000", "BANDWIDTH=1000,FRAME-RATE=+Inf"),
		strings.ReplaceAll(masterFixtureBody, "BANDWIDTH=1000", "BANDWIDTH=1000,AUDIO=\"external\""),
		strings.ReplaceAll(masterFixtureBody, "BANDWIDTH=1000", "BANDWIDTH=1000,"),
		strings.ReplaceAll(masterFixtureBody, "CODECS=\"avc1, mp4a\"", "CODECS=\"unterminated"),
		strings.ReplaceAll(masterFixtureBody, "../media.m3u8", "file:///tmp/media"),
		strings.ReplaceAll(masterFixtureBody, "../media.m3u8", "https://user@cdn.example.test/media.m3u8"),
		"#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO", "#EXTM3U\n#EXT-X-SESSION-KEY:METHOD=AES-128",
	}
	for _, body := range bodies {
		if _, err := parseMasterPlaylist(body, baseURL); err == nil {
			test.Errorf(masterAcceptedInvalidFormat, body)
		}
	}

	if _, err := parseMasterPlaylist(masterFixtureBody, nil); err == nil {
		test.Fatal("accepted missing base URL")
	}
}

func TestBestVariantSelection(test *testing.T) {
	const (
		higherBandwidth  = 2000
		squareDimension  = 1000
		smallerDimension = 360
	)

	first := VariantPlaylist{resolution: Resolution{width: masterWidth, height: masterHeight}, bandwidth: masterBandwidth}
	faster := first
	faster.bandwidth = higherBandwidth
	square := VariantPlaylist{resolution: Resolution{width: squareDimension, height: squareDimension}, bandwidth: masterBandwidth}

	best, err := GetPlaylistWithBestResolution([]VariantPlaylist{
		first,
		faster,
	})
	if err != nil || best.bandwidth != higherBandwidth {
		test.Fatalf(masterInvalidResultFormat, best, err)
	}

	best, err = GetPlaylistWithBestResolution([]VariantPlaylist{
		first,
		square,
	})
	if err != nil || best.resolution != square.resolution {
		test.Fatalf(masterInvalidResultFormat, best, err)
	}

	for _, variants := range [][]VariantPlaylist{
		nil,
		{{}},
		{
			first,
			{resolution: Resolution{width: smallerDimension}},
		},
		{
			first,
			{resolution: Resolution{width: maximumDimension + 1, height: masterHeight}},
		},
	} {
		if _, err := GetPlaylistWithBestResolution(variants); err == nil {
			test.Fatal("accepted empty variants or invalid dimensions")
		}
	}
}

type masterTransport func(*http.Request) (*http.Response, error)

func (transport masterTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type masterBody struct {
	io.Reader
	closed bool
}

func (body *masterBody) Close() error {
	body.closed = true
	return nil
}

func TestMasterPlaylistResolvesAfterRedirect(test *testing.T) {
	const (
		redirectPath   = "/redirect"
		finalPath      = "/path/master.m3u8"
		locationHeader = "Location"
	)

	originalClient := http.DefaultClient

	test.Cleanup(func() {
		http.DefaultClient = originalClient
	})

	bodyReader := strings.NewReader(masterFixtureBody)
	body := &masterBody{Reader: bodyReader}
	transport := masterTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == redirectPath {
			bodyReader := strings.NewReader("")
			responseBody := io.NopCloser(bodyReader)
			response := &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{locationHeader: []string{masterFixtureURL}},
				Body:       responseBody,
				Request:    request,
			}

			return response, nil
		}

		if request.URL.Path != finalPath {
			test.Errorf("unexpected request: %s", request.URL)
		}

		response := &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
			Request:    request,
		}

		return response, nil
	})

	http.DefaultClient = &http.Client{Transport: transport}

	variants, err := GetVariantPlaylists(context.Background(), "https://cdn.example.test"+redirectPath)
	if err != nil || len(variants) != 1 || variants[0].Link != masterResolvedVariant {
		test.Fatalf(masterInvalidResultFormat, variants, err)
	}

	if !body.closed {
		test.Fatal("master response body was not closed")
	}
}

func FuzzMasterPlaylist(fuzzer *testing.F) {
	fuzzer.Add(masterFixtureBody)
	fuzzer.Add("#EXTM3U")
	fuzzer.Add("")
	fuzzer.Fuzz(func(test *testing.T, body string) {
		baseURL, err := url.Parse(masterFixtureURL)
		if err != nil {
			test.Fatal(err)
		}

		_, _ = parseMasterPlaylist(body, baseURL)
	})
}

func TestMasterPlaylistResolvesVariantReferences(test *testing.T) {
	const (
		relativeReference = "media.m3u8"
		rootReference     = "/media.m3u8"
		absoluteReference = "https://other.example.test/media.m3u8"
		relativeResult    = "https://cdn.example.test/path/media.m3u8"
		originalReference = "../media.m3u8"
	)

	baseURL, err := url.Parse(masterFixtureURL)
	if err != nil {
		test.Fatal(err)
	}

	fixtures := []struct {
		reference string
		expected  string
	}{
		{relativeReference, relativeResult}, {rootReference, masterResolvedVariant}, {absoluteReference, absoluteReference},
	}
	for _, fixture := range fixtures {
		body := strings.ReplaceAll(masterFixtureBody, originalReference, fixture.reference)

		variants, err := parseMasterPlaylist(body, baseURL)
		if err != nil || len(variants) != 1 || variants[0].Link != fixture.expected {
			test.Fatalf(masterInvalidResultFormat, variants, err)
		}
	}
}
