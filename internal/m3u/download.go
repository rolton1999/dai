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
		loaderQueueSize = 10
		loaderWorkers   = 10
	)

	listOfFiles := make([]string, len(segmentMetas))

	segmentMetaProducedIdx := 0
	produceStage := util.NewPipelineStage(1, 1, func(_ struct{}) (segmentMeta, bool) {
		getNext := func() (segmentMeta, bool) {
			if segmentMetaProducedIdx == len(segmentMetas) {
				return segmentMeta{}, true
			}

			meta := segmentMetas[segmentMetaProducedIdx]
			segmentMetaProducedIdx += 1
			return meta, false
		}
		return getNext()
	})

	loadStage := util.NewPipelineStage(loaderQueueSize, loaderWorkers, func(meta segmentMeta) (segmentLoadMeta, bool) {
		resp, err := http.Get(meta.link)
		if err != nil {
			return segmentLoadMeta{0, "", fmt.Errorf("cannot get first segment link: %w", err)}, true
		}

		file, err := os.Create(meta.fileName)
		if err != nil {
			return segmentLoadMeta{0, "", fmt.Errorf("cannot create file for ts-data: %w", err)}, true
		}

		io.Copy(file, resp.Body)
		_ = file.Close()

		return segmentLoadMeta{meta.index, meta.fileName, nil}, false
	})

	loadMetaConsumedIdx := 0
	consumeStage := util.NewPipelineStage(1, 1, func(meta segmentLoadMeta) (struct{}, bool) {
		process := func(meta segmentLoadMeta) (struct{}, bool) {
			if meta.err != nil {
				err = meta.err
				return struct{}{}, false
			}

			listOfFiles[meta.segmentIndex] = meta.fileName
			logger.UpdateWithSuffix(fmt.Sprintf(" ~ loading %d%%", 100*loadMetaConsumedIdx/len(listOfFiles)))

			loadMetaConsumedIdx += 1
			return struct{}{}, false
		}

		return process(meta)
	})

	pipeline := util.NewPipeline()
	pipeline.AppendStage(produceStage)
	pipeline.AppendStage(loadStage)
	pipeline.AppendStage(consumeStage)
	pipeline.Run()

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
