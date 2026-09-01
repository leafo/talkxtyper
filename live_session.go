package main

import (
	"sync"
	"unicode/utf8"
)

// liveTextBuffer holds the text the typer should currently have on screen.
//
// Providers publish snapshots here instead of deltas so revisable text (such
// as Gemini's interim hypotheses) can be corrected: the typer diffs the
// snapshot against what it has typed and backspaces the changed tail. The
// snapshot lives behind a mutex instead of a channel so the socket reader
// never blocks behind keyboard injection; a slow consumer simply skips
// intermediate revisions.
type liveTextBuffer struct {
	mu    sync.Mutex
	text  string
	ready chan struct{}
}

func newLiveTextBuffer() *liveTextBuffer {
	return &liveTextBuffer{ready: make(chan struct{}, 1)}
}

// setLiveText replaces the current snapshot.
func (d *liveTextBuffer) setLiveText(text string) {
	d.mu.Lock()
	d.text = text
	d.mu.Unlock()
	d.signalReady()
}

// appendLiveText extends the current snapshot with append-only text.
func (d *liveTextBuffer) appendLiveText(text string) {
	d.mu.Lock()
	d.text += text
	d.mu.Unlock()
	d.signalReady()
}

func (d *liveTextBuffer) signalReady() {
	select {
	case d.ready <- struct{}{}:
	default:
	}
}

// liveText returns the text the typer should currently show.
func (d *liveTextBuffer) liveText() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.text
}

func (d *liveTextBuffer) liveTextReadyCh() <-chan struct{} {
	return d.ready
}

// liveTypingPlan computes how to turn the typed text into target: erase
// backspaces characters from the end of typed, then type suffix. The common
// prefix is compared by rune so a multi-byte character is never split.
func liveTypingPlan(typed, target string) (backspaces int, suffix string) {
	prefix := 0
	for prefix < len(typed) && prefix < len(target) {
		typedRune, typedSize := utf8.DecodeRuneInString(typed[prefix:])
		targetRune, targetSize := utf8.DecodeRuneInString(target[prefix:])
		if typedRune != targetRune || typedSize != targetSize {
			break
		}
		prefix += typedSize
	}
	return utf8.RuneCountInString(typed[prefix:]), target[prefix:]
}

// pcmChunker batches recorded samples into the fixed-size chunks the streaming
// APIs expect, holding back the remainder until it is filled or flushed.
type pcmChunker struct {
	chunkSamples int
	pending      []int16
}

func (c *pcmChunker) append(samples []int16, write func([]int16) error) error {
	c.pending = append(c.pending, samples...)
	// chunkSamples must be positive; guarding it keeps a zero value from
	// looping forever on empty chunks.
	for c.chunkSamples > 0 && len(c.pending) >= c.chunkSamples {
		if err := write(c.pending[:c.chunkSamples]); err != nil {
			return err
		}
		c.pending = c.pending[c.chunkSamples:]
	}
	return nil
}

// flush writes any partial chunk left over at the end of a recording.
func (c *pcmChunker) flush(write func([]int16) error) error {
	if len(c.pending) == 0 {
		return nil
	}
	if err := write(c.pending); err != nil {
		return err
	}
	c.pending = nil
	return nil
}
