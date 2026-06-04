package ui

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/sgaunet/bullets"
)

// Bullets is a Sink backed by the github.com/sgaunet/bullets library. It
// allocates one spinner per command. Commands not yet started have their
// spinner created on OnQueued so the coordinator can lay out lines up-front
// (the library recommends this). The aligned zone formatting is delegated to
// Layout; the spinner glyph conveys status, so no textual status word is drawn.
type Bullets struct {
	logger *bullets.Logger
	l      Layout

	mu       sync.Mutex
	spinners map[string]*bullets.Spinner
}

// NewBullets creates a Sink writing to w (typically os.Stderr) with the given
// Layout. Color handling is delegated to the bullets library; pass an
// NO_COLOR-aware writer if needed.
func NewBullets(w io.Writer, l Layout) *Bullets {
	return &Bullets{
		logger:   bullets.New(w),
		l:        l,
		spinners: map[string]*bullets.Spinner{},
	}
}

func (b *Bullets) get(id string) *bullets.Spinner {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.spinners[id]
}

func (b *Bullets) ensure(id, label string) *bullets.Spinner {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := b.spinners[id]; ok {
		return s
	}
	s := b.logger.Spinner(context.Background(), label)
	b.spinners[id] = s
	return s
}

// OnQueued creates the spinner for the command so its line is allocated up-front.
func (b *Bullets) OnQueued(id, text string) {
	b.ensure(id, b.l.Render(id, StatusQueued, text, nil, nil, ""))
}

// OnStart marks that the command with the given id has started running.
func (b *Bullets) OnStart(id, text string) {
	// Spinner already exists from OnQueued; ensure keeps it idempotent.
	b.ensure(id, b.l.Render(id, StatusStarted, text, nil, nil, ""))
}

// OnSuccess marks the command's spinner as succeeded with its exit code and duration.
func (b *Bullets) OnSuccess(id, text string, exitCode int, dur time.Duration) {
	if s := b.get(id); s != nil {
		s.Success(b.l.Render(id, StatusSuccess, text, &exitCode, &dur, ""))
	}
}

// OnFailure marks the command's spinner as failed with its exit code and duration.
func (b *Bullets) OnFailure(id, text string, exitCode int, dur time.Duration) {
	if s := b.get(id); s != nil {
		s.Error(b.l.Render(id, StatusFailure, text, &exitCode, &dur, ""))
	}
}

// OnCancelled marks the command's spinner as cancelled with its duration.
func (b *Bullets) OnCancelled(id, text string, dur time.Duration) {
	if s := b.get(id); s != nil {
		s.Error(b.l.Render(id, StatusCancelled, text, nil, &dur, ""))
	}
}

// OnTimedOut marks the command's spinner as timed-out with its duration.
func (b *Bullets) OnTimedOut(id, text string, dur time.Duration) {
	if s := b.get(id); s != nil {
		s.Error(b.l.Render(id, StatusTimedOut, text, nil, &dur, ""))
	}
}

// OnSpawnError marks the command's spinner as failed to spawn with the given
// error as a trailing, bounded note.
func (b *Bullets) OnSpawnError(id, text string, err error) {
	if s := b.get(id); s != nil {
		s.Error(b.l.Render(id, StatusSpawnError, text, nil, nil, "err="+err.Error()))
	}
}

// Close stops all spinners and releases the tracked UI resources.
func (b *Bullets) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.spinners {
		if s != nil {
			s.Stop()
		}
	}
	b.spinners = nil
	return nil
}
