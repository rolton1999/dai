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
	const (
		playlistApiURL        = "https://rutube.ru/api/playlist/custom/%s/?format=json"
		playlistApiDetailsURL = "https://rutube.ru/api/playlist/custom/%s/videos/?format=json"
	)

	resp, err := http.Get(fmt.Sprintf(playlistApiURL, playlistID))
	if err != nil {
		return PlaylistInfo{}, fmt.Errorf("cannot get playlist info: %w", err)
	}

	// can get title from here
	title, err := parseTitleFromPlaylistApiResponse(resp.Body)
	_ = resp.Body.Close()

	if err != nil {
		return PlaylistInfo{}, fmt.Errorf("cannot parse playlist title from api reponse: %w", err)
	}

	res := PlaylistInfo{Title: title}

	next := fmt.Sprintf(playlistApiDetailsURL, playlistID)
	for len(next) != 0 {
		resp, err = http.Get(next)
		if err != nil {
			return PlaylistInfo{}, fmt.Errorf("cannot get playlist details: %w", err)
		}

		var ids []string

		ids, next, err = parseVideoIDsFromPlaylistApiResponse(resp.Body)
		_ = resp.Body.Close()

		if err != nil {
			return PlaylistInfo{}, fmt.Errorf("cannot parse video IDs from api reponse: %w", err)
		}

		res.VideoIDs = append(res.VideoIDs, ids...)
	}

	return res, nil
}

func parseTitleFromPlaylistApiResponse(bodyReader io.Reader) (string, error) {
	body, err := io.ReadAll(bodyReader)
	if err != nil {
		return "", fmt.Errorf("cannot read body: %w", err)
	}

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

func parseVideoIDsFromPlaylistApiResponse(bodyReader io.Reader) ([]string, string, error) {
	body, err := io.ReadAll(bodyReader)
	if err != nil {
		return nil, "", fmt.Errorf("cannot read body: %w", err)
	}

	type result struct {
		ID string `json:"id"`
	}

	type expectedBody struct {
		Results []result `json:"results"`
		HasNext bool     `json:"has_next"`
		Next    string   `json:"next"`
	}

	var eBody expectedBody

	err = json.Unmarshal(body, &eBody)
	if err != nil {
		return nil, "", fmt.Errorf("cannot unmarshal body: %w", err)
	}

	ids := make([]string, 0, len(eBody.Results))
	for _, r := range eBody.Results {
		ids = append(ids, r.ID)
	}

	next := ""
	if eBody.HasNext {
		next = eBody.Next
	}

	return ids, next, nil
}
