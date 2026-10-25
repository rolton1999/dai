package pipeline_test

import (
	"context"
	"testing"

	"dai/internal/pipeline"
)

const (
	stageItemCount  = 32
	stageMultiplier = 2
)

func TestPipelineProcessesEveryItem(test *testing.T) {
	operationContext, cancel := context.WithTimeout(context.Background(), pipelineTestDeadline)
	defer cancel()

	nextItem := 0
	processedItems := make(map[int]int)
	downloadPipeline := pipeline.NewPipeline()
	producer := pipeline.NewProduceStage(func(context.Context) (int, error, bool) {
		if nextItem == stageItemCount {
			return 0, nil, true
		}

		item := nextItem
		nextItem++

		return item, nil, false
	})
	processor := pipeline.NewProcessStage(func(_ context.Context, item int) (int, error) {
		return item * stageMultiplier, nil
	})
	finalizer := pipeline.NewFinalizeStage(func(_ context.Context, item int) error {
		processedItems[item]++

		return nil
	})

	downloadPipeline.AppendProducer(producer)
	downloadPipeline.AppendMedium(processor)
	downloadPipeline.AppendFinalizer(finalizer)

	if err := downloadPipeline.Run(operationContext); err != nil {
		test.Fatal(err)
	}

	if len(processedItems) != stageItemCount {
		test.Fatalf("expected %d distinct items, got %d", stageItemCount, len(processedItems))
	}

	for item := range stageItemCount {
		if count := processedItems[item*stageMultiplier]; count != 1 {
			test.Errorf("expected item %d once, got %d occurrences", item, count)
		}
	}
}
