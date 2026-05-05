package m3u

import (
	"bytes"
	"dai/internal/util"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// #EXTM3U
// #EXT-X-TARGETDURATION:7
// #EXT-X-ALLOW-CACHE:YES
// #EXT-X-PLAYLIST-TYPE:VOD
// #EXT-X-VERSION:3
// #EXT-X-MEDIA-SEQUENCE:1
// #EXTINF:3.937,
// e742a021efe5453586f12d4d08a836fe.mp4/segment-1-v1-a1.ts <- 7th line
// #EXTINF:0.133,
// e742a021efe5453586f12d4d08a836fe.mp4/segment-2-v1-a1.ts
// #EXTINF:6.607,
// e742a021efe5453586f12d4d08a836fe.mp4/segment-3-v1-a1.ts
// #EXTINF:5.339,
// ....
// e742a021efe5453586f12d4d08a836fe.mp4/segment-1157-v1-a1.ts
// #EXT-X-ENDLIST
func DownloadTSFiles(logger util.Logger, expectedName string, playlist VariantPlaylist) ([]string, error) {
	dirName, err := os.MkdirTemp("", "")
	if err != nil {
		return nil, fmt.Errorf("cannot create tmp dir: %w", err)
	}

	resp, err := http.Get(playlist.Link)
	if err != nil {
		return nil, fmt.Errorf("cannot get variantPlaylist link: %w", err)
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()

	linkPrefix := playlist.getPlayListLinkPrefix()

	split := bytes.Split(body, []byte{'\n'})

	expectedName = strings.ReplaceAll(expectedName, " ", "_")
	expectedName = strings.ReplaceAll(expectedName, "/", "")

	tmpFilePrefix := filepath.Join(dirName, expectedName)

	const (
		workerPoolQueueSize = 10
		workers             = 10
	)

	workerPool := util.NewWorkerPool[result](workerPoolQueueSize)

	resultCh := workerPool.Start(workers)
	var stopLoading atomic.Bool

	howMany := (len(split) - 6 + 1) / 2 // see comment before the func

	listOfFiles := make([]string, howMany)

	wg := sync.WaitGroup{}

	// producer
	wg.Go(func() {
		defer workerPool.DrainAndStop()

		for segmentIndex, i := 0, 7; i < len(split); segmentIndex, i = segmentIndex+1, i+2 {
			segment := string(split[i])
			link := linkPrefix + "/" + segment
			fileName := tmpFilePrefix + "_" + strconv.Itoa(segmentIndex) + ".ts"

			task := func() result {
				resp, err := http.Get(link)
				if err != nil {
					return result{0, "", fmt.Errorf("cannot get first segment link: %w", err)}
				}

				file, err := os.Create(fileName)
				if err != nil {
					return result{0, "", fmt.Errorf("cannot create file for ts-data: %w", err)}
				}

				io.Copy(file, resp.Body)
				_ = file.Close()

				return result{segmentIndex, fileName, nil}
			}

			// in case of any error in forward pipeline steps
			if stopLoading.Load() {
				return
			}

			workerPool.Submit(task)
		}
	})

	// consumer
	wg.Go(func() {
		defer stopLoading.Store(true)

		for i := 1; ; i++ {
			result, ok := <-resultCh
			if !ok {
				return
			}
			if result.err != nil {
				err = result.err
				return
			}
			listOfFiles[result.segmentIndex] = result.fileName
			logger.UpdateWithSuffix(fmt.Sprintf(" ~ loading %d%%", 100*i/howMany))
		}
	})

	wg.Wait()
	workerPool.Stop()

	if err != nil {
		return nil, err
	}

	return listOfFiles, nil
}

type result struct {
	segmentIndex int
	fileName     string
	err          error
}
