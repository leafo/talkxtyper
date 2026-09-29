package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestModifierWaitRelease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	checks := 0
	err := waitForModifierRelease(ctx, func() (uint16, bool) {
		checks++
		if checks < 3 {
			return 1, true
		}
		return 0, true
	})
	if err != nil || checks != 3 {
		t.Fatalf("wait = %v after %d checks", err, checks)
	}
}

func TestModifierWaitCanceledBeforeQuery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitForModifierRelease(ctx, func() (uint16, bool) {
		t.Fatal("queried modifiers after cancellation")
		return 0, true
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("wait = %v, want canceled", err)
	}
}
