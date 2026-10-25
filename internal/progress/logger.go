package progress

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

const (
	loggerLineFormat   = "%s%s"
	loggerPadding      = " "
	loggerUpdateFormat = "\r%s"
)

type Logger interface {
	NewPrefix(prefix string)
	UpdateWithSuffix(suffix string)
	PrintNewLine()
}

func NewLogger() Logger {
	interactive := term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv(terminalEnvironment) != plainTerminal

	return &loggerImpl{output: os.Stdout, interactive: interactive}
}

const (
	terminalEnvironment   = "TERM"
	plainTerminal         = "dumb"
	defaultTerminalWidth  = 80
	defaultTerminalHeight = 24
)

type loggerImpl struct {
	output            io.Writer
	interactive       bool
	longestLineLength int
	prefix            string
}

func (logger *loggerImpl) NewPrefix(prefix string) {
	logger.prefix = prefix
	logger.longestLineLength = 0
}

func (logger *loggerImpl) UpdateWithSuffix(suffix string) {
	line := fmt.Sprintf(loggerLineFormat, logger.prefix, suffix)

	logger.longestLineLength = max(logger.longestLineLength, len(line))
	for len(line) < logger.longestLineLength {
		line += loggerPadding
	}

	if logger.interactive {
		_, _ = fmt.Fprintf(logger.output, loggerUpdateFormat, line)
	} else {
		_, _ = fmt.Fprintln(logger.output, line)
	}
}

func (logger *loggerImpl) PrintNewLine() {
	_, _ = fmt.Fprintln(logger.output)
}

func (logger *loggerImpl) terminalSize() (int, int) {
	if output, ok := logger.output.(*os.File); ok {
		width, height, err := term.GetSize(int(output.Fd()))
		if err == nil && width > 0 && height > 0 {
			return width, height
		}
	}

	return defaultTerminalWidth, defaultTerminalHeight
}
