package cli

import "testing"

const (
	fixtureVideoID    = "dace2feac379872d42e49b6b25c0587a"
	fixturePlaylistID = "1840659"
	parseResultError  = "kind=%v id=%q error=%v"
)

func TestParseLink(test *testing.T) {
	fixtures := []struct {
		link string
		kind argType
		id   string
	}{
		{"https://rutube.ru/video/" + fixtureVideoID, singleVideo, fixtureVideoID},
		{"https://rutube.ru/video/" + fixtureVideoID + "/?utm_source=x#fragment", singleVideo, fixtureVideoID},
		{"https://rutube.ru/plst/1840659/?query=1#fragment", playlist, fixturePlaylistID},
		{"https://rutube.ru/plst/1840659", playlist, fixturePlaylistID},
		{"https://rutube.ru/plst/1840659/", playlist, fixturePlaylistID},
		{"https://rutube.ru/video/" + fixtureVideoID + "/", singleVideo, fixtureVideoID},
		{"https://rutube.ru/video/" + fixtureVideoID + "?query=1#fragment", singleVideo, fixtureVideoID},
		{"https://RUTUBE.RU/video/" + fixtureVideoID, singleVideo, fixtureVideoID},
		{"http://rutube.ru/video/" + fixtureVideoID, notExpected, ""},
		{"https://rutube.ru/plst/", notExpected, ""},
		{"https://rutube.ru/plst/abc", notExpected, ""},
		{"https://rutube.ru/plst/1840659//", notExpected, ""},
		{"https://rutube.ru/video/short", notExpected, ""},
		{"https://rutube.ru/video/" + fixtureVideoID + "//", notExpected, ""},
		{"https://rutube.ru:443/video/" + fixtureVideoID, notExpected, ""},
		{"https://rutube.ru:invalid/video/" + fixtureVideoID, notExpected, ""},
		{"https://rutube.ru/videosomething/" + fixtureVideoID, notExpected, ""},
		{"https://rutube.ru/video/", notExpected, ""},
		{"https://rutube.ru.evil.test/video/" + fixtureVideoID, notExpected, ""},
		{"https://user@rutube.ru/video/" + fixtureVideoID, notExpected, ""},
		{"https://rutube.ru/plst/0", notExpected, ""},
		{"https://rutube.ru/video/" + fixtureVideoID + "/extra", notExpected, ""},
		{"https://rutube.ru/video/%2f", notExpected, ""},
	}
	for _, fixture := range fixtures {
		test.Run(fixture.link, func(test *testing.T) {
			kind, id, err := parseLink(fixture.link)
			if kind != fixture.kind || id != fixture.id || (err != nil) != (fixture.kind == notExpected) {
				test.Fatalf(parseResultError, kind, id, err)
			}
		})
	}
}
