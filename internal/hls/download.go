package hls

import (
	"context"
	"dai/internal/http/client"
	"dai/internal/pipeline"
	"dai/internal/progress"
	"fmt"
	"os"
)

const (
	temporaryDirectoryErrorFormat = "cannot create tmp dir: %w"
	mediaPlaylistErrorFormat      = "cannot parse media playlist: %w"
	loadingProgressFormat         = " ~ loading %d%%"
	progressPercent               = 100
)

func DownloadTSFiles(logger progress.Logger, expectedName string, playlist VariantPlaylist) ([]string, error) {
	directoryName, err := os.MkdirTemp("", "")
	if err != nil {
		return nil, fmt.Errorf(temporaryDirectoryErrorFormat, err)
	}

	downloadSucceeded := false
	defer func() {
		if !downloadSucceeded {
			_ = os.RemoveAll(directoryName)
		}
	}()

	segmentMetas, err := collectSegmentMetas(playlist, directoryName, expectedName)
	if err != nil {
		return nil, fmt.Errorf(mediaPlaylistErrorFormat, err)
	}

	produceStage := newProduceStage(segmentMetas)

	httpClient := client.NewStreamingClient(nil)
	defer httpClient.Close()

	loadStage := newLoadStage(httpClient)
	consumeStage, listOfFiles := newConsumeStage(logger, len(segmentMetas))

	downloadPipeline := pipeline.NewPipeline()
	downloadPipeline.AppendProducer(produceStage)
	downloadPipeline.AppendMedium(loadStage)
	downloadPipeline.AppendFinalizer(consumeStage)

	if err := downloadPipeline.Run(context.Background()); err != nil {
		return nil, err
	}

	downloadSucceeded = true

	return listOfFiles, nil
}

type segmentMeta struct {
	index    int
	link     string
	fileName string
}

func newProduceStage(segmentMetas []segmentMeta) pipeline.ProduceStage {
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

func newLoadStage(httpClient client.StreamingClient) pipeline.ProcessStage {
	return pipeline.NewProcessStage(func(operationContext context.Context, meta segmentMeta) (segmentMeta, error) {
		operationContext, cancel := context.WithTimeout(operationContext, segmentDownloadTimeout)
		defer cancel()

		return meta, downloadSegment(operationContext, httpClient, meta, waitForSegmentRetry)
	})
}

func newConsumeStage(logger progress.Logger, segmentCount int) (pipeline.FinalizeStage, []string) {
	completedSegmentCount := 0
	listOfFiles := make([]string, segmentCount)

	return pipeline.NewFinalizeStage(func(_ context.Context, meta segmentMeta) error {
		listOfFiles[meta.index] = meta.fileName
		logger.UpdateWithSuffix(fmt.Sprintf(loadingProgressFormat, progressPercent*completedSegmentCount/len(listOfFiles)))

		completedSegmentCount += 1

		return nil
	}), listOfFiles
}
