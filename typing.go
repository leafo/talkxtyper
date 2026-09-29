package main

import (
	"context"

	"github.com/go-vgo/robotgo"
)

// typeCharDelayMillis is the extra pause robotgo adds after each typed
// character. robotgo's X11 backend already holds every key down for 5 ms, so
// this is set to zero for the fastest typing that still keeps events in order.
const typeCharDelayMillis = 0

// typeString types input once any held modifier keys are released, so the
// synthesized keystrokes aren't turned into shortcuts by a still-held hotkey
// modifier (e.g. the Alt of Alt+B).
func typeString(ctx context.Context, input string) error {
	if err := waitForModifiersReleased(ctx); err != nil {
		return err
	}
	robotgo.TypeStr(input, 0, typeCharDelayMillis)
	return nil
}

// typeBackspaces erases count characters. robotgo pauses KeySleep (10 ms by
// default) after every tap, which makes live corrections visibly lag, so the
// pause is shortened for the duration of the run.
func typeBackspaces(ctx context.Context, count int) error {
	if err := waitForModifiersReleased(ctx); err != nil {
		return err
	}
	previousKeySleep := robotgo.KeySleep
	robotgo.KeySleep = 1
	defer func() { robotgo.KeySleep = previousKeySleep }()
	for i := 0; i < count; i++ {
		_ = robotgo.KeyTap("backspace")
	}
	return nil
}

// syncLiveTyping edits the on-screen text from typed to target with
// backspaces and typing.
func syncLiveTyping(ctx context.Context, typed, target string) error {
	backspaces, suffix := liveTypingPlan(typed, target)
	if backspaces > 0 {
		if err := typeBackspaces(ctx, backspaces); err != nil {
			return err
		}
	}
	if suffix != "" {
		return typeString(ctx, suffix)
	}
	return nil
}
