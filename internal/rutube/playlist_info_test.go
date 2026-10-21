package rutube

import (
	"errors"
	"testing"
)

type failingReader struct {
	err error
}

func (r failingReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestPlaylistParsersPreserveReadErrors(t *testing.T) {
	readErr := errors.New("interrupted response")

	_, err := parseTitleFromPlaylistApiResponse(failingReader{err: readErr})
	if !errors.Is(err, readErr) {
		t.Fatalf("title parser: expected read error, got %v", err)
	}

	_, _, err = parseVideoIDsFromPlaylistApiResponse(failingReader{err: readErr})
	if !errors.Is(err, readErr) {
		t.Fatalf("video ID parser: expected read error, got %v", err)
	}
}
