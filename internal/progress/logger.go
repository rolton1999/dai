package progress

import "fmt"

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
	return &loggerImpl{}
}

type loggerImpl struct {
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

	fmt.Printf(loggerUpdateFormat, line)
}

func (logger *loggerImpl) PrintNewLine() {
	fmt.Println()
}
