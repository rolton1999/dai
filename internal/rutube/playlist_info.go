package rutube

import (
	"bytes"
	"context"
	"dai/internal/http/client"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

const (
	playlistPageLimit        = 10000
	playlistResponseLimit    = 32 * 1024 * 1024
	playlistCycleError       = "cyclic or excessive playlist pagination"
	playlistSizeError        = "playlist metadata exceeds total size limit"
	playlistMissingBaseError = "playlist response URL is missing"

	playlistMissingTitleMessage   = "playlist title is missing"
	playlistMissingResultsMessage = "playlist results are missing"
	playlistMissingNextMessage    = "playlist continuation URL is missing"
	playlistMissingVideoIDMessage = "playlist video ID is missing"
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
	provider := New(nil)
	return provider.Playlist(context.Background(), playlistID)
}

func getPlaylistInfo(operationContext context.Context, playlistID string) (PlaylistInfo, error) {
	reader := client.New(nil)
	return getPlaylistInfoWithReader(operationContext, reader, playlistID)
}

func getPlaylistInfoWithReader(
	operationContext context.Context,
	reader client.Client,
	playlistID string,
) (PlaylistInfo, error) {
	const (
		playlistAPI       = "https://rutube.ru/api/playlist/custom/%s/?format=json"
		playlistVideosAPI = "https://rutube.ru/api/playlist/custom/%s/videos/?format=json"
	)

	titleLink := fmt.Sprintf(playlistAPI, url.PathEscape(playlistID))

	response, err := reader.Read(operationContext, titleLink)
	if err != nil {
		return PlaylistInfo{}, err
	}

	titleReader := bytes.NewReader(response.Body)

	title, err := parseTitleFromPlaylistApiResponse(titleReader)
	if err != nil {
		return PlaylistInfo{}, err
	}

	result := PlaylistInfo{Title: title}
	next := fmt.Sprintf(playlistVideosAPI, url.PathEscape(playlistID))
	seen := make(map[string]bool)

	responseBytes := len(response.Body)
	for next != "" {
		if seen[next] || len(seen) >= playlistPageLimit {
			return PlaylistInfo{}, errors.New(playlistCycleError)
		}

		seen[next] = true

		response, err = reader.Read(operationContext, next)
		if err != nil {
			return PlaylistInfo{}, err
		}

		responseBytes += len(response.Body)
		if responseBytes > playlistResponseLimit {
			return PlaylistInfo{}, errors.New(playlistSizeError)
		}

		pageReader := bytes.NewReader(response.Body)

		ids, reference, err := parseVideoIDsFromPlaylistApiResponse(pageReader)
		if err != nil {
			return PlaylistInfo{}, err
		}

		result.VideoIDs = append(result.VideoIDs, ids...)
		next = ""

		if reference != "" {
			parsed, err := url.Parse(reference)
			if err != nil {
				return PlaylistInfo{}, err
			}

			if response.URL == nil {
				return PlaylistInfo{}, errors.New(playlistMissingBaseError)
			}

			next = response.URL.ResolveReference(parsed).String()
		}
	}

	return result, nil
}

func parseTitleFromPlaylistApiResponse(bodyReader io.Reader) (string, error) {
	body, err := io.ReadAll(bodyReader)
	if err != nil {
		return "", fmt.Errorf(metadataReadErrorFormat, err)
	}

	type expectedBody struct {
		Title string `json:"title"`
	}

	var metadata expectedBody

	err = json.Unmarshal(body, &metadata)
	if err != nil {
		return "", fmt.Errorf(metadataDecodeErrorFormat, err)
	}

	if strings.TrimSpace(metadata.Title) == "" {
		return "", errors.New(playlistMissingTitleMessage)
	}

	return metadata.Title, nil
}

func parseVideoIDsFromPlaylistApiResponse(bodyReader io.Reader) ([]string, string, error) {
	body, err := io.ReadAll(bodyReader)
	if err != nil {
		return nil, "", fmt.Errorf(metadataReadErrorFormat, err)
	}

	type result struct {
		ID string `json:"id"`
	}

	type expectedBody struct {
		Results []result `json:"results"`
		HasNext bool     `json:"has_next"`
		Next    string   `json:"next"`
	}

	var metadata expectedBody

	err = json.Unmarshal(body, &metadata)
	if err != nil {
		return nil, "", fmt.Errorf(metadataDecodeErrorFormat, err)
	}

	if metadata.Results == nil {
		return nil, "", errors.New(playlistMissingResultsMessage)
	}

	if metadata.HasNext && strings.TrimSpace(metadata.Next) == "" {
		return nil, "", errors.New(playlistMissingNextMessage)
	}

	ids := make([]string, 0, len(metadata.Results))
	for _, videoResult := range metadata.Results {
		if strings.TrimSpace(videoResult.ID) == "" {
			return nil, "", errors.New(playlistMissingVideoIDMessage)
		}

		ids = append(ids, videoResult.ID)
	}

	next := ""
	if metadata.HasNext {
		next = metadata.Next
	}

	return ids, next, nil
}
