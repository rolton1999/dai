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
	defaultOutputDirectory     = "."
	outputExtension            = ".mp4"
	outputNameLimit            = 200
	videoTimeout               = 30 * time.Minute
	outputWorkspacePattern     = ".dai-output-*"
	outputPlanWorkspacePattern = ".dai-plan-*"
	outputPlanFilePermissions  = 0o600
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

	job := downloadJob{videoID: videoID, info: info, target: target}

	return service.download(operationContext, job, options.Overwrite, service.logger, nil)
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

	jobs := make([]downloadJob, 0, len(playlist.VideoIDs))
	seen := make(map[string]bool)

	for _, videoID := range playlist.VideoIDs {
		complete := false
		filename := ""

		if recovery != nil {
			complete, err = recovery.inspect(operationContext, videoID)
			if err != nil {
				return err
			}

			filename = recovery.filename(videoID)
		}

		var info rutube.VideoInfo
		if !complete {
			info, err = service.provider.Video(operationContext, videoID)
			if err != nil {
				return err
			}
		}

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

		if recovery != nil {
			if err := recovery.add(videoID, filename); err != nil {
				return err
			}
		}

		jobs = append(jobs, downloadJob{videoID: videoID, info: info, target: target, complete: complete})
	}

	if err := checkPlaylistFilenames(operationContext, directory, jobs); err != nil {
		return err
	}

	if recovery != nil {
		if err := recovery.save(); err != nil {
			return err
		}
	}

	return service.downloadPlaylist(operationContext, jobs, options, recovery)
}

func (service *serviceImpl) downloadPlaylist(
	operationContext context.Context,
	jobs []downloadJob,
	options Options,
	recovery *playlistRecovery,
) error {
	workerCount := options.Jobs
	if workerCount == 0 {
		workerCount = DefaultJobs
	}

	workerCount = min(workerCount, len(jobs))

	operationContext, cancel := context.WithCancelCause(operationContext)
	defer cancel(nil)

	display := progress.NewPlaylist(service.logger, len(jobs), workerCount)
	defer display.Close()

	var (
		workers    sync.WaitGroup
		scheduling sync.Mutex
	)

	next := 0

	for slot := range workerCount {
		workers.Go(func() {
			for {
				scheduling.Lock()
				if operationContext.Err() != nil || next == len(jobs) {
					scheduling.Unlock()
					return
				}

				index := next
				next++
				scheduling.Unlock()

				job := jobs[index]
				logger := display.Item(slot, index)

				var err error

				if job.complete {
					logger.NewPrefix(filepath.Base(job.target))
				} else {
					err = service.download(operationContext, job, options.Overwrite, logger, recovery)
				}

				if err != nil {
					cancel(fmt.Errorf(playlistVideoErrorFormat, index+1, job.info.Title, err))
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
	job downloadJob,
	overwrite bool,
	logger progress.Logger,
	recovery *playlistRecovery,
) (resultError error) {
	operationContext, cancel := context.WithTimeout(operationContext, videoTimeout)
	defer cancel()

	logger.NewPrefix(filepath.Base(job.target))

	variants, err := service.streams.Variants(operationContext, job.info.MasterPlaylistLink)
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

	workspace, err := os.MkdirTemp(filepath.Dir(job.target), outputWorkspacePattern)
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

	if recovery != nil {
		if err := recovery.prepare(operationContext, job.videoID, staging); err != nil {
			return err
		}
	}

	if overwrite {
		err = os.Rename(staging, job.target)
	} else {
		err = os.Link(staging, job.target)
	}

	if err != nil {
		return fmt.Errorf(cannotPublishVideoFormat, err)
	}

	return nil
}

type downloadJob struct {
	videoID  string
	info     rutube.VideoInfo
	target   string
	complete bool
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

func checkPlaylistFilenames(operationContext context.Context, directory string, jobs []downloadJob) (resultError error) {
	workspace, err := os.MkdirTemp(directory, outputPlanWorkspacePattern)
	if err != nil {
		return err
	}

	defer func() {
		resultError = errors.Join(resultError, os.RemoveAll(workspace))
	}()

	// Exclusive creation detects aliases using the destination filesystem's
	// case and Unicode rules without touching existing output files.
	for _, job := range jobs {
		target := job.target

		if err := operationContext.Err(); err != nil {
			return err
		}

		probe := filepath.Join(workspace, filepath.Base(target))

		file, err := os.OpenFile(probe, os.O_CREATE|os.O_EXCL|os.O_WRONLY, outputPlanFilePermissions)
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf(duplicatePlaylistOutputFormat, target)
		}

		if err != nil {
			return err
		}

		if err := file.Close(); err != nil {
			return err
		}
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
