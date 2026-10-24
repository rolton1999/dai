package hls

import "fmt"

const (
	resolutionDisplayFormat = "%dx%d"
)

type Resolution struct {
	width  int
	height int
}

func (resolution Resolution) Less(other Resolution) bool {
	return int64(resolution.width)*int64(resolution.height) < int64(other.width)*int64(other.height)
}

func (resolution Resolution) String() string {
	return fmt.Sprintf(resolutionDisplayFormat, resolution.width, resolution.height)
}
