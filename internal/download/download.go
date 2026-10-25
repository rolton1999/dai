package download

import (
	"context"
	"dai/internal/ffmpeg"
	"dai/internal/hls"
	"dai/internal/progress"
	"dai/internal/rutube"
	"errors"
	"fmt"
)

const (
	videoInfoErrorFormat    = "cannot get videoInfo: %w"
	variantsErrorFormat     = "cannot get variant playlists by master link: %w"
	segmentsErrorFormat     = "cannot download ts files: %w"
	conversionErrorFormat   = "cannot concat ts files to mp4: %w"
	cleanupErrorFormat      = "cannot remove temporary segments: %w"
	playlistInfoErrorFormat = "cannot get playlist info: %w"
	outputExtension         = ".mp4"
)

type conversionFunc func(progress.Logger, []string, string) error

func DownloadSingleVideo(logger progress.Logger, videoID string) error {
	return downloadSingleVideo(context.Background(), logger, videoID, ffmpeg.ConcatTSToResult)
}

func DownloadPlaylist(logger progress.Logger, playlistID string) error {
	playlistInfo, err := rutube.GetPlaylistInfo(playlistID)
	if err != nil {
		return fmt.Errorf(playlistInfoErrorFormat, err)
	}

	for _, videoID := range playlistInfo.VideoIDs {
		if err := DownloadSingleVideo(logger, videoID); err != nil {
			return err
		}
	}

	return nil
}

func downloadSingleVideo(
	operationContext context.Context,
	logger progress.Logger,
	videoID string,
	convert conversionFunc,
) (resultError error) {
	videoInfo, err := rutube.GetVideoInfo(videoID)
	if err != nil {
		return fmt.Errorf(videoInfoErrorFormat, err)
	}

	resultingFileName := videoInfo.Title + outputExtension

	logger.NewPrefix(resultingFileName)

	variantPlaylists, err := hls.GetVariantPlaylists(videoInfo.MasterPlaylistLink)
	if err != nil {
		return fmt.Errorf(variantsErrorFormat, err)
	}

	best, err := hls.GetPlaylistWithBestResolution(variantPlaylists)
	if err != nil {
		return err
	}

	artifact, err := hls.DownloadTSFiles(operationContext, logger, best)
	if err != nil {
		return fmt.Errorf(segmentsErrorFormat, err)
	}

	defer func() {
		if err := artifact.Close(); err != nil {
			resultError = errors.Join(resultError, fmt.Errorf(cleanupErrorFormat, err))
		}
	}()

	err = convert(logger, artifact.Files(), resultingFileName)
	if err != nil {
		return fmt.Errorf(conversionErrorFormat, err)
	}

	logger.PrintNewLine()

	return nil
}
