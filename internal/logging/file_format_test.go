package logging

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingFileOutput_JSONFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "olb.log")
	out, err := NewRotatingFileOutput(RotatingFileOptions{Filename: path, Format: "json"})
	if err != nil {
		t.Fatal(err)
	}
	logger := New(out)
	logger.Info("hello \"world\"", String("k", "v"), Int("n", 3))
	logger.Error("second line")
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("line %q is not JSON: %v", sc.Text(), err)
		}
		lines = append(lines, m)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0]["msg"] != `hello "world"` || lines[0]["k"] != "v" || lines[0]["n"] != float64(3) {
		t.Errorf("first entry = %v", lines[0])
	}
	if lines[1]["level"] != "ERROR" && lines[1]["level"] != "error" {
		t.Errorf("second entry level = %v", lines[1]["level"])
	}
}

func TestRotatingFileOutput_DefaultStaysText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "olb.log")
	out, err := NewRotatingFileOutput(RotatingFileOptions{Filename: path})
	if err != nil {
		t.Fatal(err)
	}
	New(out).Info("plain", String("k", "v"))
	out.Close()
	data, _ := os.ReadFile(path)
	if strings.HasPrefix(string(data), "{") || !strings.Contains(string(data), "plain k=v") {
		t.Errorf("text layout changed: %q", data)
	}
}
