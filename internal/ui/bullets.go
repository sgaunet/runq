package ui

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/sgaunet/bullets"
)

// Bullets is a Sink backed by the github.com/sgaunet/bullets library. It
// allocates one spinner per command. Commands not yet started have their
// spinner created on OnQueued so the coordinator can lay out lines
// up-front (the library recommends this).
type Bullets struct {
	logger *bullets.Logger

	mu       sync.Mutex
	spinners map[string]*bullets.Spinner
}

// NewBullets creates a Sink writing to w (typically os.Stderr). Color
// handling is delegated to the bullets library; pass an NO_COLOR-aware
// writer if needed.
func NewBullets(w io.Writer) *Bullets {
	return &Bullets{
		logger:   bullets.New(w),
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
	// Create the spinner now so the coordinator allocates a line.
	b.ensure(id, fmt.Sprintf("%s · %s", id, truncate(text, 80)))
}

// OnStart marks that the command with the given id has started running.
func (b *Bullets) OnStart(id, text string) {
	// Spinner already exists from OnQueued; nothing extra to do.
	b.ensure(id, fmt.Sprintf("%s · %s", id, truncate(text, 80)))
}

// OnSuccess marks the command's spinner as succeeded with its exit code and duration.
func (b *Bullets) OnSuccess(id, text string, exitCode int, dur time.Duration) {
	if s := b.get(id); s != nil {
		s.Success(fmt.Sprintf("%s · %s · exit=%d · dur=%s",
			id, truncate(text, 60), exitCode, dur.Round(time.Millisecond)))
	}
}

// OnFailure marks the command's spinner as failed with its exit code and duration.
func (b *Bullets) OnFailure(id, text string, exitCode int, dur time.Duration) {
	if s := b.get(id); s != nil {
		s.Error(fmt.Sprintf("%s · %s · exit=%d · dur=%s",
			id, truncate(text, 60), exitCode, dur.Round(time.Millisecond)))
	}
}

// OnCancelled marks the command's spinner as cancelled with its duration.
func (b *Bullets) OnCancelled(id, text string, dur time.Duration) {
	if s := b.get(id); s != nil {
		s.Error(fmt.Sprintf("%s · %s · cancelled · dur=%s",
			id, truncate(text, 60), dur.Round(time.Millisecond)))
	}
}

// OnTimedOut marks the command's spinner as timed-out with its duration.
func (b *Bullets) OnTimedOut(id, text string, dur time.Duration) {
	if s := b.get(id); s != nil {
		s.Error(fmt.Sprintf("%s · %s · timed-out · dur=%s",
			id, truncate(text, 60), dur.Round(time.Millisecond)))
	}
}

// OnSpawnError marks the command's spinner as failed to spawn with the given error.
func (b *Bullets) OnSpawnError(id, text string, err error) {
	if s := b.get(id); s != nil {
		s.Error(fmt.Sprintf("%s · %s · spawn-error · %v", id, truncate(text, 60), err))
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
