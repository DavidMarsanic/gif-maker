// Package server exposes the gif-maker engine over a small JSON+SSE HTTP
// API, bound to loopback only, for the embedded browser-based UI.
package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/DavidMarsanic/gif-maker/engine"
	"github.com/DavidMarsanic/gif-maker/internal/jobs"
	"github.com/DavidMarsanic/gif-maker/web"
)

const idleTimeout = 30 * time.Minute

// maxUploadBytes caps one request's total multipart size — generous for
// a short clip someone's about to turn into a GIF, while still refusing
// an accidental multi-GB upload outright.
const maxUploadBytes = 1 << 30 // 1GB

type Server struct {
	Engine           *engine.Engine
	Jobs             *jobs.Registry
	DefaultOutputDir string

	ctx context.Context

	lastActivity atomic.Int64
}

func New(ctx context.Context, eng *engine.Engine, defaultOutputDir string) *Server {
	s := &Server{
		ctx:              ctx,
		Engine:           eng,
		Jobs:             jobs.NewRegistry(),
		DefaultOutputDir: defaultOutputDir,
	}
	s.touch()
	return s
}

func (s *Server) Start(port int) (string, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return "", fmt.Errorf("starting local server: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/jobs", s.handleCreateJob)
	mux.HandleFunc("GET /api/jobs/{id}/events", s.handleJobEvents)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", s.handleJobCancel)
	mux.HandleFunc("POST /api/reveal", s.handleReveal)
	mux.HandleFunc("POST /api/open", s.handleOpen)
	mux.Handle("GET /", http.FileServer(http.FS(web.Static)))

	httpSrv := &http.Server{Handler: s.trackActivity(mux)}
	go func() {
		_ = httpSrv.Serve(ln)
	}()
	go s.watchIdle()

	return "http://" + ln.Addr().String(), nil
}

func (s *Server) trackActivity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.touch()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) touch() {
	s.lastActivity.Store(time.Now().Unix())
}

func (s *Server) watchIdle() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		idleFor := time.Now().Unix() - s.lastActivity.Load()
		if idleFor > int64(idleTimeout.Seconds()) && !s.Jobs.HasActive() {
			os.Exit(0)
		}
	}
}
