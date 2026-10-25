package cli

import (
	"context"
	"errors"
	"flag"
	"io"

	"dai/internal/download"
)

const (
	usedWithoutArgsEnterSomeLinkMessage = "used without args, enter some link"
	urlShouldNotBeEmptyMessage          = "url should not be empty"
	downloadServiceIsRequiredMessage    = "download service is required"
	expectedExactlyOneURLMessage        = "expected exactly one URL"
	invalidJobsMessage                  = "jobs must be greater than zero"
	resumeOnlyPlaylistMessage           = "resume is supported only for playlists"
	resumeOverwriteMessage              = "resume cannot be combined with overwrite"
)

const (
	commandName   = "dai"
	outputFlag    = "output"
	overwriteFlag = "overwrite"
	jobsFlag      = "jobs"
	resumeFlag    = "resume"
	resumeHelp    = "resume a playlist using recorded completed outputs"
	jobsHelp      = "maximum concurrent playlist videos"
	outputHelp    = "directory for downloaded videos"
	overwriteHelp = "replace existing output files"
)

const (
	defaultOutputDirectory = "."
)

var (
	errWithoutArgs = errors.New(usedWithoutArgsEnterSomeLinkMessage)
	errEmptyLink   = errors.New(urlShouldNotBeEmptyMessage)
)

type Command interface {
	Run(context.Context, []string) error
}

func New(service download.Service) (Command, error) {
	if service == nil {
		return nil, errors.New(downloadServiceIsRequiredMessage)
	}

	return &commandImpl{service: service}, nil
}

type commandImpl struct {
	service download.Service
}

func (command *commandImpl) Run(operationContext context.Context, arguments []string) error {
	flags := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	directory := flags.String(outputFlag, defaultOutputDirectory, outputHelp)

	jobs := flags.Int(jobsFlag, download.DefaultJobs, jobsHelp)

	resume := flags.Bool(resumeFlag, false, resumeHelp)

	overwrite := flags.Bool(overwriteFlag, false, overwriteHelp)
	if err := flags.Parse(arguments); err != nil {
		return err
	}

	if *jobs <= 0 {
		return errors.New(invalidJobsMessage)
	}

	if flags.NArg() == 0 {
		return errWithoutArgs
	}

	if flags.NArg() != 1 {
		return errors.New(expectedExactlyOneURLMessage)
	}

	link := flags.Arg(0)
	if link == "" {
		return errEmptyLink
	}

	kind, contentID, err := parseLink(link)
	if err != nil {
		return err
	}

	if *resume && *overwrite {
		return errors.New(resumeOverwriteMessage)
	}

	if *resume && kind != playlist {
		return errors.New(resumeOnlyPlaylistMessage)
	}

	options := download.Options{Resume: *resume, Jobs: *jobs, OutputDirectory: *directory, Overwrite: *overwrite}
	if kind == singleVideo {
		return command.service.Video(operationContext, contentID, options)
	}

	return command.service.Playlist(operationContext, contentID, options)
}
