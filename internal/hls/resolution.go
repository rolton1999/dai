package hls

import "fmt"

type Resolution struct {
	w, h int
}

func (r Resolution) Less(other Resolution) bool {
	if r.w == other.w {
		return r.h < other.h
	}

	return r.w < other.w
}

func (r Resolution) String() string {
	const template = "%dx%d"
	return fmt.Sprintf(template, r.w, r.h)
}
