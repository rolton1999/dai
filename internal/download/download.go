package download

import (
	"context"
	"dai/internal/ffmpeg"
	"dai/internal/hls"
	"dai/internal/progress"
	"dai/internal/rutube"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	outputDirectoryPermissions = 0o755
	stagingFilename            = "result.mp4"
	fallbackTitle              = "video"
	readyMessage               = ": READY"
)

const (
	downloadDependenciesAreRequiredMessage     = "download dependencies are required"
	duplicatePlaylistOutputFormat              = "duplicate playlist output: %s"
	converterDidNotProduceANonemptyFileMessage = "converter did not produce a nonempty file"
	cannotPublishVideoFormat                   = "cannot publish video: %w"
	outputAlreadyExistsFormat                  = "output already exists: %s"
)

const (
	defaultOutputDirectory = "."
	outputExtension        = ".mp4"
	outputNameLimit        = 200
	videoTimeout           = 30 * time.Minute
	outputWorkspacePattern = ".dai-output-*"
)

const (
	filenameReplacement       = "_"
	filenameReplacementRune   = '_'
	filenameSlash             = '/'
	filenameBackslash         = '\\'
	trimmedFilenameCharacters = ". "
	leadingHyphen             = "-"
)

const DefaultJobs = 4

const (
	invalidJobsMessage       = "jobs must be greater than zero"
	playlistVideoErrorFormat = "playlist item %d (%s): %w"
)

type Options struct {
	Jobs            int
	OutputDirectory string
	Overwrite       bool
	Resume          bool
}

type Service interface {
	Video(context.Context, string, Options) error
	Playlist(context.Context, string, Options) error
}

func New(
	provider rutube.Client,
	streams hls.Client,
	converter ffmpeg.Converter,
	logger progress.Logger,
) (Service, error) {
	if provider == nil || streams == nil || converter == nil || logger == nil {
		return nil, errors.New(downloadDependenciesAreRequiredMessage)
	}

	return &serviceImpl{provider: provider, streams: streams, converter: converter, logger: logger}, nil
}

type serviceImpl struct {
	provider  rutube.Client
	streams   hls.Client
	converter ffmpeg.Converter
	logger    progress.Logger
}

func (service *serviceImpl) Video(operationContext context.Context, videoID string, options Options) error {
	if options.Resume {
		return errors.New(resumeOnlyPlaylistMessage)
	}

	info, err := service.provider.Video(operationContext, videoID)
	if err != nil {
		return err
	}

	directory, err := outputDirectory(options.OutputDirectory)
	if err != nil {
		return err
	}

	target := filepath.Join(directory, safeFilename(info.Title))
	if err := checkDestination(target, options.Overwrite); err != nil {
		return err
	}

	return service.download(operationContext, info, target, options.Overwrite, service.logger, nil)
}

func (service *serviceImpl) Playlist(operationContext context.Context, playlistID string, options Options) (resultError error) {
	if options.Resume && options.Overwrite {
		return errors.New(resumeOverwriteMessage)
	}

	if options.Jobs < 0 {
		return errors.New(invalidJobsMessage)
	}

	playlist, err := service.provider.Playlist(operationContext, playlistID)
	if err != nil {
		return err
	}

	directory, err := outputDirectory(options.OutputDirectory)
	if err != nil {
		return err
	}

	var recovery *playlistRecovery
	if options.Resume {
		recovery, err = newPlaylistRecovery(directory, playlistID)
		if err != nil {
			return err
		}

		defer func() {
			resultError = errors.Join(resultError, recovery.Close())
		}()
	}

	infos := make([]rutube.VideoInfo, 0, len(playlist.VideoIDs))
	targets := make([]string, 0, len(playlist.VideoIDs))
	seen := make(map[string]bool)

	for _, videoID := range playlist.VideoIDs {
		complete, err := recovery.inspect(operationContext, videoID)
		if err != nil {
			return err
		}

		var info rutube.VideoInfo
		if complete {
			info.Title = strings.TrimSuffix(recovery.filename(videoID), outputExtension)
		} else {
			info, err = service.provider.Video(operationContext, videoID)
			if err != nil {
				return err
			}
		}

		filename := recovery.filename(videoID)
		if filename == "" {
			filename = safeFilename(info.Title)
		}

		target := filepath.Join(directory, filename)
		if seen[target] {
			return fmt.Errorf(duplicatePlaylistOutputFormat, target)
		}

		seen[target] = true
		if !complete {
			if err := checkDestination(target, options.Overwrite); err != nil {
				return err
			}
		}

		if err := recovery.add(videoID, filename); err != nil {
			return err
		}

		infos = append(infos, info)
		targets = append(targets, target)
	}

	if err := recovery.save(); err != nil {
		return err
	}

	return service.downloadPlaylist(operationContext, infos, targets, options, recovery)
}

func (service *serviceImpl) downloadPlaylist(
	operationContext context.Context,
	infos []rutube.VideoInfo,
	targets []string,
	options Options,
	recovery *playlistRecovery,
) error {
	jobs := options.Jobs
	if jobs == 0 {
		jobs = DefaultJobs
	}

	jobs = min(jobs, len(infos))

	operationContext, cancel := context.WithCancelCause(operationContext)
	defer cancel(nil)

	display := progress.NewPlaylist(service.logger, len(infos), jobs)
	defer display.Close()

	var (
		workers    sync.WaitGroup
		scheduling sync.Mutex
	)

	next := 0

	for slot := range jobs {
		workers.Go(func() {
			for {
				scheduling.Lock()
				if operationContext.Err() != nil || next == len(infos) {
					scheduling.Unlock()
					return
				}

				index := next
				next++
				scheduling.Unlock()

				logger := display.Item(slot, index)

				var err error

				if recovery.isComplete(targets[index]) {
					logger.NewPrefix(filepath.Base(targets[index]))
				} else {
					err = service.download(operationContext, infos[index], targets[index], options.Overwrite, logger, recovery)
				}

				if err != nil {
					cancel(fmt.Errorf(playlistVideoErrorFormat, index+1, infos[index].Title, err))
				}

				display.Finish(slot, err)

				if err != nil {
					return
				}
			}
		})
	}

	workers.Wait()

	return context.Cause(operationContext)
}

func (service *serviceImpl) download(
	operationContext context.Context,
	info rutube.VideoInfo,
	target string,
	overwrite bool,
	logger progress.Logger,
	recovery *playlistRecovery,
) (resultError error) {
	operationContext, cancel := context.WithTimeout(operationContext, videoTimeout)
	defer cancel()

	logger.NewPrefix(filepath.Base(target))

	variants, err := service.streams.Variants(operationContext, info.MasterPlaylistLink)
	if err != nil {
		return err
	}

	best, err := hls.GetPlaylistWithBestResolution(variants)
	if err != nil {
		return err
	}

	artifact, err := service.streams.Download(operationContext, logger, best)
	if err != nil {
		return err
	}

	defer func() {
		resultError = errors.Join(resultError, artifact.Close())
		if resultError == nil {
			logger.UpdateWithSuffix(readyMessage)
			logger.PrintNewLine()
		}
	}()

	workspace, err := os.MkdirTemp(filepath.Dir(target), outputWorkspacePattern)
	if err != nil {
		return err
	}

	defer func() {
		resultError = errors.Join(resultError, os.RemoveAll(workspace))
	}()

	staging := filepath.Join(workspace, stagingFilename)
	if err := service.converter.Convert(operationContext, logger, artifact.Files(), staging); err != nil {
		return err
	}

	if operationContext.Err() != nil {
		return operationContext.Err()
	}

	infoOnDisk, err := os.Stat(staging)
	if err != nil {
		return err
	}

	if !infoOnDisk.Mode().IsRegular() || infoOnDisk.Size() == 0 {
		return errors.New(converterDidNotProduceANonemptyFileMessage)
	}

	if err := recovery.prepare(operationContext, target, staging); err != nil {
		return err
	}

	if overwrite {
		err = os.Rename(staging, target)
	} else {
		err = os.Link(staging, target)
	}

	if err != nil {
		return fmt.Errorf(cannotPublishVideoFormat, err)
	}

	return nil
}

func outputDirectory(directory string) (string, error) {
	if directory == "" {
		directory = defaultOutputDirectory
	}

	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(absolute, outputDirectoryPermissions); err != nil {
		return "", err
	}

	return filepath.EvalSymlinks(absolute)
}

func checkDestination(path string, overwrite bool) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	if info.IsDir() || !overwrite {
		return fmt.Errorf(outputAlreadyExistsFormat, path)
	}

	return nil
}

func safeFilename(title string) string {
	title = strings.ToValidUTF8(title, filenameReplacement)
	title = strings.Map(func(character rune) rune {
		if character == filenameSlash || character == filenameBackslash || unicode.IsControl(character) {
			return filenameReplacementRune
		}

		return character
	}, title)

	title = strings.Trim(title, trimmedFilenameCharacters)
	if title == "" {
		title = fallbackTitle
	}

	if strings.HasPrefix(title, leadingHyphen) {
		title = filenameReplacement + title
	}

	for len(title) > outputNameLimit {
		_, width := utf8.DecodeLastRuneInString(title)
		title = title[:len(title)-width]
	}

	return title + outputExtension
}
