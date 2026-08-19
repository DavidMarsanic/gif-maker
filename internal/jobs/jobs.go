// Package jobs tracks in-flight GIF conversions so the HTTP layer can
// stream their progress over SSE and support cancellation. State is
// in-memory only — this is a disposable local tool, not a service.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Event is one step of a job's progress, matching the terminal fields
// (Path/Filename on success, Code on failure).
type Event struct {
	Stage    string  `json:"stage"` // processing, done, error, canceled
	Percent  float64 `json:"percent"`
	Message  string  `json:"message,omitempty"`
	Path     string  `json:"path,omitempty"`
	Filename string  `json:"filename,omitempty"`
	Code     string  `json:"code,omitempty"`
}

// Job is one running or finished operation.
type Job struct {
	ID     string
	Cancel context.CancelFunc

	mu      sync.Mutex
	history []Event
	done    bool
	subs    map[chan Event]struct{}
}

// Registry holds all jobs created during this process's lifetime.
type Registry struct {
	mu   sync.Mutex
	jobs map[string]*Job
}

func NewRegistry() *Registry {
	return &Registry{jobs: map[string]*Job{}}
}

// Create registers a new job and returns it along with a context that's
// canceled when Job.Cancel is called or the parent ctx ends.
func (r *Registry) Create(parent context.Context) (*Job, context.Context) {
	ctx, cancel := context.WithCancel(parent)
	job := &Job{
		ID:     newID(),
		Cancel: cancel,
		subs:   map[chan Event]struct{}{},
	}
	r.mu.Lock()
	r.jobs[job.ID] = job
	r.mu.Unlock()

	// Jobs are cheap and this tool is short-lived, but free memory for
	// long sessions with many operations.
	time.AfterFunc(30*time.Minute, func() {
		r.mu.Lock()
		delete(r.jobs, job.ID)
		r.mu.Unlock()
	})

	return job, ctx
}

func (r *Registry) Get(id string) (*Job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	return j, ok
}

// HasActive reports whether any job is still running — used to hold off
// the idle-timeout shutdown so a long-running operation with no other page
// interaction never gets killed out from under itself.
func (r *Registry) HasActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, j := range r.jobs {
		j.mu.Lock()
		done := j.done
		j.mu.Unlock()
		if !done {
			return true
		}
	}
	return false
}

// Publish appends the event to the job's history and fans it out to any
// live subscribers (SSE connections).
func (j *Job) Publish(e Event) {
	j.mu.Lock()
	j.history = append(j.history, e)
	if e.Stage == "done" || e.Stage == "error" || e.Stage == "canceled" {
		j.done = true
	}
	subs := make([]chan Event, 0, len(j.subs))
	for ch := range j.subs {
		subs = append(subs, ch)
	}
	j.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- e:
		default: // slow subscriber; drop rather than block the job
		}
	}
}

// Subscribe returns a channel carrying every event published so far,
// followed by any future ones. A job that finishes fast can complete
// before the browser's EventSource connection even opens; replaying only
// the *last* event (the previous behavior) meant a fast job's whole
// progress history was invisible to a late subscriber, and only the
// terminal event ever arrived. The channel is registered into subs
// (making it eligible for live events) only after the full history has
// been queued into its buffer, all under the same lock Publish uses — so
// no event can be delivered twice or dropped in the handoff between
// "replay" and "live". The returned cancel func must be called when done
// reading.
func (j *Job) Subscribe() (<-chan Event, func()) {
	j.mu.Lock()
	ch := make(chan Event, len(j.history)+32)
	for _, e := range j.history {
		ch <- e // never blocks: capacity was sized to fit the whole history
	}
	done := j.done
	if !done {
		j.subs[ch] = struct{}{}
	}
	j.mu.Unlock()

	if done {
		close(ch)
		return ch, func() {}
	}

	cancel := func() {
		j.mu.Lock()
		delete(j.subs, ch)
		j.mu.Unlock()
	}
	return ch, cancel
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
