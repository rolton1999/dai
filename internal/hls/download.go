package hls

import (
	"context"
	"dai/internal/http/client"
	"dai/internal/pipeline"
	"dai/internal/progress"
	"fmt"
	"os"
)

func DownloadTSFiles(logger progress.Logger, expectedName string, playlist VariantPlaylist) ([]string, error) {
	dirName, err := os.MkdirTemp("", "")
	if err != nil {
		return nil, fmt.Errorf("cannot create tmp dir: %w", err)
	}

	downloadSucceeded := false
	defer func() {
		if !downloadSucceeded {
			_ = os.RemoveAll(dirName)
		}
	}()

	segmentMetas, err := collectSegmentMetas(playlist, dirName, expectedName)
	if err != nil {
		return nil, fmt.Errorf("cannot parse media playlist: %w", err)
	}

	produceStage := newProduceStage(segmentMetas)
	loadStage := newLoadStage()
	consumeStage, listOfFiles := newConsumeStage(logger, len(segmentMetas), &err)

	pipeline := pipeline.NewPipeline()
	pipeline.AppendProducer(produceStage)
	pipeline.AppendMedium(loadStage)
	pipeline.AppendFinalizer(consumeStage)
	pipeline.Run()

	if err != nil {
		return nil, err
	}

	downloadSucceeded = true

	return listOfFiles, nil
}

type segmentLoadResult struct {
	segmentIndex int
	fileName     string
	err          error
}

type segmentMeta struct {
	index    int
	link     string
	fileName string
}

func newProduceStage(segmentMetas []segmentMeta) pipeline.ProduceStage {
	segmentMetaProducedIdx := 0
	getNext := func() (segmentMeta, error, bool) {
		if segmentMetaProducedIdx == len(segmentMetas) {
			return segmentMeta{}, nil, true
		}

		meta := segmentMetas[segmentMetaProducedIdx]
		segmentMetaProducedIdx += 1

		return meta, nil, false
	}

	return pipeline.NewProduceStage(getNext)
}

func newLoadStage() pipeline.ProcessStage {
	network := client.NewDoer()

	return pipeline.NewProcessStage(func(meta segmentMeta) (segmentLoadResult, error) {
		operationContext, cancel := context.WithTimeout(context.Background(), segmentDownloadTimeout)
		defer cancel()

		err := downloadSegment(operationContext, network, meta, waitForSegmentRetry)
		if err != nil {
			return segmentLoadResult{segmentIndex: meta.index, err: err}, err
		}

		return segmentLoadResult{segmentIndex: meta.index, fileName: meta.fileName}, nil
	})
}

func newConsumeStage(logger progress.Logger, howMany int, err *error) (pipeline.FinalizeStage, []string) {
	loadMetaConsumedIdx := 0
	listOfFiles := make([]string, howMany)

	return pipeline.NewFinalizeStage(func(meta segmentLoadResult) {
		if meta.err != nil {
			*err = meta.err
			return
		}

		listOfFiles[meta.segmentIndex] = meta.fileName
		logger.UpdateWithSuffix(fmt.Sprintf(" ~ loading %d%%", 100*loadMetaConsumedIdx/len(listOfFiles)))

		loadMetaConsumedIdx += 1
	}), listOfFiles
}
