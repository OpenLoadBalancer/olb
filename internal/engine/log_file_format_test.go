package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openloadbalancer/olb/internal/config"
)

// logging.format was ignored for file output, which was always plain text.
func TestCreateLogger_FileOutputHonoursFormat(t *testing.T) {
	for _, tc := range []struct {
		format   string
		wantJSON bool
	}{
		{"json", true},
		{"", true}, // json is the default, as for stdout
		{"text", false},
	} {
		t.Run("format="+tc.format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "olb.log")
			logger, rot := createLoggerWithOutput(&config.Logging{Level: "info", Format: tc.format, Output: path})
			if rot == nil {
				t.Fatal("expected a rotating file output")
			}
			logger.Info("entry")
			rot.Close()

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			line := strings.TrimSpace(string(data))
			var m map[string]any
			isJSON := json.Unmarshal([]byte(line), &m) == nil
			if isJSON != tc.wantJSON {
				t.Errorf("format %q: line %q, JSON = %v, want %v", tc.format, line, isJSON, tc.wantJSON)
			}
		})
	}
}
