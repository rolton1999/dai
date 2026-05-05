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
func DownloadTSFilesToDir(logger util.Logger, dirName string, expectedName string, playlist VariantPlaylist) ([]string, error) {
	resp, err := http.Get(playlist.Link)
	if err != nil {
		return nil, fmt.Errorf("cannot get variantPlaylist link: %w", err)
	}

	const readWriteMode = 0777
	err = os.Mkdir(dirName, readWriteMode)
	if err != nil {
		return nil, fmt.Errorf("cannot create temp dir: %w", err)
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()

	linkPrefix := playlist.getPlayListLinkPrefix()

	split := bytes.Split(body, []byte{'\n'})

	expectedName = strings.ReplaceAll(expectedName, " ", "_")
	expectedName = strings.ReplaceAll(expectedName, "/", "")

	tmpFilePrefix := filepath.Join(dirName, expectedName)

	parLoader := newParLoader[result]()
	tasks := make(chan taskType[result], 10)

	resultCh := parLoader.start(10, tasks)
	stopLoading := make(chan struct{})

	howMany := (len(split) - 6 + 1) / 2 // see comment before the func

	listOfFiles := make([]string, 0, howMany)

	wg := sync.WaitGroup{}

	wg.Go(func() {
		for i := 1; ; i++ {
			result, ok := <-resultCh
			if !ok {
				return
			}
			if result.err != nil {
				close(stopLoading)
				err = result.err
				return
			}
			listOfFiles = append(listOfFiles, result.fileName)
			logger.UpdateWithSuffix(fmt.Sprintf(" ~ loading %d%%", 100*i/howMany))
		}
	})

	wg.Go(func() {
		defer close(tasks)
		for segmentIndex, i := 0, 7; i < len(split); segmentIndex, i = segmentIndex+1, i+2 {
			task := func() result {
				segment := string(split[i])
				link := linkPrefix + "/" + segment

				resp, err := http.Get(link)
				if err != nil {
					return result{"", fmt.Errorf("cannot get first segment link: %w", err)}
				}

				fileName := tmpFilePrefix + "_" + strconv.Itoa(segmentIndex) + ".ts"

				file, err := os.Create(fileName)
				if err != nil {
					return result{"", fmt.Errorf("cannot create file for ts-data: %w", err)}
				}

				io.Copy(file, resp.Body)
				_ = file.Close()

				return result{fileName, nil}
			}

			select {
			case tasks <- task:
			case <-stopLoading:
				return
			}
		}
	})

	wg.Wait()
	parLoader.stop()

	if err != nil {
		return nil, err
	}

	return listOfFiles, nil
}

type result struct {
	fileName string
	err      error
}
