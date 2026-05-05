package usecase

import (
	"dai/internal/ffmpeg"
	"dai/internal/m3u"
	"dai/internal/rutube"
	"dai/internal/util"

	"fmt"
	"os"
)

func DownloadSingleVideo(logger util.Logger, videoID string) error {
	videoInfo, err := rutube.GetVideoInfo(videoID)
	if err != nil {
		return fmt.Errorf("cannot get videoInfo: %w", err)
	}

	resultingFileName := videoInfo.Title + ".mp4"

	logger.NewPrefix(resultingFileName)

	variantPlaylists, err := m3u.GetVariantPlaylists(videoInfo.MasterPlaylistLink)
	if err != nil {
		return fmt.Errorf("cannot get variant playlists by master link: %w", err)
	}

	best := m3u.GetPlaylistWithBestResolution(variantPlaylists)

	const dir = ".tmp"
	defer func() {
		_ = os.RemoveAll(dir)
	}()

	tsFiles, err := m3u.DownloadTSFilesToDir(logger, dir, resultingFileName, best)
	if err != nil {
		return fmt.Errorf("cannot download ts files: %w", err)
	}

	err = ffmpeg.ConcatTSToResult(logger, tsFiles, resultingFileName)
	if err != nil {
		return fmt.Errorf("cannot concat ts files to mp4: %w", err)
	}

	logger.PrintNewLine()

	return nil
}

func DownloadPlaylist(logger util.Logger, playlistID string) error {
	playlistInfo, err := rutube.GetPlaylistInfo(playlistID)
	if err != nil {
		return fmt.Errorf("cannot get playlist info: %w", err)
	}

	for _, id := range playlistInfo.VideoIDs {
		if err := DownloadSingleVideo(logger, id); err != nil {
			return err
		}
	}

	return nil
}
