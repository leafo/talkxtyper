package main

import (
	"log"
	"sync"

	"github.com/godbus/dbus/v5"
)

const notificationAppName = "TalkXTyper"

var (
	notifyOnce sync.Once
	notifyConn *dbus.Conn

	notifyMu sync.Mutex
	// Errors replace each other rather than stacking up: only the most recent
	// failure is worth showing.
	lastNotificationID uint32
)

// notifyError shows a desktop notification so failures are visible without
// watching the log. Notifications are best effort; a missing or broken
// notification daemon must never affect transcription.
func notifyError(summary string, err error) {
	log.Printf("%s: %v\n", summary, err)

	notifyOnce.Do(func() {
		conn, connErr := dbus.SessionBus()
		if connErr != nil {
			log.Printf("Desktop notifications unavailable: %v\n", connErr)
			return
		}
		notifyConn = conn
	})
	if notifyConn == nil {
		return
	}

	notifyMu.Lock()
	defer notifyMu.Unlock()

	object := notifyConn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	call := object.Call("org.freedesktop.Notifications.Notify", 0,
		notificationAppName,
		lastNotificationID,
		"dialog-error",
		summary,
		err.Error(),
		[]string{},
		map[string]dbus.Variant{},
		int32(-1),
	)
	if call.Err != nil {
		log.Printf("Error sending desktop notification: %v\n", call.Err)
		return
	}
	if storeErr := call.Store(&lastNotificationID); storeErr != nil {
		log.Printf("Error reading desktop notification id: %v\n", storeErr)
	}
}
