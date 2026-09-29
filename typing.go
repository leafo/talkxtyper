package main

import (
	"context"
	"fmt"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

// Text is typed by sending XTest key events directly. robotgo sleeps about
// 5 ms per character and remaps the keyboard for every non-ASCII character,
// which made large live corrections take seconds; sending one batch of events
// with no delays takes milliseconds.

const (
	keysymBackSpace xproto.Keysym = 0xff08
	keysymTab       xproto.Keysym = 0xff09
	keysymReturn    xproto.Keysym = 0xff0d
	keysymShiftL    xproto.Keysym = 0xffe1
)

// typeString types input once any held modifier keys are released, so the
// synthesized keystrokes aren't turned into shortcuts by a still-held hotkey
// modifier (e.g. the Alt of Alt+B).
func typeString(ctx context.Context, input string) error {
	return typeKeysyms(ctx, textKeysyms(0, input))
}

// syncLiveTyping edits the on-screen text from typed to target with
// backspaces and typing, sent as a single batch.
func syncLiveTyping(ctx context.Context, typed, target string) error {
	backspaces, suffix := liveTypingPlan(typed, target)
	return typeKeysyms(ctx, textKeysyms(backspaces, suffix))
}

func typeKeysyms(ctx context.Context, keysyms []xproto.Keysym) error {
	if len(keysyms) == 0 {
		return nil
	}
	if err := waitForModifiersReleased(ctx); err != nil {
		return err
	}
	x, err := x11Connection()
	if err != nil {
		return err
	}
	return x.typeKeysyms(keysyms)
}

func textKeysyms(backspaces int, text string) []xproto.Keysym {
	keysyms := make([]xproto.Keysym, 0, backspaces+len(text))
	for i := 0; i < backspaces; i++ {
		keysyms = append(keysyms, keysymBackSpace)
	}
	for _, r := range text {
		keysyms = append(keysyms, runeKeysym(r))
	}
	return keysyms
}

func runeKeysym(r rune) xproto.Keysym {
	switch {
	case r == '\n':
		return keysymReturn
	case r == '\t':
		return keysymTab
	case (r >= 0x20 && r <= 0x7e) || (r >= 0xa0 && r <= 0xff):
		// ASCII and Latin-1 keysyms equal their code points.
		return xproto.Keysym(r)
	default:
		return xproto.Keysym(0x01000000 | r)
	}
}

// keymap is a snapshot of the core keyboard mapping.
type keymap struct {
	minKeycode        xproto.Keycode
	keysymsPerKeycode int
	keysyms           []xproto.Keysym
}

func (m keymap) keycodeCount() int {
	return len(m.keysyms) / m.keysymsPerKeycode
}

func (m keymap) keycodeKeysyms(i int) []xproto.Keysym {
	return m.keysyms[i*m.keysymsPerKeycode : (i+1)*m.keysymsPerKeycode]
}

type keystroke struct {
	keycode xproto.Keycode
	shift   bool
}

// keystrokePlan is how to type a batch of keysyms: first assign remaps to
// their keycodes, then press strokes, holding shiftKeycode where required.
type keystrokePlan struct {
	strokes      []keystroke
	shiftKeycode xproto.Keycode
	remaps       map[xproto.Keycode]xproto.Keysym
}

// planKeystrokes finds a key for each keysym in the unshifted or shifted
// level of the mapping. Keysyms with no key are assigned to unmapped
// keycodes, then to keycodes in recyclable that this batch doesn't use.
func planKeystrokes(m keymap, keysyms []xproto.Keysym, recyclable map[xproto.Keycode]bool) (keystrokePlan, error) {
	plan := keystrokePlan{remaps: map[xproto.Keycode]xproto.Keysym{}}
	found := map[xproto.Keysym]keystroke{}
	var unmapped []xproto.Keycode
	for level := 0; level < 2 && level < m.keysymsPerKeycode; level++ {
		for i := 0; i < m.keycodeCount(); i++ {
			sym := m.keycodeKeysyms(i)[level]
			if _, ok := found[sym]; sym != 0 && !ok {
				found[sym] = keystroke{keycode: m.minKeycode + xproto.Keycode(i), shift: level == 1}
			}
		}
	}
	for i := 0; i < m.keycodeCount(); i++ {
		empty := true
		for _, sym := range m.keycodeKeysyms(i) {
			empty = empty && sym == 0
		}
		if empty {
			unmapped = append(unmapped, m.minKeycode+xproto.Keycode(i))
		}
	}
	if shift, ok := found[keysymShiftL]; ok && !shift.shift {
		plan.shiftKeycode = shift.keycode
	}

	plan.strokes = make([]keystroke, len(keysyms))
	used := map[xproto.Keycode]bool{}
	var missing []int
	for i, sym := range keysyms {
		stroke, ok := found[sym]
		if !ok || (stroke.shift && plan.shiftKeycode == 0) {
			missing = append(missing, i)
			continue
		}
		plan.strokes[i] = stroke
		used[stroke.keycode] = true
	}

	var spare []xproto.Keycode
	spare = append(spare, unmapped...)
	for i := 0; i < m.keycodeCount(); i++ {
		keycode := m.minKeycode + xproto.Keycode(i)
		if recyclable[keycode] && !used[keycode] {
			spare = append(spare, keycode)
		}
	}

	assigned := map[xproto.Keysym]xproto.Keycode{}
	for _, i := range missing {
		sym := keysyms[i]
		keycode, ok := assigned[sym]
		if !ok {
			if len(spare) == 0 {
				return keystrokePlan{}, fmt.Errorf("no free keycode to type keysym %#x", sym)
			}
			keycode, spare = spare[0], spare[1:]
			assigned[sym] = keycode
			plan.remaps[keycode] = sym
		}
		plan.strokes[i] = keystroke{keycode: keycode}
	}
	return plan, nil
}

func (x *x11Display) typeKeysyms(keysyms []xproto.Keysym) error {
	if x.xtestErr != nil {
		return fmt.Errorf("XTest extension unavailable: %w", x.xtestErr)
	}
	x.typingMu.Lock()
	defer x.typingMu.Unlock()
	x.drainEvents()

	setup := xproto.Setup(x.conn)
	count := byte(setup.MaxKeycode - setup.MinKeycode + 1)
	reply, err := xproto.GetKeyboardMapping(x.conn, setup.MinKeycode, count).Reply()
	if err != nil {
		return fmt.Errorf("reading keyboard mapping: %w", err)
	}
	m := keymap{
		minKeycode:        setup.MinKeycode,
		keysymsPerKeycode: int(reply.KeysymsPerKeycode),
		keysyms:           reply.Keysyms,
	}

	plan, err := planKeystrokes(m, keysyms, x.remapped)
	if err != nil {
		return err
	}

	// Remapped keycodes keep their keysyms rather than being restored after
	// typing: clients fetch the new mapping when they process MappingNotify,
	// which can be after a restore, and they would then misread the keys.
	// The server applies requests in order, so the remap precedes the keys.
	for keycode, sym := range plan.remaps {
		syms := make([]xproto.Keysym, m.keysymsPerKeycode)
		for level := 0; level < 2 && level < len(syms); level++ {
			syms[level] = sym
		}
		xproto.ChangeKeyboardMapping(x.conn, 1, keycode, byte(len(syms)), syms)
		x.remapped[keycode] = true
	}

	fake := func(eventType byte, keycode xproto.Keycode) {
		xtest.FakeInput(x.conn, eventType, byte(keycode), 0, x.root, 0, 0, 0)
	}
	for _, stroke := range plan.strokes {
		if stroke.shift {
			fake(xproto.KeyPress, plan.shiftKeycode)
		}
		fake(xproto.KeyPress, stroke.keycode)
		fake(xproto.KeyRelease, stroke.keycode)
		if stroke.shift {
			fake(xproto.KeyRelease, plan.shiftKeycode)
		}
	}

	// A round trip flushes the batch and waits for the server to process it.
	if _, err := xproto.GetInputFocus(x.conn).Reply(); err != nil {
		return fmt.Errorf("typing: %w", err)
	}
	return nil
}
