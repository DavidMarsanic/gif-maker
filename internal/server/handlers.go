package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/DavidMarsanic/gif-maker/engine"
	"github.com/DavidMarsanic/gif-maker/internal/browser"
	"github.com/DavidMarsanic/gif-maker/internal/jobs"
)

// handleCreateJob accepts a multipart upload of one local video file plus
// optional start/end/width fields, and runs the ffmpeg|gifski conversion
// in the background, reporting progress over SSE.
func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid upload", "code": "bad-request"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no file uploaded", "code": "bad-request"})
		return
	}
	defer file.Close()

	var start, end *time.Duration
	if v := r.FormValue("start"); v != "" {
		if secs, err := strconv.ParseFloat(v, 64); err == nil {
			d := time.Duration(secs * float64(time.Second))
			start = &d
		}
	}
	if v := r.FormValue("end"); v != "" {
		if secs, err := strconv.ParseFloat(v, 64); err == nil {
			d := time.Duration(secs * float64(time.Second))
			end = &d
		}
	}
	var width int
	if v := r.FormValue("width"); v != "" {
		width, _ = strconv.Atoi(v)
	}

	job, ctx := s.Jobs.Create(s.ctx)
	scratch, err := os.MkdirTemp("", "gif-maker-*")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	name := sanitizeFilename(header.Filename)
	if name == "" {
		name = "input.mp4"
	}
	srcPath := filepath.Join(scratch, name)
	dst, err := os.Create(srcPath)
	if err != nil {
		os.RemoveAll(scratch)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		os.RemoveAll(scratch)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "saving upload: " + err.Error()})
		return
	}
	dst.Close()

	go func() {
		defer os.RemoveAll(scratch)

		onProgress := func(p engine.Progress) {
			job.Publish(jobs.Event{Stage: p.Stage, Message: p.Message})
		}
		result, err := s.Engine.Convert(ctx, srcPath, start, end, engine.Options{Width: width}, s.DefaultOutputDir, onProgress)
		if err != nil {
			if ctx.Err() != nil {
				job.Publish(jobs.Event{Stage: "canceled"})
				return
			}
			code := "error"
			if errors.Is(err, engine.ErrMissingDependency) {
				code = "missing-tool"
			}
			job.Publish(jobs.Event{Stage: "error", Message: err.Error(), Code: code})
			return
		}
		job.Publish(jobs.Event{Stage: "done", Percent: 100, Path: result.Path, Filename: result.Filename})
	}()

	writeJSON(w, http.StatusOK, map[string]string{"jobId": job.ID})
}

func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	if name == "." || name == ".." || name == "" {
		return ""
	}
	return name
}

func (s *Server) handleJobEvents(w http.ResponseWriter, r *http.Request) {
	job, ok := s.Jobs.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, cancel := job.Subscribe()
	defer cancel()

	for {
		select {
		case e, open := <-ch:
			if !open {
				return
			}
			data, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			if e.Stage == "done" || e.Stage == "error" || e.Stage == "canceled" {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleJobCancel(w http.ResponseWriter, r *http.Request) {
	job, ok := s.Jobs.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	job.Cancel()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReveal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := browser.Reveal(req.Path); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := browser.Open(req.Path); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body", "code": "bad-request"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
