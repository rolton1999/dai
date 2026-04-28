package main

import (
	"fmt"
	"os"
)

func downloadSingleVideo(logger logger, videoID string) error {
	videoInfo, err := getVideoInfo(videoID)
	if err != nil {
		return fmt.Errorf("cannot get videoInfo: %w", err)
	}

	resultingFileName := videoInfo.title + ".mp4"

	logger.NewPrefix(resultingFileName)

	variantPlaylists, err := getVariantPlaylists(videoInfo.masterPlaylistLink)
	if err != nil {
		return fmt.Errorf("cannot get variant playlists by master link: %w", err)
	}

	best := getPlaylistWithBestResolution(variantPlaylists)

	const dir = ".tmp"
	defer func() {
		_ = os.RemoveAll(dir)
	}()

	tsFiles, err := downloadTSFilesToDir(logger, dir, resultingFileName, best)
	if err != nil {
		return fmt.Errorf("cannot download ts files: %w", err)
	}

	err = concatTSToResult(logger, tsFiles, resultingFileName)
	if err != nil {
		return fmt.Errorf("cannot concat ts files to mp4: %w", err)
	}

	logger.PrintNewLine()

	return nil
}

func downloadPlaylist(logger logger, playlistID string) error {
	playlistInfo, err := getPlaylistInfo(playlistID)
	if err != nil {
		return fmt.Errorf("cannot get playlist info: %w", err)
	}

	for _, id := range playlistInfo.videoIDs {
		if err := downloadSingleVideo(logger, id); err != nil {
			return err
		}
	}

	return nil
}
