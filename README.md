# GIF Maker

Turn a video file you already have into a high-quality, optimized GIF —
entirely on this machine. Opens as its own window.

Uses [gifski](https://github.com/ImageOptim/gifski) — the highest-quality
GIF encoder available, built on pngquant's fancy per-frame palette work —
piped straight from ffmpeg's decoded frames, exactly gifski's own
documented usage. Neither tool is bundled; both are expected on `PATH`.

This is a local-file tool, not a URL downloader — for pulling a video from
the web first, see [video-clipper](https://github.com/DavidMarsanic/video-clipper),
or [clip-and-gif](https://github.com/DavidMarsanic/clip-and-gif), which
combines both in one app.

## Requirements

Two external tools, both expected on `PATH`, neither bundled:

- [`ffmpeg`](https://ffmpeg.org/download.html) — decoding
- [`gifski`](https://github.com/ImageOptim/gifski#download-and-install) — encoding

On macOS: `brew install ffmpeg gifski`.

**A Chromium-based browser already installed**: Google Chrome, Chromium,
Brave, Microsoft Edge, or Arc — renders the app's own UI window.

If anything is missing, the app still opens — it'll tell you what's
missing the moment you try to use it, rather than failing silently on
launch.

## Use

1. Open GIF Maker — it opens its own window.
2. Drop a video file, or click to choose one.
3. Drag the timeline to pick a range (or leave it as the whole video).
4. Pick a width — smaller is the single biggest lever for GIF file size.
5. **Export GIF.**

The result is saved to your Downloads folder.

## License

MIT — see [LICENSE](LICENSE).
