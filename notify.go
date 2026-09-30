package main

import (
	"log"

	"github.com/godbus/dbus/v5"
)

// notifyError shows a desktop notification. Tailstrayle usually runs from
// autostart with no visible stderr, so failures of user-initiated actions
// (e.g. missing operator permission) would otherwise go unnoticed.
func notifyError(summary string, err error) {
	log.Printf("%s: %v", summary, err)

	conn, cerr := dbus.SessionBus()
	if cerr != nil {
		log.Printf("notification failed: %v", cerr)
		return
	}
	call := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").Call(
		"org.freedesktop.Notifications.Notify", 0,
		"Tailstrayle",             // app_name
		uint32(0),                 // replaces_id
		"network-vpn",             // app_icon
		summary,                   // summary
		err.Error(),               // body
		[]string{},                // actions
		map[string]dbus.Variant{}, // hints
		int32(-1),                 // expire_timeout: server default
	)
	if call.Err != nil {
		log.Printf("notification failed: %v", call.Err)
	}
}
