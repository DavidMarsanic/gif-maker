// Command gif-maker turns a video file you already have into a
// high-quality GIF via ffmpeg + gifski, entirely on this machine. Bare
// invocation opens a local browser UI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/DavidMarsanic/gif-maker/engine"
	"github.com/DavidMarsanic/gif-maker/internal/browser"
	"github.com/DavidMarsanic/gif-maker/internal/paths"
	"github.com/DavidMarsanic/gif-maker/internal/server"
)

const version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("gif-maker", flag.ContinueOnError)

	output := fs.String("output", "", "output directory (default: your Downloads folder)")
	port := fs.Int("port", 0, "local UI server port (default: automatic)")
	showVersion := fs.Bool("version", false, "print the version and exit")
	fs.Usage = func() { printUsage(fs) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	if *showVersion {
		fmt.Println("gif-maker " + version)
		return 0
	}

	outputDir, err := paths.ResolveDownloadsDir(*output)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	eng := engine.New()
	for _, note := range eng.VersionNotes {
		fmt.Fprintln(os.Stderr, "note:", note)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := server.New(ctx, eng, outputDir)
	addr, err := srv.Start(*port)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	fmt.Fprintln(os.Stderr, "GIF Maker running at", addr, "— press Ctrl+C to quit")

	// When a host process (securexe-launcher) is the one showing the UI —
	// in its own native window, so it can get a real Dock identity instead
	// of a spawned Chrome window — it sets this before starting us and
	// watches this same stderr line to discover the URL. Opening our own
	// Chrome window too would just leave a second, redundant one.
	if os.Getenv("SECUREXE_HOSTED") == "" {
		if err := browser.OpenAppWindow(addr + "/"); err != nil {
			fmt.Fprintln(os.Stderr, "couldn't open a window automatically:", err)
			fmt.Fprintln(os.Stderr, "open this URL manually:", addr+"/")
		}
	}

	<-ctx.Done()
	return 0
}

func printUsage(fs *flag.FlagSet) {
	fmt.Fprint(os.Stderr, `gif-maker — turn a video segment into a high-quality, optimized GIF,
entirely on this machine.

Bare invocation opens a local browser UI: drop a video file, drag a
timeline to pick a range, export.

Usage:
  gif-maker          open the browser UI

Flags:
`)
	fs.PrintDefaults()
	fmt.Fprint(os.Stderr, `
Requires ffmpeg and gifski on PATH. Neither is bundled.
`)
}
