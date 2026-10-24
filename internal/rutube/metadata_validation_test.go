package rutube

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const (
	metadataEmptyObject           = `{}`
	metadataNullObject            = `null`
	metadataMalformedJSON         = `{invalid`
	metadataBlankText             = " \t"
	metadataVideoTemplate         = `{"title":%q,"video_balancer":{"m3u8":%q}}`
	metadataTitleTemplate         = `{"title":%q}`
	metadataMissingBalancer       = `{"title":"video"}`
	metadataMissingVideoTitle     = `{"video_balancer":{"m3u8":"https://example.test/master.m3u8"}}`
	metadataValidVideoTitle       = "video"
	metadataValidVideoURL         = "https://example.test/master.m3u8"
	metadataValidHTTPURL          = "http://example.test/master.m3u8"
	metadataPaddedTitle           = " video "
	metadataQueriedURL            = "https://example.test/master.m3u8?token=a%2Fb#variant"
	metadataVideoCaseName         = "video"
	metadataPlaylistCaseName      = "playlist"
	metadataFileURL               = "file:///tmp/master.m3u8"
	metadataRelativeURL           = "/master.m3u8"
	metadataCredentialURL         = "https://user@example.test/master.m3u8"
	metadataHostlessURL           = "https:///master.m3u8"
	metadataInvalidEscapeURL      = "https://example.test/%invalid"
	metadataAcceptedInvalidFormat = "accepted invalid metadata: %q"
	metadataUnexpectedVideoFormat = "info=%v error=%v"
	metadataUnexpectedTitleFormat = "title=%q error=%v"
	metadataUnexpectedPageFormat  = "ids=%v next=%q error=%v"
	metadataReadFailureMessage    = "fixture interrupted metadata read"
	metadataEmptyPage             = `{"results":[],"has_next":false}`
	metadataNullResults           = `{"results":null}`
	metadataMissingVideoID        = `{"results":[{}]}`
	metadataEmptyVideoID          = `{"results":[{"id":""}]}`
	metadataBlankVideoID          = `{"results":[{"id":" \t"}]}`
	metadataMissingNext           = `{"results":[{"id":"first"}],"has_next":true}`
	metadataBlankNext             = `{"results":[{"id":"first"}],"has_next":true,"next":" \t"}`
	metadataFinalPage             = `{"results":[{"id":"first"}],"has_next":false,"next":"stale"}`
	metadataFirstID               = "first"
	metadataHTTPAcceptedMessage   = "HTTP 200 error object was accepted as metadata"
	metadataPageRequestedMessage  = "playlist pages requested after invalid title metadata"
	metadataBodyNotClosedMessage  = "invalid metadata body was not closed"
)

func TestVideoMetadataRequiresTitleAndUsablePlaylistURL(test *testing.T) {
	fixtures := []struct {
		title string
		link  string
		valid bool
	}{
		{metadataValidVideoTitle, metadataValidVideoURL, true},
		{metadataValidVideoTitle, metadataValidHTTPURL, true},
		{metadataPaddedTitle, metadataQueriedURL, true},
		{metadataBlankText, metadataValidVideoURL, false},
		{"", metadataValidVideoURL, false},
		{metadataValidVideoTitle, "", false},
		{metadataValidVideoTitle, metadataBlankText, false},
		{metadataValidVideoTitle, metadataFileURL, false},
		{metadataValidVideoTitle, metadataRelativeURL, false},
		{metadataValidVideoTitle, metadataCredentialURL, false},
		{metadataValidVideoTitle, metadataHostlessURL, false},
		{metadataValidVideoTitle, metadataInvalidEscapeURL, false},
	}
	for _, fixture := range fixtures {
		body := fmt.Sprintf(metadataVideoTemplate, fixture.title, fixture.link)
		reader := strings.NewReader(body)

		info, err := parseVideoInfoFromVideoApiResponse(reader)
		if fixture.valid {
			if err != nil || info.Title != fixture.title || info.MasterPlaylistLink != fixture.link {
				test.Fatalf(metadataUnexpectedVideoFormat, info, err)
			}
		} else if err == nil {
			test.Errorf(metadataAcceptedInvalidFormat, body)
		}
	}

	for _, body := range []string{
		metadataEmptyObject,
		metadataNullObject,
		metadataMalformedJSON,
		metadataMissingBalancer,
		metadataMissingVideoTitle,
	} {
		reader := strings.NewReader(body)
		if _, err := parseVideoInfoFromVideoApiResponse(reader); err == nil {
			test.Errorf(metadataAcceptedInvalidFormat, body)
		}
	}
}

func TestPlaylistMetadataRequiresNonblankTitle(test *testing.T) {
	for _, body := range []string{
		metadataEmptyObject,
		metadataNullObject,
		metadataMalformedJSON,
		fmt.Sprintf(metadataTitleTemplate, metadataBlankText),
	} {
		reader := strings.NewReader(body)
		if _, err := parseTitleFromPlaylistApiResponse(reader); err == nil {
			test.Errorf(metadataAcceptedInvalidFormat, body)
		}
	}

	reader := strings.NewReader(retryPlaylistTitleBody)

	title, err := parseTitleFromPlaylistApiResponse(reader)
	if err != nil || title == "" {
		test.Fatalf(metadataUnexpectedTitleFormat, title, err)
	}
}

func TestPlaylistPagesRequireResultsIDsAndContinuation(test *testing.T) {
	for _, body := range []string{
		metadataEmptyObject,
		metadataNullObject,
		metadataMalformedJSON,
		metadataNullResults,
		metadataMissingVideoID,
		metadataEmptyVideoID,
		metadataBlankVideoID,
		metadataMissingNext,
		metadataBlankNext,
	} {
		reader := strings.NewReader(body)
		if _, _, err := parseVideoIDsFromPlaylistApiResponse(reader); err == nil {
			test.Errorf(metadataAcceptedInvalidFormat, body)
		}
	}

	for _, body := range []string{
		metadataEmptyPage,
		metadataFinalPage,
		retryPlaylistFirstPage,
	} {
		reader := strings.NewReader(body)

		ids, next, err := parseVideoIDsFromPlaylistApiResponse(reader)
		if err != nil {
			test.Fatalf(metadataUnexpectedPageFormat, ids, next, err)
		}

		if body == metadataEmptyPage && (len(ids) != 0 || next != "") {
			test.Fatalf(metadataUnexpectedPageFormat, ids, next, err)
		}

		if body == metadataFinalPage && (len(ids) != 1 || ids[0] != metadataFirstID || next != "") {
			test.Fatalf(metadataUnexpectedPageFormat, ids, next, err)
		}

		if body == retryPlaylistFirstPage && next == "" {
			test.Fatalf(metadataUnexpectedPageFormat, ids, next, err)
		}
	}
}

func TestVideoMetadataPreservesReadError(test *testing.T) {
	readError := errors.New(metadataReadFailureMessage)
	reader := failingReader{err: readError}

	_, err := parseVideoInfoFromVideoApiResponse(reader)
	if !errors.Is(err, readError) {
		test.Fatalf(metadataUnexpectedVideoFormat, VideoInfo{}, err)
	}
}

func TestHTTP200ErrorJSONIsNotSuccessfulMetadata(test *testing.T) {
	for _, playlist := range []bool{
		false,
		true,
	} {
		name := metadataVideoCaseName
		if playlist {
			name = metadataPlaylistCaseName
		}

		test.Run(name, func(test *testing.T) {
			bodies := make([]*statusResponseBody, 0)
			transport := retryTransport(func(request *http.Request) (*http.Response, error) {
				if playlist && request.URL.Path != retryPlaylistTitlePath {
					test.Error(metadataPageRequestedMessage)
				}

				body := newStatusBody(metadataEmptyObject)
				bodies = append(bodies, body)
				response := &http.Response{
					StatusCode: http.StatusOK,
					Body:       body,
					Request:    request,
				}

				return response, nil
			})
			installStatusTransport(test, transport)

			var err error
			if playlist {
				_, err = getPlaylistInfo(context.Background(), retryPlaylistID)
			} else {
				_, err = getVideoInfo(context.Background(), retryVideoID)
			}

			if err == nil {
				test.Fatal(metadataHTTPAcceptedMessage)
			}

			for _, body := range bodies {
				if !body.closed {
					test.Error(metadataBodyNotClosedMessage)
				}
			}
		})
	}
}
