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

// how to load m3u8
// https://gist.github.com/devinschumacher/aad315b92661fdaad316b949c33c94c7

func main() {
	if len(os.Args) == 0 {
		panic("enter some url")
	}

	url := os.Args[1]
	if len(url) == 0 {
		panic("url should not be empty")
	}

	linkType := analyzeArgURL(url)
	switch linkType {
	case singleVideo:
		videoID, err := getVideoIDFromArgURL(url)
		if err != nil {
			panic(err)
		}
		downloadSingleVideo(videoID)
	case playlist:
		panic("not implemented")
	}

}

func downloadSingleVideo(videoID string) {
	videoInfo, err := getVideoInfo(videoID)
	if err != nil {
		panic(err)
	}
	// fmt.Println("got videoInfo: ", videoInfo)

	variantPlaylists, err := getVariantPlaylists(videoInfo.masterPlaylistLink)
	if err != nil {
		panic(err)
	}

	variantPlaylists = sortByResulutionDesc(variantPlaylists)

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
	resp.Body.Close()

	prefix := playlist.getPlayListLinkPrefix()

	split := bytes.Split(body, []byte{'\n'})

	namePrefix = strings.ReplaceAll(namePrefix, " ", "_")
	namePrefix = strings.ReplaceAll(namePrefix, "/", "")

	howMany := (len(split) - 7) / 2
	fmt.Printf("Gonna load %d fragments\n", howMany)
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
	done := make(chan struct{})
	go func() {
		frames := []string{"/", "-", "\\", "|"}
		for frameI := 0; ; frameI = (frameI + 1) % len(frames) {
			select {
			case <-done:
				return
			default:
			}
			fmt.Printf("\r%s converting...", frames[frameI])
			time.Sleep(200 * time.Millisecond)
		}
	}()

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

	out := "./out.mp4"
	cmd := exec.Command("ffmpeg", "-f", "concat", "-safe", "0", "-i", listPath, "-c", "copy", "-bsf:a", "aac_adtstoasc", out)
	// cmd.Stdout = os.Stdout
	// cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic(err)
	}

	// optional: remove concat list
	os.Remove(listPath)
	os.RemoveAll(dirName)
	close(done)

	fmt.Println("done:", out)

	return nil
}

func getVariantPlaylists(m3u8Link string) ([]variantPlaylist, error) {
	const extM3UPrefix = "#EXTM3U"

	resp, err := http.Get(m3u8Link)
	if err != nil {
		return nil, fmt.Errorf("cannot get m3u8: %w", err)
	}

	extM3UBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cannot read body of m3u8: %w", err)
	}

	if !bytes.HasPrefix(extM3UBytes, []byte(extM3UPrefix)) {
		return nil, fmt.Errorf("expected extM3U")
	}

	extM3UBytes = extM3UBytes[len(extM3UPrefix):]
	extM3UBytes = bytes.TrimSpace(extM3UBytes)

	var variantPlaylists []variantPlaylist

	lines := bytes.Split(extM3UBytes, []byte{'\n'})

	for i := 0; i < len(lines); i += 2 {
		variantPlaylist, err := variantPlaylistFromBytes(lines[i], lines[i+1])
		if err != nil {
			return nil, fmt.Errorf("cannot parse variantPlaylist: %w", err)
		}
		variantPlaylists = append(variantPlaylists, variantPlaylist)
	}

	return variantPlaylists, nil
}
