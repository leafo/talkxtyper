package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// heldModifierMask covers the modifiers that turn typed characters into
// shortcuts. Lock (Caps Lock) and Mod2 (Num Lock) are latched states, not
// held keys, so they are ignored.
const heldModifierMask = xproto.ModMaskShift | xproto.ModMaskControl | xproto.ModMask1 | xproto.ModMask4

const modifierReleasePoll = 10 * time.Millisecond

var (
	modifierConnOnce sync.Once
	modifierConn     *xgb.Conn
	modifierRoot     xproto.Window
)

func heldModifiers() (uint16, bool) {
	modifierConnOnce.Do(func() {
		conn, err := xgb.NewConn()
		if err != nil {
			log.Printf("Could not connect to X to check held modifiers: %v\n", err)
			return
		}
		modifierConn = conn
		modifierRoot = xproto.Setup(conn).DefaultScreen(conn).Root
	})
	if modifierConn == nil {
		return 0, false
	}

	reply, err := xproto.QueryPointer(modifierConn, modifierRoot).Reply()
	if err != nil || reply == nil {
		return 0, false
	}
	return reply.Mask & heldModifierMask, true
}

// waitForModifiersReleased pauses until modifiers are released or typing is
// canceled. A held modifier must never be bypassed because time has elapsed.
func waitForModifiersReleased(ctx context.Context) error {
	return waitForModifierRelease(ctx, heldModifiers)
}

func waitForModifierRelease(ctx context.Context, held func() (uint16, bool)) error {
	start := time.Now()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		mask, ok := held()
		if !ok || mask == 0 {
			if waited := time.Since(start); waited >= modifierReleasePoll {
				log.Printf("Waited %s for modifier keys to be released before typing\n", waited.Round(time.Millisecond))
			}
			return ctx.Err()
		}
		timer := time.NewTimer(modifierReleasePoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
