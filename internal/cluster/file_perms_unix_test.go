//go:build !windows

package cluster

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// Raft log entries and snapshots carry replicated config (which can hold
// credentials), so nothing under the data dir may be group/world readable.
func TestClusterDataFiles_PrivatePermissions(t *testing.T) {
	dir := t.TempDir()

	p, err := NewFilePersister(filepath.Join(dir, "raft"))
	if err != nil {
		t.Fatalf("NewFilePersister: %v", err)
	}
	if err := p.SaveRaftState(RaftStateV1{Term: 1, VotedFor: "n1"}); err != nil {
		t.Fatalf("SaveRaftState: %v", err)
	}
	if err := p.SaveLogEntry(&LogEntry{Index: 1, Term: 1, Command: []byte("cmd")}); err != nil {
		t.Fatalf("SaveLogEntry: %v", err)
	}

	store, err := NewFileSnapshotStore(filepath.Join(dir, "snapshots"), 3)
	if err != nil {
		t.Fatalf("NewFileSnapshotStore: %v", err)
	}
	if err := store.Save(&Snapshot{LastIncludedIndex: 1, LastIncludedTerm: 1, Data: []byte(`{}`)}); err != nil {
		t.Fatalf("Save snapshot: %v", err)
	}

	checked := 0
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == dir {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Errorf("%s mode = %o, want no group/other access", path, perm)
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 5 {
		t.Fatalf("checked only %d paths, expected dirs + state, log and snapshot files", checked)
	}
}
