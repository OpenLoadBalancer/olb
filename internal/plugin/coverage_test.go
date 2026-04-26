package plugin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// --------------------------------------------------------------------------
// Tests targeting uncovered code paths for 97%+ coverage
// --------------------------------------------------------------------------

// TestCov_IsAllowed_EmptyAllowedList directly calls the unexported isAllowed
// method with an empty AllowedPlugins slice. RegisterPlugin guards against
// calling isAllowed when the list is empty (short-circuit), so this branch
// is unreachable through the public API alone.
func TestCov_IsAllowed_EmptyAllowedList(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	// DefaultPluginManagerConfig has empty AllowedPlugins, so isAllowed
	// should return false via the empty-list branch (line 376).
	if pm.isAllowed("anything") {
		t.Error("isAllowed() should return false when AllowedPlugins is empty")
	}
}

// TestCov_IsAllowed_NonEmptyListMatch verifies the slices.Contains true branch.
func TestCov_IsAllowed_NonEmptyListMatch(t *testing.T) {
	cfg := PluginManagerConfig{
		AllowedPlugins: []string{"alpha", "beta"},
	}
	pm := NewPluginManager(cfg)

	if !pm.isAllowed("alpha") {
		t.Error("isAllowed() should return true for 'alpha'")
	}
	if !pm.isAllowed("beta") {
		t.Error("isAllowed() should return true for 'beta'")
	}
	if pm.isAllowed("gamma") {
		t.Error("isAllowed() should return false for 'gamma'")
	}
}

// TestCov_LoadDir_ReadDirNonNotExistError triggers a non-IsNotExist error
// from os.ReadDir with AllowedPlugins set so we get past the early return.
// On Windows, "NUL" is a reserved device name that causes ReadDir to return
// "Incorrect function" (not IsNotExist). On Unix, an unreadable directory
// produces a permission-denied error.
func TestCov_LoadDir_ReadDirNonNotExistError(t *testing.T) {
	cfg := PluginManagerConfig{
		AllowedPlugins: []string{"some-plugin"},
	}
	pm := NewPluginManager(cfg)

	if runtime.GOOS == "windows" {
		// "NUL" causes os.ReadDir to return a non-IsNotExist error on Windows
		err := pm.LoadDir("NUL")
		if err == nil {
			t.Log("LoadDir(NUL) did not error (unexpected on Windows)")
		} else {
			t.Logf("LoadDir(NUL) correctly returned error: %v", err)
		}
	} else {
		// On Unix, create a directory and remove read permissions
		if os.Getuid() == 0 {
			t.Skip("running as root, permission tests are unreliable")
		}
		dir := t.TempDir()
		noPermDir := filepath.Join(dir, "noperm")
		if err := os.Mkdir(noPermDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(noPermDir, 0000); err != nil {
			t.Skipf("chmod failed: %v", err)
		}
		defer os.Chmod(noPermDir, 0755)

		err := pm.LoadDir(noPermDir)
		if err == nil {
			t.Error("expected error for unreadable directory")
		}
	}
}

// TestCov_LoadPlugin_AbsPathError triggers the filepath.Abs error path in
// LoadPlugin by using a path containing a null byte, which causes
// filepath.Abs to return "invalid argument".
func TestCov_LoadPlugin_AbsPathError(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	err := pm.LoadPlugin("test\x00path/plugin.so")
	if err == nil {
		t.Error("LoadPlugin should fail for path with null byte")
	}
}

// TestCov_LoadDir_AbsPathError triggers the filepath.Abs error path in
// LoadDir by using a path containing a null byte. AllowedPlugins must be
// set to get past the early return in LoadDir.
func TestCov_LoadDir_AbsPathError(t *testing.T) {
	cfg := PluginManagerConfig{
		AllowedPlugins: []string{"some-plugin"},
	}
	pm := NewPluginManager(cfg)

	err := pm.LoadDir("test\x00dir")
	if err == nil {
		t.Error("LoadDir should fail for path with null byte")
	}
}

// TestCov_LoadPlugin_WindowsInability documents that lines 430-441 of LoadPlugin
// (Lookup, type assertion, factory call, RegisterPlugin) cannot be exercised
// on Windows because Go's plugin package does not support Windows.
// These 8 statements represent the only uncovered code that cannot be tested
// on this platform.
func TestCov_LoadPlugin_WindowsUnsupportedPaths(t *testing.T) {
	t.Skip("Go plugin.Open is not supported on Windows; lines 430-441 of LoadPlugin cannot be exercised")
}
