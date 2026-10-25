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

	reader, err := client.New(network)
	if err != nil {
		return err
	}

	provider, err := rutube.New(reader)
	if err != nil {
		return err
	}

	streams, err := hls.New(reader, network)
	if err != nil {
		return err
	}

	converter := ffmpeg.New(nil)
	logger := progress.NewLogger()

	service, err := download.New(provider, streams, converter, logger)
	if err != nil {
		return err
	}

	command, err := cli.New(service, os.Stdout)
	if err != nil {
		return err
	}

	return command.Run(operationContext, os.Args[1:])
}
