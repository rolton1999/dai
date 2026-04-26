package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func downloadSingleVideo(videoID string) {
	stopGettingInfoMessage := spinningMessage("Getting info", "Got video info successfully")
	videoInfo, err := getVideoInfo(videoID)
	if err != nil {
		panic(err)
	}

	variantPlaylists, err := getVariantPlaylists(videoInfo.masterPlaylistLink)
	if err != nil {
		panic(err)
	}

	variantPlaylists = sortByResulutionDesc(variantPlaylists)

	stopGettingInfoMessage()

	err = downloadTSFiles("tmp", videoInfo.title, variantPlaylists[0])
	if err != nil {
		panic(err)
	}
}

func downloadTSFiles(dirName string, namePrefix string, playlist variantPlaylist) error {
	resp, err := http.Get(playlist.link)
	if err != nil {
		return fmt.Errorf("cannot get variantPlaylist link: %w", err)
	}

	const readWriteMode = 0777
	err = os.Mkdir(dirName, readWriteMode)
	if err != nil {
		return fmt.Errorf("cannot create temp dir: %w", err)
	}

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

	body, err := io.ReadAll(resp.Body)
	// fmt.Println("body: ", string(body))
	// os.Exit(0)
	resp.Body.Close()

	prefix := playlist.getPlayListLinkPrefix()

	split := bytes.Split(body, []byte{'\n'})

	namePrefix = strings.ReplaceAll(namePrefix, " ", "_")
	namePrefix = strings.ReplaceAll(namePrefix, "/", "")

	howMany := (len(split) - 6 + 1) / 2
	listOfFiles := make([]string, 0, howMany)
	for segmentIndex, i := 0, 7; i < len(split); segmentIndex, i = segmentIndex+1, i+2 {
		segment := string(split[i])
		link := prefix + "/" + segment
		resp, err = http.Get(link)
		if err != nil {
			return fmt.Errorf("cannot get first segment link: %w", err)
		}

		fileName := dirName + "/" + namePrefix + "_" + strconv.Itoa(segmentIndex) + ".ts"
		file, err := os.Create(fileName)
		if err != nil {
			panic(err)
		}

		io.Copy(file, resp.Body)
		file.Close()

		listOfFiles = append(listOfFiles, fileName)

		fmt.Printf("\rLoaded: %d%%", 100*(segmentIndex+1)/howMany)
	}
	fmt.Println()

	out := "./out.mp4"
	stopConvertingMessage := spinningMessage("converting", "Done: "+out)

	// write concat list file for ffmpeg concat demuxer
	listPath := "concat.txt"
	listF, err := os.Create(listPath)
	if err != nil {
		panic(err)
	}

	for _, t := range listOfFiles {
		// ffmpeg concat demuxer requires: file 'path'
		fmt.Fprintf(listF, "file '%s'\n", t)
	}
	listF.Close()

	cmd := exec.Command("ffmpeg", "-f", "concat", "-safe", "0", "-i", listPath, "-c", "copy", "-bsf:a", "aac_adtstoasc", out)
	// cmd.Stdout = os.Stdout
	// cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic(err)
	}

	os.Remove(listPath)
	os.RemoveAll(dirName)

	stopConvertingMessage()

	return nil
}

func spinningMessage(message string, after string) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		frames := []string{"/", "-", "\\", "|"}

		defer func() {
			// need to perfectly vanish the line by \r
			for len(after) < len(message)+5 /*5 for spin signs and `...`*/ {
				after += " "
			}
			fmt.Printf("\r%s\n", after)
			close(done)
		}()

		for frameI := 0; ; frameI = (frameI + 1) % len(frames) {
			select {
			case <-stop:
				return
			default:
			}
			fmt.Printf("\r%s %s...", frames[frameI], message)
			time.Sleep(200 * time.Millisecond)
		}
	}()

	return func() {
		close(stop)
		<-done
	}
}
