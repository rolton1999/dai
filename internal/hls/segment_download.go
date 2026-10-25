package hls

import (
	"context"
	"dai/internal/http/client"
	"fmt"
	"io"
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
	httpClient client.StreamingClient,
	meta segmentMeta,
	wait func(context.Context, time.Duration) error,
) error {
	for attempt := 0; attempt < segmentDownloadAttempts; attempt++ {
		if err := operationContext.Err(); err != nil {
			return fmt.Errorf(segmentDownloadError, meta.index+1, attempt, err)
		}

		attemptContext, cancel := context.WithTimeout(operationContext, segmentAttemptTimeout)
		retry, err := downloadSegmentAttempt(attemptContext, httpClient, meta)

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

func downloadSegmentAttempt(operationContext context.Context, httpClient client.StreamingClient, meta segmentMeta) (bool, error) {
	request, err := http.NewRequestWithContext(operationContext, http.MethodGet, meta.link, nil)
	if err != nil {
		return false, err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return client.RetryableError(err), err
	}

	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode != http.StatusOK {
		retry := client.RetryableStatus(response.StatusCode)

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
		return client.RetryableError(copyErr), fmt.Errorf(segmentWriteError, copyErr)
	}

	if closeErr != nil {
		_ = os.Remove(meta.fileName)
		return false, fmt.Errorf(segmentCloseError, closeErr)
	}

	return false, nil
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
