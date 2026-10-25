package progress

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

const (
	displayTestSlots              = 4
	displayTestTotal              = 16
	displayTestFirstTitle         = "First video.mp4"
	displayTestSecondTitle        = "Second video.mp4"
	displayTestReplacementTitle   = "Next video.mp4"
	displayTestFirstProgress      = " ~ loading 64%"
	displayTestSecondProgress     = " ~ converting /"
	displayTestReady              = ": READY"
	displayTestReadyFirst         = "[1/16] First video.mp4: READY"
	displayTestActiveFirst        = "[1/16] First video.mp4 ~ loading 64%"
	displayTestActiveSecond       = "[2/16] Second video.mp4 ~ converting /"
	displayTestActiveReplacement  = "[3/16] Next video.mp4 ~ fetching playlist"
	displayTestSummaryActive      = "Playlist: 0/16 completed | 2 active | 14 queued | 0 failed"
	displayTestSummaryReplacement = "Playlist: 1/16 completed | 2 active | 13 queued | 0 failed"
	displayTestSummaryFinal       = "Playlist: 1/16 completed | 0 active | 13 queued | 2 failed"
	displayTestMissing            = "missing %q in progress output: %q"
	displayTestControlSequence    = "\033"
	displayTestUnexpectedANSI     = "redirected output contains terminal controls: %q"
	displayTestReadyCount         = "completion appeared %d times: %q"
	displayTestFailure            = "fixture failure"
	displayTestTitleFormat        = "Video %d.mp4"
	displayTestReadyFormat        = "[%d/4] Video %d.mp4: READY"
	displayTestLongTitle          = "界"
	displayTestLongRepeat         = 100
	displayTestLongStatus         = " ~ loading 99%"
	displayTestLongStatusMissing  = "long title hid progress or split UTF-8: %q"
)

func requireDisplayText(test *testing.T, output string, expected string) {
	test.Helper()

	if !strings.Contains(output, expected) {
		test.Fatalf(displayTestMissing, expected, output)
	}
}

func TestPlaylistDisplayKeepsRowsIndependentAndReusesSlots(test *testing.T) {
	var output bytes.Buffer

	logger := &loggerImpl{output: &output, interactive: true}
	display := NewPlaylist(logger, displayTestTotal, displayTestSlots)
	first := display.Item(0, 0)
	first.NewPrefix(displayTestFirstTitle)
	first.UpdateWithSuffix(displayTestFirstProgress)

	second := display.Item(1, 1)
	second.NewPrefix(displayTestSecondTitle)
	second.UpdateWithSuffix(displayTestSecondProgress)

	frame := output.String()
	requireDisplayText(test, frame, displayTestSummaryActive)
	requireDisplayText(test, frame, displayTestActiveFirst)
	requireDisplayText(test, frame, displayTestActiveSecond)
	output.Reset()
	first.UpdateWithSuffix(displayTestReady)
	display.Finish(0, nil)
	replacement := display.Item(0, 2)
	replacement.NewPrefix(displayTestReplacementTitle)

	frame = output.String()
	requireDisplayText(test, frame, displayTestReadyFirst)
	requireDisplayText(test, frame, displayTestActiveSecond)
	requireDisplayText(test, frame, displayTestActiveReplacement)
	requireDisplayText(test, frame, displayTestSummaryReplacement)
	output.Reset()

	failure := errors.New(displayTestFailure)
	display.Finish(0, failure)
	display.Finish(1, context.Canceled)
	display.Close()

	frame = output.String()
	requireDisplayText(test, frame, playlistFailed)
	requireDisplayText(test, frame, playlistCanceled)
	requireDisplayText(test, frame, displayTestSummaryFinal)
}

func TestRedirectedPlaylistProgressIsPlainAndConcurrent(test *testing.T) {
	var output bytes.Buffer

	logger := &loggerImpl{output: &output}
	display := NewPlaylist(logger, displayTestSlots, displayTestSlots)

	var workers sync.WaitGroup
	for slot := range displayTestSlots {
		workers.Go(func() {
			item := display.Item(slot, slot)
			item.NewPrefix(fmt.Sprintf(displayTestTitleFormat, slot))
			item.UpdateWithSuffix(displayTestFirstProgress)
			item.UpdateWithSuffix(displayTestReady)
			display.Finish(slot, nil)
		})
	}

	workers.Wait()
	display.Close()

	text := output.String()
	if strings.Contains(text, displayTestControlSequence) {
		test.Fatalf(displayTestUnexpectedANSI, text)
	}

	for slot := range displayTestSlots {
		expected := fmt.Sprintf(displayTestReadyFormat, slot+1, slot)
		if count := strings.Count(text, expected); count != 1 {
			test.Fatalf(displayTestReadyCount, count, text)
		}
	}
}

func TestLongPlaylistTitleKeepsStatusVisible(test *testing.T) {
	var output bytes.Buffer

	logger := &loggerImpl{output: &output, interactive: true}
	display := NewPlaylist(logger, 1, 1)
	item := display.Item(0, 0)
	item.NewPrefix(strings.Repeat(displayTestLongTitle, displayTestLongRepeat))
	item.UpdateWithSuffix(displayTestLongStatus)

	text := output.String()
	if !strings.Contains(text, playlistTruncation+displayTestLongStatus) || strings.ContainsRune(text, '\ufffd') {
		test.Fatalf(displayTestLongStatusMissing, text)
	}

	display.Finish(0, nil)
	display.Close()
}

const (
	orderedDisplayTotal           = 18
	orderedDisplaySlots           = 4
	orderedDisplayTitle           = "video.mp4"
	orderedDisplayRowFormat       = "[%d/18]"
	orderedDisplayExpectedLines   = 19
	orderedDisplayMismatch        = "frame %d: expected a header and 18 ordered rows, got %q"
	orderedDisplayCursorMismatch  = "frame %d: incorrect cursor movement: %q"
	orderedDisplayCompletionOrder = "completion order changed playlist numbering: %q"
	orderedDisplaySummaryPrefix   = "Playlist:"
	orderedDisplayNewline         = "\n"
	orderedDisplayEmpty           = ""
)

type frameWriter struct {
	frames []string
}

func (writer *frameWriter) Write(contents []byte) (int, error) {
	writer.frames = append(writer.frames, string(contents))
	return len(contents), nil
}

func TestPlaylistFramesKeepHeaderAndAllRowsInPlaylistOrder(test *testing.T) {
	writer := &frameWriter{}
	logger := &loggerImpl{output: writer, interactive: true}

	display := NewPlaylist(logger, orderedDisplayTotal, orderedDisplaySlots)
	for slot := range orderedDisplaySlots {
		item := display.Item(slot, slot)
		item.NewPrefix(orderedDisplayTitle)
		item.UpdateWithSuffix(displayTestFirstProgress)
	}

	for _, slot := range []int{1, 0, 3, 2} {
		display.Finish(slot, nil)
		item := display.Item(slot, slot+orderedDisplaySlots)
		item.NewPrefix(orderedDisplayTitle)
	}

	display.Close()

	cursor := fmt.Sprintf(playlistCursorUp, orderedDisplayExpectedLines)
	cleaner := strings.NewReplacer(playlistClear, orderedDisplayEmpty, playlistNoWrap, orderedDisplayEmpty, playlistWrap, orderedDisplayEmpty)

	for index, frame := range writer.frames {
		if index > 0 {
			if !strings.HasPrefix(frame, cursor) {
				test.Fatalf(orderedDisplayCursorMismatch, index, frame)
			}

			frame = strings.TrimPrefix(frame, cursor)
		}

		text := cleaner.Replace(frame)

		lines := strings.Split(strings.TrimSuffix(text, orderedDisplayNewline), orderedDisplayNewline)
		if len(lines) != orderedDisplayExpectedLines || !strings.HasPrefix(lines[0], orderedDisplaySummaryPrefix) {
			test.Fatalf(orderedDisplayMismatch, index, text)
		}

		for row := range orderedDisplayTotal {
			if !strings.HasPrefix(lines[row+1], fmt.Sprintf(orderedDisplayRowFormat, row+1)) {
				test.Fatalf(orderedDisplayMismatch, index, text)
			}
		}
	}
}

func TestRedirectedCompletionsFollowPlaylistOrder(test *testing.T) {
	var output bytes.Buffer

	logger := &loggerImpl{output: &output}

	display := NewPlaylist(logger, orderedDisplaySlots, orderedDisplaySlots)
	for slot := range orderedDisplaySlots {
		item := display.Item(slot, slot)
		item.NewPrefix(fmt.Sprintf(displayTestTitleFormat, slot))
	}

	for _, slot := range []int{1, 0, 3, 2} {
		display.Finish(slot, nil)
	}

	display.Close()

	text := output.String()
	previous := -1

	for slot := range orderedDisplaySlots {
		expected := fmt.Sprintf(displayTestReadyFormat, slot+1, slot)

		position := strings.Index(text, expected)
		if position <= previous {
			test.Fatalf(orderedDisplayCompletionOrder, text)
		}

		previous = position
	}
}

const (
	compactDisplayTotal        = 48
	compactDisplayNextIndex    = 30
	compactDisplayFirstSlot    = 0
	compactDisplayReusableSlot = 1
	compactDisplayRowFormat    = "[%d/48]"
	compactDisplayActiveFormat = "[%d/48] video.mp4 ~ fetching playlist"
	compactDisplaySummary      = "Playlist: 29/48 completed | 4 active | 15 queued | 0 failed"
	compactDisplayOrderError   = "compact panel is out of order: %q"
)

func TestLimitedPanelKeepsEveryActiveVideoVisibleInOrder(test *testing.T) {
	writer := &frameWriter{}
	logger := &loggerImpl{output: writer, interactive: true}
	display := NewPlaylist(logger, compactDisplayTotal, orderedDisplaySlots)
	first := display.Item(compactDisplayFirstSlot, 0)
	first.NewPrefix(orderedDisplayTitle)

	for index := 1; index < compactDisplayNextIndex; index++ {
		item := display.Item(compactDisplayReusableSlot, index)
		item.NewPrefix(orderedDisplayTitle)
		display.Finish(compactDisplayReusableSlot, nil)
	}

	for slot := 1; slot < orderedDisplaySlots; slot++ {
		item := display.Item(slot, compactDisplayNextIndex+slot-1)
		item.NewPrefix(orderedDisplayTitle)
	}

	frame := writer.frames[len(writer.frames)-1]
	frame = frame[strings.Index(frame, playlistClear):]
	cleaner := strings.NewReplacer(playlistClear, orderedDisplayEmpty, playlistNoWrap, orderedDisplayEmpty, playlistWrap, orderedDisplayEmpty)
	text := cleaner.Replace(frame)
	requireDisplayText(test, text, compactDisplaySummary)
	requireDisplayText(test, text, fmt.Sprintf(compactDisplayActiveFormat, 1))

	for slot := 1; slot < orderedDisplaySlots; slot++ {
		requireDisplayText(test, text, fmt.Sprintf(compactDisplayActiveFormat, compactDisplayNextIndex+slot))
	}

	previous := 0

	lines := strings.Split(strings.TrimSuffix(text, orderedDisplayNewline), orderedDisplayNewline)
	for _, line := range lines[1:] {
		var index int
		if _, err := fmt.Sscanf(line, compactDisplayRowFormat, &index); err != nil || index <= previous {
			test.Fatalf(compactDisplayOrderError, text)
		}

		previous = index
	}

	display.Close()
}
