// Package engine wraps ffmpeg + gifski behind a small, UI-agnostic
// interface: Inspect and Convert. It's a public (non-internal) package on
// purpose — this is the piece clip-and-gif imports as a real Go module
// dependency to get GIF conversion, rather than duplicating it.
//
// Neither tool is bundled; both are expected on PATH. gifski's own
// documented usage is exactly what Convert does under the hood:
//
//	ffmpeg -i video.mp4 -f yuv4mpegpipe - | gifski -o anim.gif -
package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var ErrMissingDependency = errors.New("required tool not found")

// VideoInfo is what Inspect reports about a local video file.
type VideoInfo struct {
	Duration float64 `json:"duration"` // seconds
	Width    int     `json:"width"`
	Height   int     `json:"height"`
}

// Options carries the user's GIF export choices.
type Options struct {
	// Width, if > 0, is passed to gifski's --width — the single biggest
	// lever for GIF file size, per gifski's own README tips.
	Width int
}

// Progress is streamed to the caller-supplied callback during Convert.
type Progress struct {
	Stage   string `json:"stage"` // converting, done, error
	Message string `json:"message,omitempty"`
}

// Result describes the GIF file written to disk.
type Result struct {
	Path     string `json:"path"`
	Filename string `json:"filename"`
}

// Engine is the whole gif-maker backend, independent of any UI.
type Engine struct {
	FFmpegPath  string
	FFprobePath string
	GifskiPath  string

	// VersionNotes is populated by New — one entry per resolved tool whose
	// version doesn't match what this engine was last tested against.
	// Non-blocking: a newer or older tool very often still works fine —
	// this just makes drift visible instead of silently invisible, since
	// nothing here pins or auto-updates either tool.
	VersionNotes []string

	toolsErr error // set by New if a required tool wasn't found; checked lazily
}

// New resolves ffmpeg/ffprobe/gifski on PATH and always returns a usable
// Engine — it deliberately never fails outright. A double-clicked GUI app
// has no terminal to print a startup error to, so a hard failure here
// would mean the UI just never appears with no visible explanation. A
// missing tool is instead surfaced the first time Inspect/Convert
// actually needs it, by which point there's a window that can show it.
func New() *Engine {
	e := &Engine{}
	ffmpeg, err := lookPath("ffmpeg")
	if err != nil {
		e.toolsErr = fmt.Errorf("%w: ffmpeg — install it (macOS: brew install ffmpeg; "+
			"otherwise see https://ffmpeg.org/download.html)", ErrMissingDependency)
		return e
	}
	ffprobe, err := lookPath("ffprobe")
	if err != nil {
		e.toolsErr = fmt.Errorf("%w: ffprobe — installed alongside ffmpeg; check your "+
			"ffmpeg installation includes it", ErrMissingDependency)
		return e
	}
	e.FFmpegPath, e.FFprobePath = ffmpeg, ffprobe

	gifski, err := lookPath("gifski")
	if err != nil {
		e.toolsErr = fmt.Errorf("%w: gifski — install it (macOS: brew install gifski; "+
			"otherwise see https://github.com/ImageOptim/gifski#download-and-install)", ErrMissingDependency)
		return e
	}
	e.GifskiPath = gifski
	e.VersionNotes = checkToolVersions(ffmpeg, gifski)
	return e
}

// lookPath resolves name via the standard PATH lookup, falling back to a
// short list of common install locations for the current OS if that fails.
//
// This exists because a GUI-launched process on macOS — whether opened via
// Finder/LaunchServices or spawned by securexe-launcher — does not inherit
// the user's interactive shell PATH. It gets the OS's minimal default
// (typically /usr/bin:/bin:/usr/sbin:/sbin), which excludes Homebrew's
// install directories entirely. A terminal-launched build never hits this,
// because the shell already sourced .zprofile/.zshrc and put Homebrew on
// PATH — which is exactly why this bug can pass local testing and then fail
// for a real double-clicked build. See also VideoClipDownloader/engine/tools.go,
// which has the same fallback for the same reason — keep the two in sync.
func lookPath(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	candidateName := name
	if runtime.GOOS == "windows" && filepath.Ext(name) == "" {
		candidateName = name + ".exe"
	}
	for _, dir := range fallbackToolDirsFunc() {
		candidate := filepath.Join(dir, candidateName)
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

// fallbackToolDirsFunc is a var (not a plain func call) so tests can
// substitute a temp directory instead of the real fallback locations.
var fallbackToolDirsFunc = fallbackToolDirs

// fallbackToolDirs lists common install locations for CLI tools that a
// GUI-launched process's minimal PATH won't include, ordered by likelihood.
func fallbackToolDirs() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/opt/homebrew/bin", // Homebrew on Apple Silicon
			"/usr/local/bin",    // Homebrew on Intel Macs
			"/opt/local/bin",    // MacPorts
		}
	case "linux":
		return []string{
			"/usr/local/bin",
			"/snap/bin",
			"/var/lib/flatpak/exports/bin",
		}
	case "windows":
		dirs := []string{`C:\ProgramData\chocolatey\bin`}
		if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, "scoop", "shims"))
		}
		return dirs
	default:
		return nil
	}
}

// isExecutableFile reports whether path is a regular file that can be run
// as a command. Windows has no executable bit to check, so existence as a
// non-directory is treated as sufficient there.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0111 != 0
}

// expectedFFmpegVersion and expectedGifskiVersion are what this engine was
// actually last tested against — keep in sync with brightencode.json's
// matching dependency entries by hand; nothing enforces the two staying
// equal, but a mismatch between them here is a real bug.
const (
	expectedFFmpegVersion = "9.0.1"
	expectedGifskiVersion = "1.34.0"
)

// checkToolVersions runs a version command against each resolved tool and
// returns one human-readable note per tool whose reported version doesn't
// match expectedFFmpegVersion/expectedGifskiVersion. Deliberately
// non-blocking — see the VersionNotes doc comment above.
func checkToolVersions(ffmpegPath, gifskiPath string) []string {
	var notes []string
	if v, err := ffmpegVersion(ffmpegPath); err == nil && v != expectedFFmpegVersion {
		notes = append(notes, fmt.Sprintf(
			"ffmpeg is %s, this app was last tested against %s — should still work, but if something breaks, that's the first thing to check",
			v, expectedFFmpegVersion))
	}
	if v, err := gifskiVersion(gifskiPath); err == nil && v != expectedGifskiVersion {
		notes = append(notes, fmt.Sprintf(
			"gifski is %s, this app was last tested against %s — should still work, but if something breaks, that's the first thing to check",
			v, expectedGifskiVersion))
	}
	return notes
}

// ffmpeg's "-version" output starts with "ffmpeg version 9.0.1 Copyright...";
// only the version token itself is worth comparing.
func ffmpegVersion(path string) (string, error) {
	out, err := exec.Command(path, "-version").Output()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "version" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}
	return "", fmt.Errorf("unrecognized ffmpeg -version output")
}

// gifski's "--version" output is just "gifski 1.34.0".
func gifskiVersion(path string) (string, error) {
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return "", fmt.Errorf("unrecognized gifski --version output")
	}
	return fields[len(fields)-1], nil
}

// CheckTools reports the missing-dependency error recorded by New, if any.
func (e *Engine) CheckTools() error {
	return e.toolsErr
}

// Inspect reads duration and frame size from a local video file via ffprobe.
func (e *Engine) Inspect(ctx context.Context, path string) (*VideoInfo, error) {
	if e.toolsErr != nil {
		return nil, e.toolsErr
	}
	cmd := exec.CommandContext(ctx, e.FFprobePath,
		"-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", path)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("reading video info: %w", err)
	}

	var probe struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		return nil, fmt.Errorf("parsing video info: %w", err)
	}

	info := &VideoInfo{}
	fmt.Sscanf(probe.Format.Duration, "%f", &info.Duration)
	for _, s := range probe.Streams {
		if s.CodecType == "video" {
			info.Width, info.Height = s.Width, s.Height
			break
		}
	}
	return info, nil
}

// Convert trims srcPath to [start, end) (the whole file if both are nil)
// and encodes it to a GIF via ffmpeg | gifski, writing the result into
// outputDir. Never through a shell: the two processes are wired together
// directly via an OS pipe on ffmpeg's stdout / gifski's stdin.
func (e *Engine) Convert(ctx context.Context, srcPath string, start, end *time.Duration, opts Options, outputDir string, onProgress func(Progress)) (*Result, error) {
	if e.toolsErr != nil {
		return nil, e.toolsErr
	}

	if onProgress != nil {
		onProgress(Progress{Stage: "converting", Message: "converting to GIF…"})
	}

	stem := strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
	outPath := uniquePath(outputDir, stem, ".gif")

	var ffArgs []string
	if start != nil {
		ffArgs = append(ffArgs, "-ss", secondsArg(*start))
	}
	ffArgs = append(ffArgs, "-i", srcPath)
	if start != nil && end != nil {
		ffArgs = append(ffArgs, "-t", secondsArg(*end-*start))
	}
	ffArgs = append(ffArgs, "-f", "yuv4mpegpipe", "-")

	gsArgs := []string{"-o", outPath}
	if opts.Width > 0 {
		gsArgs = append(gsArgs, "--width", strconv.Itoa(opts.Width))
	}
	gsArgs = append(gsArgs, "-")

	ff := exec.CommandContext(ctx, e.FFmpegPath, ffArgs...)
	gs := exec.CommandContext(ctx, e.GifskiPath, gsArgs...)

	pipe, err := ff.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("starting ffmpeg: %w", err)
	}
	gs.Stdin = pipe

	var ffStderr, gsStderr bytes.Buffer
	ff.Stderr = &ffStderr
	gs.Stderr = &gsStderr

	// gifski has to be listening before ffmpeg starts writing, or the
	// first chunk of the y4m stream has nowhere to go.
	if err := gs.Start(); err != nil {
		return nil, fmt.Errorf("starting gifski: %w", err)
	}
	if err := ff.Start(); err != nil {
		_ = gs.Process.Kill()
		return nil, fmt.Errorf("starting ffmpeg: %w", err)
	}

	ffErr := ff.Wait()
	gsErr := gs.Wait()
	if ffErr != nil {
		_ = os.Remove(outPath)
		return nil, fmt.Errorf("ffmpeg: %s", errDetail(ffStderr.String()))
	}
	if gsErr != nil {
		_ = os.Remove(outPath)
		return nil, fmt.Errorf("gifski: %s", errDetail(gsStderr.String()))
	}

	return &Result{Path: outPath, Filename: filepath.Base(outPath)}, nil
}

func secondsArg(d time.Duration) string {
	return fmt.Sprintf("%.3f", d.Seconds())
}

// uniquePath never silently overwrites something already in outputDir.
func uniquePath(dir, stem, ext string) string {
	candidate := filepath.Join(dir, stem+ext)
	for i := 2; fileExists(candidate); i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
	}
	return candidate
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// errDetail keeps the last few non-empty stderr lines — ffmpeg/gifski
// often explain a failure a line or two before their final terse message.
func errDetail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "failed with no output"
	}
	lines := strings.Split(s, "\n")
	var kept []string
	for i := len(lines) - 1; i >= 0 && len(kept) < 5; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		kept = append([]string{line}, kept...)
	}
	return strings.Join(kept, " | ")
}
