package hls

import (
	"dai/internal/pipeline"
	"dai/internal/progress"
	"fmt"
	"io"
	"net/http"
	"os"
)

func DownloadTSFiles(logger progress.Logger, expectedName string, playlist VariantPlaylist) ([]string, error) {
	dirName, err := os.MkdirTemp("", "")
	if err != nil {
		return nil, fmt.Errorf("cannot create tmp dir: %w", err)
	}

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
	return pipeline.NewProcessStage(func(meta segmentMeta) (segmentLoadResult, error) {
		resp, err := http.Get(meta.link)
		if err != nil {
			return segmentLoadResult{0, "", fmt.Errorf("cannot get first segment link: %w", err)}, err
		}

		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			err := fmt.Errorf("cannot download segment: HTTP %d", resp.StatusCode)
			return segmentLoadResult{meta.index, "", err}, err
		}

		file, err := os.Create(meta.fileName)
		if err != nil {
			return segmentLoadResult{0, "", fmt.Errorf("cannot create file for ts-data: %w", err)}, err
		}

		_, copyErr := io.Copy(file, resp.Body)
		closeErr := file.Close()

		if copyErr != nil {
			err := fmt.Errorf("cannot write segment: %w", copyErr)
			return segmentLoadResult{meta.index, "", err}, err
		}

		if closeErr != nil {
			err := fmt.Errorf("cannot close segment: %w", closeErr)
			return segmentLoadResult{meta.index, "", err}, err
		}

		return segmentLoadResult{meta.index, meta.fileName, nil}, nil
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
