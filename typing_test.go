package main

import (
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

func TestPlanKeystrokes(t *testing.T) {
	m := keymap{minKeycode: 10, keysymsPerKeycode: 2, keysyms: []xproto.Keysym{
		'a', 'A', // 10
		keysymShiftL, 0, // 11
		keysymBackSpace, 0, // 12
		0, 0, // 13
		0, 0, // 14
		'b', 'B', // 15
	}}

	plan, err := planKeystrokes(m, textKeysyms(1, "aAéb世é"), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantStrokes := []keystroke{{12, false}, {10, false}, {10, true}, {13, false}, {15, false}, {14, false}, {13, false}}
	wantRemaps := map[xproto.Keycode]xproto.Keysym{13: runeKeysym('é'), 14: runeKeysym('世')}
	if !reflect.DeepEqual(plan.strokes, wantStrokes) || !reflect.DeepEqual(plan.remaps, wantRemaps) || plan.shiftKeycode != 11 {
		t.Errorf("plan = %+v, want strokes %v remaps %v shift 11", plan, wantStrokes, wantRemaps)
	}

	if _, err := planKeystrokes(m, textKeysyms(0, "é世ü"), nil); err == nil {
		t.Error("expected an error when more keysyms are missing than keycodes are free")
	}
}

func TestPlanKeystrokesRecyclesOwnKeycodes(t *testing.T) {
	// 11 and 12 hold keysyms from earlier remaps; no keycode is unmapped.
	m := keymap{minKeycode: 10, keysymsPerKeycode: 2, keysyms: []xproto.Keysym{
		'a', 'A',
		runeKeysym('é'), runeKeysym('é'),
		runeKeysym('ü'), runeKeysym('ü'),
	}}
	recyclable := map[xproto.Keycode]bool{11: true, 12: true}

	plan, err := planKeystrokes(m, textKeysyms(0, "é世"), recyclable)
	if err != nil {
		t.Fatal(err)
	}
	wantStrokes := []keystroke{{11, false}, {12, false}}
	wantRemaps := map[xproto.Keycode]xproto.Keysym{12: runeKeysym('世')}
	if !reflect.DeepEqual(plan.strokes, wantStrokes) || !reflect.DeepEqual(plan.remaps, wantRemaps) {
		t.Errorf("plan = %+v, want strokes %v remaps %v", plan, wantStrokes, wantRemaps)
	}
}

func TestPlanKeystrokesWithoutShiftKey(t *testing.T) {
	m := keymap{minKeycode: 10, keysymsPerKeycode: 2, keysyms: []xproto.Keysym{
		'a', 'A',
		0, 0,
	}}
	plan, err := planKeystrokes(m, textKeysyms(0, "A"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []keystroke{{11, false}}; !reflect.DeepEqual(plan.strokes, want) {
		t.Errorf("strokes = %v, want %v", plan.strokes, want)
	}
}

// TestTypingIntoXvfb types into a window on a private Xvfb server, decoding
// the key events the way a client would, including refetching the keyboard
// mapping on MappingNotify.
func TestTypingIntoXvfb(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Xvfb test in short mode")
	}
	if _, err := exec.LookPath("Xvfb"); err != nil {
		t.Skip("Xvfb not installed")
	}

	display := startXvfb(t)

	typerConn, err := xgb.NewConnDisplay(display)
	if err != nil {
		t.Fatal(err)
	}
	defer typerConn.Close()
	typer := newX11Display(typerConn)

	client := newKeyEventClient(t, display)
	defer client.conn.Close()

	text := "Hello, World! héllo — “quotes” 世界 ✓\ttab"
	if err := typer.typeKeysyms(textKeysyms(0, text)); err != nil {
		t.Fatal(err)
	}
	client.waitFor(t, text)

	// A live correction: erase "✓\ttab" and type a replacement.
	if err := typer.typeKeysyms(textKeysyms(5, "ünïcode ✔")); err != nil {
		t.Fatal(err)
	}
	client.waitFor(t, "Hello, World! héllo — “quotes” 世界 ünïcode ✔")
}

func startXvfb(t *testing.T) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cmd := exec.Command("Xvfb", "-displayfd", "3", "-nolisten", "tcp")
	cmd.ExtraFiles = []*os.File{w}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	w.Close()
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})

	buf := make([]byte, 16)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("reading Xvfb display number: %v", err)
	}
	number, err := strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	if err != nil {
		t.Fatalf("parsing Xvfb display number %q: %v", buf[:n], err)
	}
	return ":" + strconv.Itoa(number)
}

type keyEventClient struct {
	conn    *xgb.Conn
	mapping keymap
	text    []rune
}

func newKeyEventClient(t *testing.T, display string) *keyEventClient {
	t.Helper()
	conn, err := xgb.NewConnDisplay(display)
	if err != nil {
		t.Fatal(err)
	}
	c := &keyEventClient{conn: conn}
	screen := xproto.Setup(conn).DefaultScreen(conn)
	window, err := xproto.NewWindowId(conn)
	if err != nil {
		t.Fatal(err)
	}
	xproto.CreateWindow(conn, screen.RootDepth, window, screen.Root, 0, 0, 100, 100, 0,
		xproto.WindowClassInputOutput, screen.RootVisual,
		xproto.CwEventMask, []uint32{xproto.EventMaskKeyPress | xproto.EventMaskStructureNotify})
	xproto.MapWindow(conn, window)
	for {
		ev, err := conn.WaitForEvent()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := ev.(xproto.MapNotifyEvent); ok {
			break
		}
	}
	if err := xproto.SetInputFocusChecked(conn, xproto.InputFocusParent, window, xproto.TimeCurrentTime).Check(); err != nil {
		t.Fatal(err)
	}
	c.refreshMapping(t)
	return c
}

func (c *keyEventClient) refreshMapping(t *testing.T) {
	setup := xproto.Setup(c.conn)
	reply, err := xproto.GetKeyboardMapping(c.conn, setup.MinKeycode, byte(setup.MaxKeycode-setup.MinKeycode+1)).Reply()
	if err != nil {
		t.Fatal(err)
	}
	c.mapping = keymap{minKeycode: setup.MinKeycode, keysymsPerKeycode: int(reply.KeysymsPerKeycode), keysyms: reply.Keysyms}
}

// waitFor processes key events until the decoded text equals want.
func (c *keyEventClient) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for string(c.text) != want {
		if time.Now().After(deadline) {
			t.Fatalf("typed text = %q, want %q", string(c.text), want)
		}
		ev, err := c.conn.PollForEvent()
		if err != nil {
			t.Fatal(err)
		}
		switch ev := ev.(type) {
		case nil:
			time.Sleep(time.Millisecond)
		case xproto.MappingNotifyEvent:
			c.refreshMapping(t)
		case xproto.KeyPressEvent:
			level := 0
			if ev.State&xproto.ModMaskShift != 0 {
				level = 1
			}
			sym := c.mapping.keycodeKeysyms(int(ev.Detail - c.mapping.minKeycode))[level]
			switch {
			case sym == keysymShiftL:
			case sym == keysymBackSpace:
				c.text = c.text[:len(c.text)-1]
			case sym == keysymTab:
				c.text = append(c.text, '\t')
			case sym&0xff000000 == 0x01000000:
				c.text = append(c.text, rune(sym&0xffffff))
			default:
				c.text = append(c.text, rune(sym))
			}
		}
	}
}
