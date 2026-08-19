// Package paths resolves where output files are written.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// ResolveDownloadsDir returns override (creating it if needed) or, if
// override is empty, the user's normal Downloads folder — the
// Securexe-standard save location isn't documented anywhere this tool has
// access to, so this is the sensible cross-platform default. Filename
// collisions and sanitization are left to yt-dlp's own output template
// handling, which already does both.
func ResolveDownloadsDir(override string) (string, error) {
	dir := override
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home directory: %w", err)
		}
		dir = filepath.Join(home, "Downloads")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating output directory %s: %w", dir, err)
	}
	return dir, nil
}
