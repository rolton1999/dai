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

func DownloadTSFiles(logger util.Logger, expectedName string, playlist VariantPlaylist) ([]string, error) {
	dirName, err := os.MkdirTemp("", "")
	if err != nil {
		return nil, fmt.Errorf("cannot create tmp dir: %w", err)
	}

	segmentMetas, err := collectSegmentMetas(playlist, dirName, expectedName)
	if err != nil {
		return nil, fmt.Errorf("cannot parse master playlist: %w", err)
	}

	const (
		workerPoolQueueSize = 10
		workers             = 10
	)

	workerPool := util.NewWorkerPool[segmentLoadMeta](workerPoolQueueSize)
	resultCh := workerPool.Start(workers)

	var stopPipeline atomic.Bool

	listOfFiles := make([]string, len(segmentMetas))

	wg := sync.WaitGroup{}

	// produceState := util.NewPipelineStage(func(_ struct{}) (util.TaskType[segmentLoadMeta], bool) {
	// 	i := 0
	// 	getNext := func() (util.TaskType[segmentLoadMeta], bool) {
	// 		if i == len(segmentMetas) {
	// 			return nil, true
	// 		}
	// 		task := getLoadTaskFromMeta(segmentMetas[i])
	// 		i += 1
	// 		return task, false
	// 	}
	// 	return getNext()
	// })
	// _ = produceState
	// loadStage := util.NewPipelineStage(func(task util.TaskType[segmentLoadMeta]) (struct{}, bool) {
	// 	workerPool.Submit(task)
	// 	return struct{}{}, false
	// })
	// _ = loadStage
	//produceStage :=
	//loadStage :=
	//consumeStage :=

	// producer
	wg.Go(func() {
		defer workerPool.DrainAndStop()

		for _, taskMeta := range segmentMetas {
			task := getLoadTaskFromMeta(taskMeta)

			// in case of any error in forward pipeline steps
			if stopPipeline.Load() {
				return
			}

			workerPool.Submit(task)
		}
	})

	// consumer
	wg.Go(func() {
		defer stopPipeline.Store(true)

		for i := 1; ; i++ {
			resultI, ok := <-resultCh
			if !ok {
				return
			}
			result := resultI.(segmentLoadMeta)
			if result.err != nil {
				err = result.err
				return
			}
			listOfFiles[result.segmentIndex] = result.fileName
			logger.UpdateWithSuffix(fmt.Sprintf(" ~ loading %d%%", 100*i/len(listOfFiles)))
		}
	})

	wg.Wait()
	workerPool.Stop()

	if err != nil {
		return nil, err
	}

	return listOfFiles, nil
}

type segmentLoadMeta struct {
	segmentIndex int
	fileName     string
	err          error
}

type segmentMeta struct {
	index    int
	link     string
	fileName string
}

func getLoadTaskFromMeta(taskMeta segmentMeta) util.TaskType[segmentLoadMeta] {
	return func() segmentLoadMeta {
		resp, err := http.Get(taskMeta.link)
		if err != nil {
			return segmentLoadMeta{0, "", fmt.Errorf("cannot get first segment link: %w", err)}
		}

		file, err := os.Create(taskMeta.fileName)
		if err != nil {
			return segmentLoadMeta{0, "", fmt.Errorf("cannot create file for ts-data: %w", err)}
		}

		io.Copy(file, resp.Body)
		_ = file.Close()

		return segmentLoadMeta{taskMeta.index, taskMeta.fileName, nil}
	}
}

/*
What we get by playlist.link

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
*/

// Yes, it can be done without slice of structs but this is not the hottest place and code is much simpler
func collectSegmentMetas(playlist VariantPlaylist, dirName, concatenatedResultName string) ([]segmentMeta, error) {
	resp, err := http.Get(playlist.Link)
	if err != nil {
		return nil, fmt.Errorf("cannot get variantPlaylist link: %w", err)
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()

	linkPrefix := playlist.getPlayListLinkPrefix()

	split := bytes.Split(body, []byte{'\n'})

	concatenatedResultName = strings.ReplaceAll(concatenatedResultName, " ", "_")
	concatenatedResultName = strings.ReplaceAll(concatenatedResultName, "/", "")

	tmpFilePrefix := filepath.Join(dirName, concatenatedResultName)

	howMany := (len(split) - 6 + 1) / 2 // see comment before the func

	metas := make([]segmentMeta, 0, howMany)

	for segmentIndex, i := 0, 7; i < len(split); segmentIndex, i = segmentIndex+1, i+2 {
		segment := string(split[i])
		link := linkPrefix + "/" + segment
		fileName := tmpFilePrefix + "_" + strconv.Itoa(segmentIndex) + ".ts"
		metas = append(metas, segmentMeta{segmentIndex, link, fileName})
	}

	return metas, nil
}
