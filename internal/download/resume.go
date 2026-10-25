package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	resumeVersion             = 1
	resumeFileFormat          = ".dai-playlist-%s.json"
	resumeLockFormat          = ".dai-playlist-%s.lock"
	resumeTemporaryPattern    = ".dai-manifest-*"
	resumeFilePermissions     = 0o600
	resumeReadLimit           = 8 * 1024 * 1024
	resumeHashBufferSize      = 128 * 1024
	resumeJSONIndent          = "  "
	resumePathSeparators      = "/\\"
	resumeInvalidManifest     = "invalid playlist resume manifest"
	resumeUnsupportedVersion  = "unsupported playlist resume manifest version: %d"
	resumeWrongPlaylist       = "resume manifest belongs to another playlist"
	resumeInvalidPlaylistID   = "invalid playlist ID for resume state"
	resumeOutputMismatch      = "recorded output changed: %s; move it aside before resuming"
	resumeUnknownOutput       = "output is not recorded as complete: %s; move it aside before resuming"
	resumeNonregularFile      = "resume requires a regular file: %s"
	resumeFileChanged         = "file changed while calculating checksum: %s"
	resumeStateError          = "playlist resume state: %w"
	resumeLockBusy            = "playlist resume state is already in use: %w"
	resumeOnlyPlaylistMessage = "resume is supported only for playlists"
	resumeOverwriteMessage    = "resume cannot be combined with overwrite"
	resumeUnknownTarget       = "video is missing from the resume manifest: %s"
)

type resumeManifest struct {
	Version    int                    `json:"version"`
	PlaylistID string                 `json:"playlist_id"`
	Videos     map[string]resumeVideo `json:"videos"`
}

type resumeVideo struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
}

func newPlaylistRecovery(directory string, playlistID string) (*playlistRecovery, error) {
	if playlistID == "" || playlistID == "." || playlistID == ".." || strings.ContainsAny(playlistID, resumePathSeparators) {
		return nil, errors.New(resumeInvalidPlaylistID)
	}

	manifestPath := filepath.Join(directory, fmt.Sprintf(resumeFileFormat, playlistID))
	lockPath := filepath.Join(directory, fmt.Sprintf(resumeLockFormat, playlistID))

	lock, err := newResumeLock(lockPath)
	if err != nil {
		return nil, fmt.Errorf(resumeStateError, err)
	}

	videos := make(map[string]resumeVideo)
	manifest := resumeManifest{Version: resumeVersion, PlaylistID: playlistID, Videos: videos}
	byFilename := make(map[string]string)

	recovery := &playlistRecovery{
		directory:  directory,
		path:       manifestPath,
		lock:       lock,
		manifest:   manifest,
		byFilename: byFilename,
	}
	if err := recovery.load(); err != nil {
		return nil, errors.Join(fmt.Errorf(resumeStateError, err), lock.Close())
	}

	return recovery, nil
}

type playlistRecovery struct {
	mutex      sync.Mutex
	directory  string
	path       string
	lock       *resumeLock
	manifest   resumeManifest
	byFilename map[string]string
}

func (recovery *playlistRecovery) Close() error {
	return recovery.lock.Close()
}

func (recovery *playlistRecovery) filename(videoID string) string {
	return recovery.manifest.Videos[videoID].Filename
}

func (recovery *playlistRecovery) inspect(operationContext context.Context, videoID string) (bool, error) {
	video, exists := recovery.manifest.Videos[videoID]
	if !exists {
		return false, nil
	}

	target := filepath.Join(recovery.directory, video.Filename)

	information, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	if !information.Mode().IsRegular() {
		return false, fmt.Errorf(resumeNonregularFile, target)
	}

	if video.SHA256 == "" {
		return false, fmt.Errorf(resumeUnknownOutput, target)
	}

	if information.Size() != video.Size {
		return false, fmt.Errorf(resumeOutputMismatch, target)
	}

	digest, size, err := checksumResumeFile(operationContext, target)
	if err != nil {
		return false, err
	}

	if digest != video.SHA256 || size != video.Size {
		return false, fmt.Errorf(resumeOutputMismatch, target)
	}

	return true, nil
}

func (recovery *playlistRecovery) add(videoID string, filename string) error {
	if owner, exists := recovery.byFilename[filename]; exists && owner != videoID {
		return fmt.Errorf(duplicatePlaylistOutputFormat, filename)
	}

	video := recovery.manifest.Videos[videoID]
	video.Filename = filename
	recovery.manifest.Videos[videoID] = video
	recovery.byFilename[filename] = videoID

	return nil
}

func (recovery *playlistRecovery) prepare(operationContext context.Context, videoID string, staging string) error {
	digest, size, err := checksumResumeFile(operationContext, staging)
	if err != nil {
		return err
	}

	recovery.mutex.Lock()
	defer recovery.mutex.Unlock()

	video, exists := recovery.manifest.Videos[videoID]
	if !exists {
		return fmt.Errorf(resumeUnknownTarget, videoID)
	}

	video.Size = size
	video.SHA256 = digest
	recovery.manifest.Videos[videoID] = video

	// Persist publication intent first so a crash after publication is recoverable.
	return recovery.save()
}

func (recovery *playlistRecovery) load() (resultError error) {
	information, err := os.Lstat(recovery.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	if !information.Mode().IsRegular() || information.Size() > resumeReadLimit {
		return errors.New(resumeInvalidManifest)
	}

	file, err := os.Open(recovery.path)
	if err != nil {
		return err
	}

	defer func() {
		resultError = errors.Join(resultError, file.Close())
	}()

	reader := io.LimitReader(file, resumeReadLimit+1)

	body, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	if len(body) > resumeReadLimit {
		return errors.New(resumeInvalidManifest)
	}

	var manifest resumeManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return fmt.Errorf(resumeStateError, err)
	}

	if manifest.Version != resumeVersion {
		return fmt.Errorf(resumeUnsupportedVersion, manifest.Version)
	}

	if manifest.PlaylistID != recovery.manifest.PlaylistID {
		return errors.New(resumeWrongPlaylist)
	}

	if manifest.Videos == nil {
		return errors.New(resumeInvalidManifest)
	}

	for videoID, video := range manifest.Videos {
		if videoID == "" || !validResumeFilename(video.Filename) {
			return errors.New(resumeInvalidManifest)
		}

		if _, exists := recovery.byFilename[video.Filename]; exists {
			return errors.New(resumeInvalidManifest)
		}

		if video.SHA256 == "" {
			if video.Size != 0 {
				return errors.New(resumeInvalidManifest)
			}
		} else {
			digest, err := hex.DecodeString(video.SHA256)
			if err != nil || len(digest) != sha256.Size || video.Size <= 0 {
				return errors.New(resumeInvalidManifest)
			}
		}

		recovery.byFilename[video.Filename] = videoID
	}

	recovery.manifest = manifest

	return nil
}

func validResumeFilename(filename string) bool {
	stem, hasExtension := strings.CutSuffix(filename, outputExtension)
	if !hasExtension || len(stem) > outputNameLimit {
		return false
	}

	// Truncation can expose a trailing dot or space in an otherwise safe name.
	// Accept those recorded names without renaming existing downloaded files.
	trimmedStem := strings.TrimRight(stem, trimmedFilenameCharacters)

	return safeFilename(stem) == trimmedStem+outputExtension
}

func (recovery *playlistRecovery) save() (resultError error) {
	body, err := json.MarshalIndent(recovery.manifest, "", resumeJSONIndent)
	if err != nil {
		return err
	}

	if len(body) > resumeReadLimit {
		return errors.New(resumeInvalidManifest)
	}

	file, err := os.CreateTemp(recovery.directory, resumeTemporaryPattern)
	if err != nil {
		return err
	}

	temporary := file.Name()
	defer func() {
		if temporary != "" {
			resultError = errors.Join(resultError, os.Remove(temporary))
		}
	}()

	if _, err := file.Write(body); err != nil {
		return errors.Join(err, file.Close())
	}

	if err := file.Sync(); err != nil {
		return errors.Join(err, file.Close())
	}

	if err := file.Close(); err != nil {
		return err
	}

	if err := os.Rename(temporary, recovery.path); err != nil {
		return err
	}

	temporary = ""

	return syncResumeDirectory(recovery.directory)
}

func openResumeLockFile(path string) (*os.File, error) {
	information, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if err == nil && !information.Mode().IsRegular() {
		return nil, fmt.Errorf(resumeNonregularFile, path)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, resumeFilePermissions)
	if err != nil {
		return nil, err
	}

	opened, err := file.Stat()
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}

	actual, err := os.Lstat(path)
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(opened, actual) {
		return nil, errors.Join(fmt.Errorf(resumeNonregularFile, path), err, file.Close())
	}

	return file, nil
}

func checksumResumeFile(operationContext context.Context, path string) (resultDigest string, resultSize int64, resultError error) {
	information, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}

	if !information.Mode().IsRegular() {
		return "", 0, fmt.Errorf(resumeNonregularFile, path)
	}

	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}

	defer func() {
		resultError = errors.Join(resultError, file.Close())
	}()

	opened, err := file.Stat()
	if err != nil {
		return "", 0, err
	}

	if !os.SameFile(information, opened) {
		return "", 0, fmt.Errorf(resumeFileChanged, path)
	}

	digest := sha256.New()
	buffer := make([]byte, resumeHashBufferSize)

	var size int64

	for {
		if err := operationContext.Err(); err != nil {
			return "", 0, err
		}

		count, readError := file.Read(buffer)
		if count > 0 {
			_, _ = digest.Write(buffer[:count])
			size += int64(count)
		}

		if errors.Is(readError, io.EOF) {
			break
		}

		if readError != nil {
			return "", 0, readError
		}
	}

	after, err := file.Stat()
	if err != nil {
		return "", 0, err
	}

	if size != opened.Size() || after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) {
		return "", 0, fmt.Errorf(resumeFileChanged, path)
	}

	return hex.EncodeToString(digest.Sum(nil)), size, nil
}
