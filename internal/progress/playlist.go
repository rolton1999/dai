package progress

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode"
)

const (
	playlistSummaryFormat    = "Playlist: %d/%d completed | %d active | %d queued | %d failed"
	playlistItemFormat       = "[%d/%d] %s%s"
	playlistQueued           = " ~ queued"
	playlistReady            = ": READY"
	playlistStarting         = " ~ fetching playlist"
	playlistFailed           = ": FAILED"
	playlistCanceled         = ": CANCELED"
	playlistCursorUp         = "\033[%dA\r"
	playlistClear            = "\033[J"
	playlistNoWrap           = "\033[?7l"
	playlistWrap             = "\033[?7h"
	playlistTruncation       = "..."
	playlistNonASCIIWidth    = 2
	playlistASCIILimit       = 127
	playlistReservedRows     = 2
	playlistTitleReplacement = '_'
)

type Playlist interface {
	Item(slot int, index int) Logger
	Finish(slot int, err error)
	Close()
}

func NewPlaylist(logger Logger, total int, slots int) Playlist {
	rows := make([]playlistRow, total)
	for index := range rows {
		rows[index] = playlistRow{index: index, suffix: playlistQueued}
	}

	assignments := make([]int, slots)

	return &playlistDisplay{sink: logger, total: total, rows: rows, assignments: assignments}
}

type playlistDisplay struct {
	mutex       sync.Mutex
	sink        Logger
	total       int
	started     int
	completed   int
	failed      int
	active      int
	rendered    int
	rows        []playlistRow
	assignments []int
}

func (display *playlistDisplay) Item(slot int, index int) Logger {
	display.mutex.Lock()
	defer display.mutex.Unlock()

	display.rows[index] = playlistRow{index: index, suffix: playlistStarting, active: true}
	display.assignments[slot] = index
	display.started++
	display.active++

	return &playlistLogger{display: display, index: index}
}

func (display *playlistDisplay) Finish(slot int, err error) {
	display.mutex.Lock()
	defer display.mutex.Unlock()

	row := &display.rows[display.assignments[slot]]
	row.finished = true
	row.active = false

	display.active--
	if err == nil {
		display.completed++
		row.suffix = playlistReady
	} else {
		display.failed++

		row.suffix = playlistFailed
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			row.suffix = playlistCanceled
		}
	}

	display.flush(false)
	display.render()
}

func (display *playlistDisplay) Close() {
	display.mutex.Lock()
	defer display.mutex.Unlock()

	display.flush(true)

	if logger, ok := display.sink.(*loggerImpl); ok && logger.interactive {
		display.render()
	} else {
		display.writeLine(display.summary())
	}
}

func (display *playlistDisplay) flush(final bool) {
	if logger, ok := display.sink.(*loggerImpl); ok && logger.interactive {
		return
	}

	for index := range display.rows {
		row := &display.rows[index]
		if !row.finished {
			if !final {
				return
			}

			continue
		}

		if !row.printed {
			display.writeLine(display.line(*row))
			row.printed = true
		}
	}
}

func (display *playlistDisplay) render() {
	logger, ok := display.sink.(*loggerImpl)
	if !ok || !logger.interactive {
		return
	}

	width, height := logger.terminalSize()
	capacity := max(0, height-playlistReservedRows)
	visible := display.visibleRows(capacity)

	var frame strings.Builder
	if display.rendered > 0 {
		_, _ = fmt.Fprintf(&frame, playlistCursorUp, display.rendered)
	}

	frame.WriteString(playlistClear)
	frame.WriteString(playlistNoWrap)

	_, _ = fmt.Fprintln(&frame, truncatePlaylistLine(display.summary(), width))
	for _, row := range visible {
		_, _ = fmt.Fprintln(&frame, display.line(row))
	}

	frame.WriteString(playlistWrap)
	_, _ = fmt.Fprint(logger.output, frame.String())
	display.rendered = 1 + len(visible)
}

func (display *playlistDisplay) visibleRows(capacity int) []playlistRow {
	if len(display.rows) <= capacity {
		return display.rows
	}

	selected := make([]bool, len(display.rows))

	remaining := capacity
	for index, row := range display.rows {
		if row.active && remaining > 0 {
			selected[index] = true
			remaining--
		}
	}

	for index := len(display.rows) - 1; index >= 0 && remaining > 0; index-- {
		if display.rows[index].finished {
			selected[index] = true
			remaining--
		}
	}

	for index := range display.rows {
		if remaining > 0 && !selected[index] {
			selected[index] = true
			remaining--
		}
	}

	visible := make([]playlistRow, 0, capacity)

	for index, row := range display.rows {
		if selected[index] {
			visible = append(visible, row)
		}
	}

	return visible
}

func (display *playlistDisplay) summary() string {
	return fmt.Sprintf(playlistSummaryFormat, display.completed, display.total, display.active, display.total-display.started, display.failed)
}

func (display *playlistDisplay) line(row playlistRow) string {
	title := row.title

	if logger, ok := display.sink.(*loggerImpl); ok && logger.interactive {
		width, _ := logger.terminalSize()
		decoration := fmt.Sprintf(playlistItemFormat, row.index+1, display.total, "", row.suffix)
		title = truncatePlaylistLine(title, max(0, width-len(decoration)))
	}

	return fmt.Sprintf(playlistItemFormat, row.index+1, display.total, title, row.suffix)
}

func (display *playlistDisplay) writeLine(line string) {
	if logger, ok := display.sink.(*loggerImpl); ok {
		_, _ = fmt.Fprintln(logger.output, line)
		return
	}

	display.sink.NewPrefix(line)
	display.sink.UpdateWithSuffix("")
	display.sink.PrintNewLine()
}

type playlistRow struct {
	index    int
	title    string
	suffix   string
	active   bool
	finished bool
	printed  bool
}

type playlistLogger struct {
	display *playlistDisplay
	index   int
}

func (logger *playlistLogger) NewPrefix(prefix string) {
	logger.display.mutex.Lock()
	defer logger.display.mutex.Unlock()

	prefix = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return playlistTitleReplacement
		}

		return character
	}, prefix)
	logger.display.rows[logger.index].title = prefix
	logger.display.render()
}

func (logger *playlistLogger) UpdateWithSuffix(suffix string) {
	logger.display.mutex.Lock()
	defer logger.display.mutex.Unlock()

	logger.display.rows[logger.index].suffix = suffix
	logger.display.render()
}

func (*playlistLogger) PrintNewLine() {
}

func truncatePlaylistLine(line string, columns int) string {
	available := max(0, columns-len(playlistTruncation))
	used := 0
	ending := 0
	truncated := false

	for index, character := range line {
		width := 1
		if unicode.Is(unicode.Mn, character) {
			width = 0
		} else if character > playlistASCIILimit {
			width = playlistNonASCIIWidth
		}

		used += width
		if used <= available {
			ending = index + len(string(character))
		}

		if used > columns {
			truncated = true
			break
		}
	}

	if !truncated {
		return line
	}

	if columns < len(playlistTruncation) {
		return strings.Repeat(".", max(0, columns))
	}

	return line[:ending] + playlistTruncation
}
