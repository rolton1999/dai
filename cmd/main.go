package main

import (
	"dai/internal/run"
	"fmt"
	"os"
)

func main() {
	if err := run.Run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
