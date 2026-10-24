package main

import (
	"dai/internal/cli"
	"fmt"
	"os"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
