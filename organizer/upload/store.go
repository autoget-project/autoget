package upload

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/autoget-project/autoget/protocol"
)

var (
	ErrNotFound       = errors.New("upload not found")
	ErrMetaCorrupt    = errors.New("upload metadata corrupted")
	ErrSizeMismatch   = errors.New("upload size mismatch")
	ErrInvalidParam   = errors.New("invalid parameter")
	ErrStorageFull    = errors.New("insufficient storage")
	ErrChecksumFailed = errors.New("checksum mismatch")
)

var torrentIDRegex = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// ErrOffsetConflict indicates that the requested chunk offset does not match the server's last_offset.
type ErrOffsetConflict struct {
	LastOffset int64
}

func (e ErrOffsetConflict) Error() string {
	return fmt.Sprintf("upload offset conflict: server last_offset is %d", e.LastOffset)
}

// UploadMeta records the persistent state of an upload session.
type UploadMeta struct {
	UploadID     string `json:"upload_id"`
	TorrentID    string `json:"torrent_id"`
	RelativePath string `json:"relative_path"`
	TotalSize    int64  `json:"total_size"`
	Checksum     string `json:"checksum,omitempty"`
	ModifyTime   int64  `json:"modify_time,omitempty"`
	LastOffset   int64  `json:"last_offset"`
	UpdatedAt    int64  `json:"updated_at"` // Unix timestamp in seconds
}

// Store handles on-disk chunk persistence, metadata state, and atomic finalization.
type Store struct {
	root         string // directory where .part and .json files are stored (e.g. {completedDir}/.uploads)
	completedDir string // final destination directory
	reserveBytes uint64 // minimum storage headroom required for new uploads

	mu    sync.RWMutex
	locks map[string]*sync.Mutex
}

// NewStore initializes an upload Store and ensures the root uploads directory exists.
func NewStore(root, completedDir string, reserveBytes uint64) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create upload root directory: %w", err)
	}
	return &Store{
		root:         root,
		completedDir: completedDir,
		reserveBytes: reserveBytes,
		locks:        make(map[string]*sync.Mutex),
	}, nil
}

func (s *Store) getLock(uploadID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, ok := s.locks[uploadID]
	if !ok {
		lock = &sync.Mutex{}
		s.locks[uploadID] = lock
	}
	return lock
}

func (s *Store) removeLock(uploadID string) {
	s.mu.Lock()
	delete(s.locks, uploadID)
	s.mu.Unlock()
}

func (s *Store) metaPath(uploadID string) string {
	return filepath.Join(s.root, uploadID+".json")
}

func (s *Store) partPath(uploadID string) string {
	return filepath.Join(s.root, uploadID+".part")
}

// ValidateParams verifies torrent_id and relative_path security invariants.
func (s *Store) ValidateParams(torrentID, relPath string) error {
	if !torrentIDRegex.MatchString(torrentID) {
		return fmt.Errorf("%w: invalid torrent_id format", ErrInvalidParam)
	}
	if relPath == "" || filepath.IsAbs(relPath) || strings.HasPrefix(relPath, "/") || strings.HasPrefix(relPath, "\\") {
		return fmt.Errorf("%w: relative_path must be non-empty and relative", ErrInvalidParam)
	}

	cleanRel := filepath.Clean(relPath)
	if cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: relative_path escapes target directory", ErrInvalidParam)
	}

	// Final destination check
	destDir := filepath.Join(s.completedDir, torrentID)
	finalPath := filepath.Join(destDir, cleanRel)
	relToDest, err := filepath.Rel(destDir, finalPath)
	if err != nil || strings.HasPrefix(relToDest, "..") || relToDest == "." {
		return fmt.Errorf("%w: relative_path escapes destination tree", ErrInvalidParam)
	}

	// Symlink check on existing elements along the path
	checkPath := destDir
	for _, part := range strings.Split(cleanRel, string(filepath.Separator)) {
		checkPath = filepath.Join(checkPath, part)
		info, err := os.Lstat(checkPath)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%w: relative_path components cannot be symlinks", ErrInvalidParam)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	return nil
}

// FinalPath returns the absolute path where the file will reside once completed.
func (s *Store) FinalPath(torrentID, relPath string) string {
	return filepath.Join(s.completedDir, torrentID, filepath.Clean(relPath))
}

// CheckFreeSpace verifies available storage capacity using unix.Statfs.
func (s *Store) CheckFreeSpace(requiredBytes int64) error {
	var stat unix.Statfs_t
	if err := unix.Statfs(s.completedDir, &stat); err != nil {
		return fmt.Errorf("failed to stat filesystem: %w", err)
	}

	// Available space for unprivileged users = Bavail * Bsize
	availableBytes := stat.Bavail * uint64(stat.Bsize)
	neededBytes := uint64(requiredBytes) + s.reserveBytes
	if availableBytes < neededBytes {
		return fmt.Errorf("%w: available %d bytes, need %d bytes (including %d reserve)", ErrStorageFull, availableBytes, neededBytes, s.reserveBytes)
	}
	return nil
}

// atomicSaveMeta writes meta JSON to a temporary file, fsyncs it, atomically renames it, and fsyncs the parent directory.
func (s *Store) atomicSaveMeta(meta *UploadMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}

	target := s.metaPath(meta.UploadID)
	tmpPath := target + fmt.Sprintf(".tmp.%d", time.Now().UnixNano())

	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	// fsync directory
	dirF, err := os.Open(s.root)
	if err == nil {
		_ = dirF.Sync()
		_ = dirF.Close()
	}
	return nil
}

// loadMeta reads and parses upload metadata, enforcing truncation self-healing invariants.
func (s *Store) loadMeta(uploadID string) (*UploadMeta, error) {
	metaFile := s.metaPath(uploadID)
	data, err := os.ReadFile(metaFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	var meta UploadMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, ErrMetaCorrupt
	}

	// Truncation self-healing on part file
	partFile := s.partPath(uploadID)
	partInfo, err := os.Stat(partFile)
	if err != nil {
		if os.IsNotExist(err) {
			if meta.LastOffset == 0 {
				return &meta, nil
			}
			return nil, ErrNotFound
		}
		return nil, err
	}

	partSize := partInfo.Size()
	if partSize > meta.LastOffset {
		// Crashed mid-write: truncate back to last authoritative offset
		if err := os.Truncate(partFile, meta.LastOffset); err != nil {
			return nil, fmt.Errorf("failed to truncate part file: %w", err)
		}
	} else if partSize < meta.LastOffset {
		// External corruption / unexpected file shrink: invalidate session
		_ = os.Remove(metaFile)
		_ = os.Remove(partFile)
		return nil, ErrNotFound
	}

	return &meta, nil
}

// Init handles upload initialization or retrieval of an existing upload session.
func (s *Store) Init(req *protocol.UploadInitRequest) (*protocol.UploadStatusResponse, bool, error) {
	if err := s.ValidateParams(req.TorrentID, req.RelativePath); err != nil {
		return nil, false, err
	}

	uploadID := protocol.DeriveUploadID(req.TorrentID, req.RelativePath)
	lock := s.getLock(uploadID)
	lock.Lock()
	defer lock.Unlock()

	finalTarget := s.FinalPath(req.TorrentID, req.RelativePath)
	if fi, err := os.Stat(finalTarget); err == nil && !fi.IsDir() {
		if fi.Size() == req.TotalSize {
			return &protocol.UploadStatusResponse{
				UploadID:  uploadID,
				Offset:    req.TotalSize,
				TotalSize: req.TotalSize,
				Status:    protocol.StatusCompleted,
			}, false, nil
		}
	}

	// Special case: 0-byte file immediately creates destination and completes
	if req.TotalSize == 0 {
		if err := os.MkdirAll(filepath.Dir(finalTarget), 0o755); err != nil {
			return nil, false, err
		}
		f, err := os.OpenFile(finalTarget, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return nil, false, err
		}
		_ = f.Close()
		if req.ModifyTime > 0 {
			mTime := time.Unix(req.ModifyTime, 0)
			_ = os.Chtimes(finalTarget, mTime, mTime)
		}
		return &protocol.UploadStatusResponse{
			UploadID:  uploadID,
			Offset:    0,
			TotalSize: 0,
			Status:    protocol.StatusCompleted,
		}, false, nil
	}

	// Check if session already exists
	meta, err := s.loadMeta(uploadID)
	if err == nil {
		if meta.TotalSize != req.TotalSize {
			return nil, false, fmt.Errorf("%w: total size mismatch (existing %d, requested %d)", ErrSizeMismatch, meta.TotalSize, req.TotalSize)
		}
		return &protocol.UploadStatusResponse{
			UploadID:  uploadID,
			Offset:    meta.LastOffset,
			TotalSize: meta.TotalSize,
			Status:    protocol.StatusUploading,
		}, false, nil
	} else if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrMetaCorrupt) {
		return nil, false, err
	}

	// Create new session: check storage headroom
	if err := s.CheckFreeSpace(req.TotalSize); err != nil {
		return nil, false, err
	}

	now := time.Now().Unix()
	meta = &UploadMeta{
		UploadID:     uploadID,
		TorrentID:    req.TorrentID,
		RelativePath: req.RelativePath,
		TotalSize:    req.TotalSize,
		Checksum:     req.Checksum,
		ModifyTime:   req.ModifyTime,
		LastOffset:   0,
		UpdatedAt:    now,
	}

	// Create or truncate .part file
	partPath := s.partPath(uploadID)
	f, err := os.OpenFile(partPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, false, err
	}
	_ = f.Close()

	if err := s.atomicSaveMeta(meta); err != nil {
		_ = os.Remove(partPath)
		return nil, false, err
	}

	return &protocol.UploadStatusResponse{
		UploadID:  uploadID,
		Offset:    0,
		TotalSize: meta.TotalSize,
		Status:    protocol.StatusUploading,
	}, true, nil
}

// Get returns the current status and triggers truncation self-healing if needed.
func (s *Store) Get(uploadID string) (*protocol.UploadStatusResponse, error) {
	lock := s.getLock(uploadID)
	lock.Lock()
	defer lock.Unlock()

	meta, err := s.loadMeta(uploadID)
	if err != nil {
		return nil, err
	}

	return &protocol.UploadStatusResponse{
		UploadID:  meta.UploadID,
		Offset:    meta.LastOffset,
		TotalSize: meta.TotalSize,
		Status:    protocol.StatusUploading,
	}, nil
}

// AppendChunk writes n bytes from r into .part at the specified offset.
// If expectedChecksum is non-empty, chunk bytes are verified on the fly against it.
func (s *Store) AppendChunk(uploadID string, offset int64, r io.Reader, n int64, expectedChecksum string) (int64, error) {
	lock := s.getLock(uploadID)
	lock.Lock()
	defer lock.Unlock()

	meta, err := s.loadMeta(uploadID)
	if err != nil {
		return 0, err
	}

	if offset != meta.LastOffset {
		return meta.LastOffset, ErrOffsetConflict{LastOffset: meta.LastOffset}
	}

	partPath := s.partPath(uploadID)
	partFile, err := os.OpenFile(partPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return 0, err
	}
	defer func() { _ = partFile.Close() }()

	// Ensure file size equals meta.LastOffset
	fi, err := partFile.Stat()
	if err != nil {
		return 0, err
	}
	if fi.Size() != meta.LastOffset {
		if err := partFile.Truncate(meta.LastOffset); err != nil {
			return 0, err
		}
	}

	if _, err := partFile.Seek(meta.LastOffset, io.SeekStart); err != nil {
		return 0, err
	}

	reader := io.Reader(r)
	hasher := sha256.New()
	if expectedChecksum != "" {
		reader = io.TeeReader(r, hasher)
	}

	written, copyErr := io.CopyN(partFile, reader, n)
	if copyErr != nil && !errors.Is(copyErr, io.EOF) {
		// Truncate back on error
		_ = partFile.Truncate(meta.LastOffset)
		return meta.LastOffset, copyErr
	}

	if expectedChecksum != "" {
		actualChecksum := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(actualChecksum, expectedChecksum) && !strings.EqualFold("sha256:"+actualChecksum, expectedChecksum) {
			_ = partFile.Truncate(meta.LastOffset)
			return meta.LastOffset, fmt.Errorf("%w: expected %s, got %s", ErrChecksumFailed, expectedChecksum, actualChecksum)
		}
	}

	if err := partFile.Sync(); err != nil {
		_ = partFile.Truncate(meta.LastOffset)
		return meta.LastOffset, err
	}

	meta.LastOffset += written
	meta.UpdatedAt = time.Now().Unix()

	if err := s.atomicSaveMeta(meta); err != nil {
		return meta.LastOffset - written, err
	}

	return meta.LastOffset, nil
}

// Finish verifies total size, performs atomic rename to destination, sets modify time, and deletes metadata.
func (s *Store) Finish(uploadID string, req *protocol.UploadFinishRequest) error {
	lock := s.getLock(uploadID)
	lock.Lock()
	defer lock.Unlock()

	meta, err := s.loadMeta(uploadID)
	if err != nil {
		if errors.Is(err, ErrNotFound) && req != nil && req.TorrentID != "" && req.RelativePath != "" {
			// Idempotency check: verify if destination file exists
			finalTarget := s.FinalPath(req.TorrentID, req.RelativePath)
			if fi, statErr := os.Stat(finalTarget); statErr == nil && !fi.IsDir() {
				s.removeLock(uploadID)
				return nil
			}
		}
		return err
	}

	if meta.LastOffset != meta.TotalSize {
		return ErrOffsetConflict{LastOffset: meta.LastOffset}
	}

	partPath := s.partPath(uploadID)

	// Optional full file checksum verification
	if meta.Checksum != "" {
		if err := verifyFileChecksum(partPath, meta.Checksum); err != nil {
			return err
		}
	}

	finalTarget := s.FinalPath(meta.TorrentID, meta.RelativePath)
	targetDir := filepath.Dir(finalTarget)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	if err := os.Rename(partPath, finalTarget); err != nil {
		return fmt.Errorf("failed to rename part file to final destination: %w", err)
	}

	if meta.ModifyTime > 0 {
		mTime := time.Unix(meta.ModifyTime, 0)
		_ = os.Chtimes(finalTarget, mTime, mTime)
	}

	// Delete metadata
	_ = os.Remove(s.metaPath(uploadID))

	// Sync target directory
	if df, err := os.Open(targetDir); err == nil {
		_ = df.Sync()
		_ = df.Close()
	}

	s.removeLock(uploadID)
	return nil
}

// Cancel removes part and meta files. Operation is idempotent.
func (s *Store) Cancel(uploadID string) error {
	lock := s.getLock(uploadID)
	lock.Lock()
	defer lock.Unlock()

	_ = os.Remove(s.partPath(uploadID))
	_ = os.Remove(s.metaPath(uploadID))

	s.removeLock(uploadID)
	return nil
}

func verifyFileChecksum(filePath, expected string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return err
	}

	actual := hex.EncodeToString(hasher.Sum(nil))
	cleanExpected := strings.TrimPrefix(expected, "sha256:")
	if !strings.EqualFold(actual, cleanExpected) {
		return fmt.Errorf("%w: expected %s, got %s", ErrChecksumFailed, cleanExpected, actual)
	}
	return nil
}
