package download

import (
	"dai/internal/ffmpeg"
	"dai/internal/hls"
	"dai/internal/progress"
	"dai/internal/rutube"

	"fmt"
)

func DownloadSingleVideo(logger progress.Logger, videoID string) error {
	videoInfo, err := rutube.GetVideoInfo(videoID)
	if err != nil {
		return fmt.Errorf("cannot get videoInfo: %w", err)
	}

	resultingFileName := videoInfo.Title + ".mp4"

	logger.NewPrefix(resultingFileName)

	variantPlaylists, err := hls.GetVariantPlaylists(videoInfo.MasterPlaylistLink)
	if err != nil {
		return fmt.Errorf("cannot get variant playlists by master link: %w", err)
	}

	best := hls.GetPlaylistWithBestResolution(variantPlaylists)

	tsFiles, err := hls.DownloadTSFiles(logger, resultingFileName, best)
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

func DownloadPlaylist(logger progress.Logger, playlistID string) error {
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
