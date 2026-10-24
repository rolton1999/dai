package pipeline_test

import (
	"testing"

	"dai/internal/pipeline"
)

const (
	stageItemCount  = 32
	stageMultiplier = 2
)

func TestPipelineProcessesEveryItem(test *testing.T) {
	nextItem := 0
	processedItems := make(map[int]int)
	downloadPipeline := pipeline.NewPipeline()
	downloadPipeline.AppendProducer(pipeline.NewProduceStage(func() (int, error, bool) {
		if nextItem == stageItemCount {
			return 0, nil, true
		}

		item := nextItem
		nextItem++

		return item, nil, false
	}))
	downloadPipeline.AppendMedium(pipeline.NewProcessStage(func(item int) (int, error) {
		return item * stageMultiplier, nil
	}))
	downloadPipeline.AppendFinalizer(pipeline.NewFinalizeStage(func(item int) {
		processedItems[item]++
	}))
	downloadPipeline.Run()

	if len(processedItems) != stageItemCount {
		test.Fatalf("expected %d distinct items, got %d", stageItemCount, len(processedItems))
	}

	for item := range stageItemCount {
		if count := processedItems[item*stageMultiplier]; count != 1 {
			test.Errorf("expected item %d once, got %d occurrences", item, count)
		}
	}
}
