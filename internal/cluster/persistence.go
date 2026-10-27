// Package cluster provides distributed clustering and consensus using Raft.
//
// This file implements file-based persistence for Raft state, allowing a node
// to recover its state (term, vote, log entries) after a restart. Snapshots
// are stored separately via the SnapshotStore interface.
package cluster

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// RaftStateV1 is the persisted Raft node state, written to raft_state.json.
type RaftStateV1 struct {
	Term        uint64 `json:"term"`
	VotedFor    string `json:"voted_for"`
	CommitIndex uint64 `json:"commit_index"`
	LastApplied uint64 `json:"last_applied"`
}

// FilePersister saves and loads Raft state to/from a data directory.
//
// Directory layout:
//
//	<dir>/raft_state.json   — node state (term, vote, indices)
//	<dir>/log/              — log entry files (entry_<index>.json)
//
// Thread-safe via mutex.
type FilePersister struct {
	mu  sync.Mutex
	dir string
}

// NewFilePersister creates a new persister rooted at dir.
// The directory is created if it does not exist.
func NewFilePersister(dir string) (*FilePersister, error) {
	if dir == "" {
		return nil, errors.New("cluster: persister dir is empty")
	}
	logDir := filepath.Join(dir, "log")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, fmt.Errorf("cluster: create persister dir: %w", err)
	}
	return &FilePersister{dir: dir}, nil
}

// SaveRaftState writes the current Raft state to disk.
func (p *FilePersister) SaveRaftState(state RaftStateV1) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal raft state: %w", err)
	}

	path := filepath.Join(p.dir, "raft_state.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write raft state: %w", err)
	}
	// Atomic rename
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename raft state: %w", err)
	}
	return nil
}

// LoadRaftState reads the persisted Raft state from disk.
// Returns a zero state if no state file exists (fresh start).
func (p *FilePersister) LoadRaftState() (RaftStateV1, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	path := filepath.Join(p.dir, "raft_state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return RaftStateV1{}, nil
		}
		return RaftStateV1{}, fmt.Errorf("read raft state: %w", err)
	}

	var state RaftStateV1
	if err := json.Unmarshal(data, &state); err != nil {
		return RaftStateV1{}, fmt.Errorf("unmarshal raft state: %w", err)
	}
	return state, nil
}

// SaveLogEntry appends a single log entry to the log directory.
func (p *FilePersister) SaveLogEntry(entry *LogEntry) error {
	if entry == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.saveLogEntryLocked(entry)
}

func (p *FilePersister) saveLogEntryLocked(entry *LogEntry) error {
	name := fmt.Sprintf("entry_%010d.json", entry.Index)
	path := filepath.Join(p.dir, "log", name)

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal log entry %d: %w", entry.Index, err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write log entry %d: %w", entry.Index, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename log entry %d: %w", entry.Index, err)
	}
	return nil
}

// SaveLogEntries saves all log entries, replacing any previously saved entries.
func (p *FilePersister) SaveLogEntries(entries []*LogEntry) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Remove old entries
	logDir := filepath.Join(p.dir, "log")
	entries2, _ := os.ReadDir(logDir)
	for _, e := range entries2 {
		if strings.HasPrefix(e.Name(), "entry_") {
			os.Remove(filepath.Join(logDir, e.Name()))
		}
	}

	// Write new entries
	for _, entry := range entries {
		if err := p.saveLogEntryLocked(entry); err != nil {
			return err
		}
	}
	return nil
}

// LoadLogEntries loads all persisted log entries in index order.
func (p *FilePersister) LoadLogEntries() ([]*LogEntry, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	logDir := filepath.Join(p.dir, "log")
	entries_dir, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read log dir: %w", err)
	}

	var entries []*LogEntry
	for _, e := range entries_dir {
		if !strings.HasPrefix(e.Name(), "entry_") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(logDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue // skip unreadable entries
		}
		var entry LogEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue // skip malformed entries
		}
		entries = append(entries, &entry)
	}

	// Sort by index
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Index < entries[j].Index
	})

	return entries, nil
}

// DeleteLogBefore removes log entries with index <= compactIndex.
func (p *FilePersister) DeleteLogBefore(compactIndex uint64) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	logDir := filepath.Join(p.dir, "log")
	entries_dir, err := os.ReadDir(logDir)
	if err != nil {
		return nil // nothing to delete
	}

	for _, e := range entries_dir {
		if !strings.HasPrefix(e.Name(), "entry_") {
			continue
		}
		// Parse index from filename: entry_0000000001.json
		var idx uint64
		if _, err := fmt.Sscanf(e.Name(), "entry_%d.json", &idx); err != nil {
			continue
		}
		if idx <= compactIndex {
			os.Remove(filepath.Join(logDir, e.Name()))
		}
	}
	return nil
}

// Dir returns the persistence directory.
func (p *FilePersister) Dir() string { return p.dir }
