// Package cluster provides distributed clustering and consensus using Raft.
package cluster

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// --------------------------------------------------------------------------
// Snapshot types
// --------------------------------------------------------------------------

// Snapshot represents a point-in-time snapshot of the state machine.
type Snapshot struct {
	LastIncludedIndex uint64            `json:"last_included_index"`
	LastIncludedTerm  uint64            `json:"last_included_term"`
	Data              []byte            `json:"data"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

// SnapshotMeta contains metadata about a stored snapshot (without the data).
type SnapshotMeta struct {
	LastIncludedIndex uint64            `json:"last_included_index"`
	LastIncludedTerm  uint64            `json:"last_included_term"`
	Size              int64             `json:"size"`
	Timestamp         time.Time         `json:"timestamp"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

// SnapshotStore defines the interface for persisting and loading snapshots.
type SnapshotStore interface {
	// Save persists a snapshot.
	Save(snapshot *Snapshot) error
	// Load returns the most recent snapshot.
	Load() (*Snapshot, error)
	// List returns metadata for all available snapshots, newest first.
	List() ([]*SnapshotMeta, error)
}

// --------------------------------------------------------------------------
// MemorySnapshotStore – in-memory (testing)
// --------------------------------------------------------------------------

// MemorySnapshotStore keeps snapshots in memory. It is primarily intended for
// unit tests. Access is safe for concurrent use.
type MemorySnapshotStore struct {
	mu        sync.RWMutex
	snapshots []*snapshotEntry
}

type snapshotEntry struct {
	snapshot  *Snapshot
	timestamp time.Time
}

// NewMemorySnapshotStore creates a new in-memory snapshot store.
func NewMemorySnapshotStore() *MemorySnapshotStore {
	return &MemorySnapshotStore{
		snapshots: make([]*snapshotEntry, 0),
	}
}

// Save stores the snapshot in memory.
func (m *MemorySnapshotStore) Save(snapshot *Snapshot) error {
	if snapshot == nil {
		return errors.New("snapshot is nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Deep-copy the data so the caller can mutate its buffer safely.
	dataCopy := make([]byte, len(snapshot.Data))
	copy(dataCopy, snapshot.Data)

	metaCopy := make(map[string]string, len(snapshot.Metadata))
	for k, v := range snapshot.Metadata {
		metaCopy[k] = v
	}

	m.snapshots = append(m.snapshots, &snapshotEntry{
		snapshot: &Snapshot{
			LastIncludedIndex: snapshot.LastIncludedIndex,
			LastIncludedTerm:  snapshot.LastIncludedTerm,
			Data:              dataCopy,
			Metadata:          metaCopy,
		},
		timestamp: time.Now(),
	})

	return nil
}

// Load returns the most recent snapshot or an error if none exists.
func (m *MemorySnapshotStore) Load() (*Snapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.snapshots) == 0 {
		return nil, errors.New("no snapshots available")
	}

	entry := m.snapshots[len(m.snapshots)-1]
	return entry.snapshot, nil
}

// List returns metadata for all stored snapshots, newest first.
func (m *MemorySnapshotStore) List() ([]*SnapshotMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metas := make([]*SnapshotMeta, len(m.snapshots))
	for i, entry := range m.snapshots {
		metas[len(m.snapshots)-1-i] = &SnapshotMeta{
			LastIncludedIndex: entry.snapshot.LastIncludedIndex,
			LastIncludedTerm:  entry.snapshot.LastIncludedTerm,
			Size:              int64(len(entry.snapshot.Data)),
			Timestamp:         entry.timestamp,
			Metadata:          entry.snapshot.Metadata,
		}
	}

	return metas, nil
}

// --------------------------------------------------------------------------
// FileSnapshotStore – disk-based
// --------------------------------------------------------------------------

// FileSnapshotStore persists snapshots to a directory on disk. Each snapshot
// is stored as a JSON file named by its index. Only the most recent retain
// snapshots are kept.
type FileSnapshotStore struct {
	dir    string
	retain int
	mu     sync.Mutex
}

// NewFileSnapshotStore creates a new disk-backed snapshot store rooted at dir.
// retain controls how many snapshots to keep (minimum 1).
func NewFileSnapshotStore(dir string, retain int) (*FileSnapshotStore, error) {
	if retain < 1 {
		retain = 1
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create snapshot dir: %w", err)
	}

	return &FileSnapshotStore{
		dir:    dir,
		retain: retain,
	}, nil
}

func (f *FileSnapshotStore) snapshotPath(index uint64) string {
	return filepath.Join(f.dir, fmt.Sprintf("snapshot-%020d.json", index))
}

func (f *FileSnapshotStore) metaPath(index uint64) string {
	return filepath.Join(f.dir, fmt.Sprintf("snapshot-%020d.meta", index))
}

// Save writes the snapshot to disk and trims old snapshots.
func (f *FileSnapshotStore) Save(snapshot *Snapshot) error {
	if snapshot == nil {
		return errors.New("snapshot is nil")
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Write snapshot data.
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}

	path := f.snapshotPath(snapshot.LastIncludedIndex)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}

	// Write metadata.
	meta := &SnapshotMeta{
		LastIncludedIndex: snapshot.LastIncludedIndex,
		LastIncludedTerm:  snapshot.LastIncludedTerm,
		Size:              int64(len(snapshot.Data)),
		Timestamp:         time.Now(),
		Metadata:          snapshot.Metadata,
	}

	metaData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	metaPath := f.metaPath(snapshot.LastIncludedIndex)
	if err := os.WriteFile(metaPath, metaData, 0o644); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	// Trim old snapshots.
	return f.trimSnapshots()
}

// Load returns the most recent snapshot from disk.
func (f *FileSnapshotStore) Load() (*Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	files, err := f.listSnapshotFiles()
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, errors.New("no snapshots available")
	}

	// files are sorted ascending; pick the last (highest index).
	path := files[len(files)-1]
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}

	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}

	return &snapshot, nil
}

// List returns metadata for all snapshots on disk, newest first.
func (f *FileSnapshotStore) List() ([]*SnapshotMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	files, err := f.listMetaFiles()
	if err != nil {
		return nil, err
	}

	metas := make([]*SnapshotMeta, 0, len(files))
	for i := len(files) - 1; i >= 0; i-- {
		data, err := os.ReadFile(files[i])
		if err != nil {
			continue
		}
		var meta SnapshotMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		metas = append(metas, &meta)
	}

	return metas, nil
}

// listSnapshotFiles returns snapshot file paths sorted by name (ascending
// index).
func (f *FileSnapshotStore) listSnapshotFiles() ([]string, error) {
	pattern := filepath.Join(f.dir, "snapshot-*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// listMetaFiles returns meta file paths sorted by name (ascending index).
func (f *FileSnapshotStore) listMetaFiles() ([]string, error) {
	pattern := filepath.Join(f.dir, "snapshot-*.meta")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// trimSnapshots removes old snapshots, keeping only the latest retain count.
func (f *FileSnapshotStore) trimSnapshots() error {
	snapFiles, err := f.listSnapshotFiles()
	if err != nil {
		return err
	}
	metaFiles, err := f.listMetaFiles()
	if err != nil {
		return err
	}

	// Remove excess snapshot files.
	for len(snapFiles) > f.retain {
		if err := os.Remove(snapFiles[0]); err != nil && !os.IsNotExist(err) {
			return err
		}
		snapFiles = snapFiles[1:]
	}

	// Remove excess meta files.
	for len(metaFiles) > f.retain {
		if err := os.Remove(metaFiles[0]); err != nil && !os.IsNotExist(err) {
			return err
		}
		metaFiles = metaFiles[1:]
	}

	return nil
}
