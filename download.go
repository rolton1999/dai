package main

import (
	"fmt"
	"os"
)

func downloadSingleVideo(videoID string) error {
	stopGettingInfoMessage := spinningMessage("Getting info", "Got video info successfully")
	videoInfo, err := getVideoInfo(videoID)
	if err != nil {
		return fmt.Errorf("cannot get videoInfo: %w", err)
	}

	variantPlaylists, err := getVariantPlaylists(videoInfo.masterPlaylistLink)
	if err != nil {
		return fmt.Errorf("cannot get variant playlists by master link: %w", err)
	}

	best := getPlaylistWithBestResolution(variantPlaylists)

	stopGettingInfoMessage()

	const dir = ".tmp"
	defer func() {
		_ = os.RemoveAll(dir)
	}()

	tsFiles, err := downloadTSFilesToDir(dir, videoInfo.title, best)
	if err != nil {
		return fmt.Errorf("cannot download ts files: %w", err)
	}

	err = concatTSToResult(tsFiles, videoInfo.title+".mp4")
	if err != nil {
		return fmt.Errorf("cannot concat ts files to mp4: %w", err)
	}

	return nil
}
