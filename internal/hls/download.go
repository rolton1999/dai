package hls

import (
	"context"
	"dai/internal/http/client"
	"dai/internal/pipeline"
	"dai/internal/progress"
	"errors"
	"fmt"
	"os"
)

const (
	temporaryDirectoryErrorFormat = "cannot create tmp dir: %w"
	mediaPlaylistErrorFormat      = "cannot parse media playlist: %w"
	loadingProgressFormat         = " ~ loading %d%%"
	progressPercent               = 100
	segmentDirectoryPattern       = "dai-segments-*"
	segmentFilePrefix             = "segment"
)

// The caller owns a successful artifact and must close it after conversion.
type Artifact interface {
	Files() []string
	Duration() float64
	Close() error
}

func DownloadTSFiles(operationContext context.Context, logger progress.Logger, playlist VariantPlaylist) (result Artifact, resultError error) {
	httpClient := client.NewStreamingClient(nil)
	defer httpClient.Close()

	reader, err := client.New(httpClient)
	if err != nil {
		return nil, err
	}

	return downloadTSFiles(operationContext, logger, playlist, reader, httpClient)
}

func downloadTSFiles(
	operationContext context.Context,
	logger progress.Logger,
	playlist VariantPlaylist,
	reader client.Client,
	httpClient client.StreamingClient,
) (result Artifact, resultError error) {
	directoryName, err := os.MkdirTemp("", segmentDirectoryPattern)
	if err != nil {
		return nil, fmt.Errorf(temporaryDirectoryErrorFormat, err)
	}

	downloadSucceeded := false
	defer func() {
		if !downloadSucceeded {
			resultError = errors.Join(resultError, os.RemoveAll(directoryName))
		}
	}()

	segmentMetas, err := collectSegmentMetasWithReader(operationContext, reader, playlist, directoryName, segmentFilePrefix)
	if err != nil {
		return nil, fmt.Errorf(mediaPlaylistErrorFormat, err)
	}

	produceStage := newProduceStage(segmentMetas)

	loadStage := newLoadStage(httpClient)
	consumeStage, listOfFiles := newConsumeStage(logger, len(segmentMetas))

	downloadPipeline := pipeline.NewPipeline[segmentMeta]()
	downloadPipeline.AppendProducer(produceStage)
	downloadPipeline.AppendMedium(loadStage)
	downloadPipeline.AppendFinalizer(consumeStage)

	if err := downloadPipeline.Run(operationContext); err != nil {
		return nil, err
	}

	var duration float64
	for _, segment := range segmentMetas {
		duration += segment.duration
	}

	downloadSucceeded = true

	return &artifact{directory: directoryName, files: listOfFiles, duration: duration}, nil
}

type artifact struct {
	duration  float64
	directory string
	files     []string
}

func (download *artifact) Files() []string {
	return append([]string(nil), download.files...)
}

func (download *artifact) Duration() float64 {
	return download.duration
}

func (download *artifact) Close() error {
	return os.RemoveAll(download.directory)
}

type segmentMeta struct {
	duration float64
	index    int
	link     string
	fileName string
}

func newProduceStage(segmentMetas []segmentMeta) pipeline.ProduceStage[segmentMeta] {
	nextSegmentIndex := 0
	getNext := func(context.Context) (segmentMeta, error, bool) {
		if nextSegmentIndex == len(segmentMetas) {
			return segmentMeta{}, nil, true
		}

		meta := segmentMetas[nextSegmentIndex]
		nextSegmentIndex += 1

		return meta, nil, false
	}

	return pipeline.NewProduceStage(getNext)
}

func newLoadStage(httpClient client.StreamingClient) pipeline.ProcessStage[segmentMeta] {
	return pipeline.NewProcessStage(func(operationContext context.Context, meta segmentMeta) (segmentMeta, error) {
		operationContext, cancel := context.WithTimeout(operationContext, segmentDownloadTimeout)
		defer cancel()

		return meta, downloadSegment(operationContext, httpClient, meta, waitForSegmentRetry)
	})
}

func newConsumeStage(logger progress.Logger, segmentCount int) (pipeline.FinalizeStage[segmentMeta], []string) {
	completedSegmentCount := 0
	listOfFiles := make([]string, segmentCount)

	return pipeline.NewFinalizeStage(func(_ context.Context, meta segmentMeta) error {
		listOfFiles[meta.index] = meta.fileName
		completedSegmentCount++
		logger.UpdateWithSuffix(fmt.Sprintf(loadingProgressFormat, progressPercent*completedSegmentCount/len(listOfFiles)))

		return nil
	}), listOfFiles
}
