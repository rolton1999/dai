package rutube

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type PlaylistInfo struct {
	Title    string
	VideoIDs []string
}

func (pi PlaylistInfo) String() string {
	const template = "{\n\ttitle: %s,\n\tvideoIDs: %v\n}"
	return fmt.Sprintf(template, pi.Title, pi.VideoIDs)
}

func GetPlaylistInfo(playlistID string) (PlaylistInfo, error) {
	const playlistApiURL = "https://rutube.ru/api/playlist/custom/%s/?format=json"
	resp, err := http.Get(fmt.Sprintf(playlistApiURL, playlistID))
	if err != nil {
		return PlaylistInfo{}, fmt.Errorf("cannot get playlist info: %w", err)
	}
	defer resp.Body.Close()

	// can get title from here
	// const template = "https://rutube.ru/api/playlist/custom/%s/?format=json"
	title, err := parseTitleFromPlaylistApiResponse(resp.Body)
	if err != nil {
		return PlaylistInfo{}, fmt.Errorf("cannot parse playlist title from api reponse: %w", err)
	}

	const playlistApiDetailsURL = "https://rutube.ru/api/playlist/custom/%s/videos/?format=json"
	resp, err = http.Get(fmt.Sprintf(playlistApiDetailsURL, playlistID))
	if err != nil {
		return PlaylistInfo{}, fmt.Errorf("cannot get playlist details: %w", err)
	}
	defer resp.Body.Close()

	ids, err := parseVideoIDsFromPlaylistApiResponse(resp.Body)

	if err != nil {
		return PlaylistInfo{}, fmt.Errorf("cannot parse video IDs from api reponse: %w", err)
	}

	return PlaylistInfo{title, ids}, nil
}

func parseTitleFromPlaylistApiResponse(bodyReader io.Reader) (string, error) {
	body, err := io.ReadAll(bodyReader)

	type expectedBody struct {
		Title string `json:"title"`
	}

	var eBody expectedBody

	err = json.Unmarshal(body, &eBody)
	if err != nil {
		return "", fmt.Errorf("cannot unmarshal body: %w", err)
	}

	return eBody.Title, nil
}

func parseVideoIDsFromPlaylistApiResponse(bodyReader io.Reader) ([]string, error) {
	body, err := io.ReadAll(bodyReader)

	type result struct {
		ID string `json:"id"`
	}
	type expectedBody struct {
		Results []result `json:"results"`
	}
	var eBody expectedBody

	err = json.Unmarshal(body, &eBody)
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshal body: %w", err)
	}

	ids := make([]string, 0, len(eBody.Results))
	for _, r := range eBody.Results {
		ids = append(ids, r.ID)
	}

	return ids, nil
}
