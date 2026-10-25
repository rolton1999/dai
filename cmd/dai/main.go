package main

import (
	"context"
	"dai/internal/cli"
	"dai/internal/download"
	"dai/internal/ffmpeg"
	"dai/internal/hls"
	"dai/internal/http/client"
	"dai/internal/progress"
	"dai/internal/rutube"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	operationContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(operationContext)

	stop()

	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)

		os.Exit(1)
	}
}

func run(operationContext context.Context) error {
	network := client.NewStreamingClient(nil)
	defer network.Close()

	reader := client.New(network)
	provider := rutube.New(reader)
	streams := hls.New(reader, network)
	converter := ffmpeg.New(nil)
	logger := progress.NewLogger()

	service, err := download.New(provider, streams, converter, logger)
	if err != nil {
		return err
	}

	command, err := cli.New(service)
	if err != nil {
		return err
	}

	return command.Run(operationContext, os.Args[1:])
}
