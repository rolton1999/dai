package util

import "fmt"

type Logger interface {
	NewPrefix(prefix string)
	UpdateWithSuffix(suffix string)
	PrintNewLine()
}

func NewLogger() Logger {
	return &loggerImpl{}
}

type loggerImpl struct {
	longestLineLen int
	prefix         string
}

func (l *loggerImpl) NewPrefix(prefix string) {
	l.prefix = prefix
	l.longestLineLen = 0
}

func (l *loggerImpl) UpdateWithSuffix(suffix string) {
	s := fmt.Sprintf("%s%s", l.prefix, suffix)
	l.longestLineLen = max(l.longestLineLen, len(s))
	for len(s) < l.longestLineLen {
		s += " "
	}
	fmt.Printf("\r%s", s)
}

func (l *loggerImpl) PrintNewLine() {
	fmt.Println()
}
