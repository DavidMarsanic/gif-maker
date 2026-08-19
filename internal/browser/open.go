// Package browser opens URLs and reveals/opens files in the OS-native way,
// so the same code path works whether the target is "here's the UI" or
// "here's the finished PDF".
package browser

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Open launches the user's default browser (or default handler for a
// file:// path / any URL) at target.
func Open(target string) error {
	return run(openArgs(target))
}

// Reveal shows path selected in the OS file browser (Finder/Explorer/etc.).
func Reveal(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return run([]string{"open", "-R", path})
	case "windows":
		return run([]string{"explorer", "/select,", path})
	default:
		return run([]string{"xdg-open", filepath.Dir(path)})
	}
}

func openArgs(target string) []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"open", target}
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler", target}
	default:
		return []string{"xdg-open", target}
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command to run")
	}
	cmd := exec.Command(args[0], args[1:]...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("running %s: %w", args[0], err)
	}
	go cmd.Wait() // reap the process without blocking the caller
	return nil
}
