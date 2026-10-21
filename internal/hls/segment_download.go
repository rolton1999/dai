package hls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

const (
	segmentDownloadAttempts = 4
	segmentAttemptTimeout   = 30 * time.Second
	segmentDownloadTimeout  = 2 * time.Minute
	segmentRetryDelay       = 250 * time.Millisecond
	segmentDownloadError    = "cannot download segment %d after %d attempts: %w"
	segmentStatusError      = "HTTP %d"
	segmentCreateError      = "cannot create segment file: %w"
	segmentWriteError       = "cannot write segment: %w"
	segmentCloseError       = "cannot close segment: %w"
)

func downloadSegment(
	operationContext context.Context,
	client *http.Client,
	meta segmentMeta,
	wait func(context.Context, time.Duration) error,
) error {
	for attempt := 0; attempt < segmentDownloadAttempts; attempt++ {
		if err := operationContext.Err(); err != nil {
			return fmt.Errorf(segmentDownloadError, meta.index+1, attempt, err)
		}

		attemptContext, cancel := context.WithTimeout(operationContext, segmentAttemptTimeout)
		retry, err := downloadSegmentAttempt(attemptContext, client, meta)

		cancel()

		if err == nil {
			return nil
		}

		if !retry || attempt+1 == segmentDownloadAttempts {
			return fmt.Errorf(segmentDownloadError, meta.index+1, attempt+1, err)
		}

		if err := wait(operationContext, segmentRetryDelay*time.Duration(1<<attempt)); err != nil {
			return fmt.Errorf(segmentDownloadError, meta.index+1, attempt+1, err)
		}
	}

	return nil
}

func downloadSegmentAttempt(operationContext context.Context, client *http.Client, meta segmentMeta) (bool, error) {
	request, err := http.NewRequestWithContext(operationContext, http.MethodGet, meta.link, nil)
	if err != nil {
		return false, err
	}

	response, err := client.Do(request)
	if err != nil {
		return retryableSegmentTransfer(err), err
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		retry := response.StatusCode == http.StatusRequestTimeout ||
			response.StatusCode == http.StatusTooManyRequests ||
			response.StatusCode == http.StatusInternalServerError ||
			response.StatusCode == http.StatusBadGateway ||
			response.StatusCode == http.StatusServiceUnavailable ||
			response.StatusCode == http.StatusGatewayTimeout

		return retry, fmt.Errorf(segmentStatusError, response.StatusCode)
	}

	file, err := os.Create(meta.fileName)
	if err != nil {
		return false, fmt.Errorf(segmentCreateError, err)
	}

	written, copyErr := io.Copy(file, response.Body)
	closeErr := file.Close()

	if copyErr == nil && response.ContentLength > 0 && written != response.ContentLength {
		copyErr = io.ErrUnexpectedEOF
	}

	if copyErr != nil {
		_ = os.Remove(meta.fileName)
		return retryableSegmentTransfer(copyErr), fmt.Errorf(segmentWriteError, copyErr)
	}

	if closeErr != nil {
		_ = os.Remove(meta.fileName)
		return false, fmt.Errorf(segmentCloseError, closeErr)
	}

	return false, nil
}

func retryableSegmentTransfer(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}

	var fileError *os.PathError
	if errors.As(err, &fileError) {
		return false
	}

	var networkError net.Error

	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) || errors.As(err, &networkError)
}

func waitForSegmentRetry(operationContext context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-operationContext.Done():
		return operationContext.Err()
	case <-timer.C:
		return nil
	}
}
