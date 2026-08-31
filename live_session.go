package main

import (
	"strings"
	"sync"
)

// liveDeltaBuffer accumulates transcript text for the typer.
//
// Delta text accumulates here instead of a channel so the socket reader never
// blocks behind keyboard injection; a slow consumer just takes a larger
// coalesced chunk on its next read.
type liveDeltaBuffer struct {
	mu    sync.Mutex
	buf   strings.Builder
	ready chan struct{}
}

func newLiveDeltaBuffer() *liveDeltaBuffer {
	return &liveDeltaBuffer{ready: make(chan struct{}, 1)}
}

func (d *liveDeltaBuffer) queueDelta(text string) {
	d.mu.Lock()
	d.buf.WriteString(text)
	d.mu.Unlock()
	select {
	case d.ready <- struct{}{}:
	default:
	}
}

// takeDeltas returns and clears the transcript text accumulated since the
// last call.
func (d *liveDeltaBuffer) takeDeltas() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	text := d.buf.String()
	d.buf.Reset()
	return text
}

func (d *liveDeltaBuffer) deltaReadyCh() <-chan struct{} {
	return d.ready
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
