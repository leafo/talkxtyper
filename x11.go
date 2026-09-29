package main

import (
	"fmt"
	"log"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

// x11Display is the X connection used to read modifier state and to
// synthesize typing.
type x11Display struct {
	conn     *xgb.Conn
	root     xproto.Window
	xtestErr error

	// typingMu serializes typing so concurrent edits can't interleave keys or
	// claim the same spare keycode.
	typingMu sync.Mutex
	// remapped holds spare keycodes we assigned keysyms to. They keep their
	// keysyms so later text reuses them, and are recycled when no unmapped
	// keycode is left.
	remapped map[xproto.Keycode]bool
}

var (
	x11Once sync.Once
	x11     *x11Display
	x11Err  error
)

func x11Connection() (*x11Display, error) {
	x11Once.Do(func() {
		conn, err := xgb.NewConn()
		if err != nil {
			x11Err = fmt.Errorf("connecting to X: %w", err)
			log.Printf("%v\n", x11Err)
			return
		}
		x11 = newX11Display(conn)
	})
	if x11Err != nil {
		return nil, x11Err
	}
	return x11, nil
}

func newX11Display(conn *xgb.Conn) *x11Display {
	return &x11Display{
		conn:     conn,
		root:     xproto.Setup(conn).DefaultScreen(conn).Root,
		xtestErr: xtest.Init(conn),
		remapped: map[xproto.Keycode]bool{},
	}
}

// drainEvents discards queued events. The connection selects no input, but
// every client receives MappingNotify (including for our own remaps), and xgb
// stops reading replies once its event buffer fills.
func (x *x11Display) drainEvents() {
	for {
		ev, err := x.conn.PollForEvent()
		if ev == nil && err == nil {
			return
		}
	}
}
