package main

import (
	"sync"

	"github.com/energye/systray"
)

// item wraps a systray.MenuItem and only forwards actual changes. Every
// setter in the systray library emits a D-Bus LayoutUpdated signal, so
// re-applying identical state on each poll makes an open menu flicker.
//
// Methods must be called with app.mu held.
type item struct {
	mi      *systray.MenuItem
	title   string
	enabled bool
	checked bool
	visible bool
	stale   bool // resend all state on next set, see markStale
}

func newItem(mi *systray.MenuItem, title string) *item {
	return &item{mi: mi, title: title, enabled: true, visible: true}
}

func (it *item) setTitle(title string) {
	if it.stale || title != it.title {
		it.title = title
		it.mi.SetTitle(title)
	}
}

func (it *item) setEnabled(enabled bool) {
	if it.stale || enabled != it.enabled {
		it.enabled = enabled
		if enabled {
			it.mi.Enable()
		} else {
			it.mi.Disable()
		}
	}
}

func (it *item) setChecked(checked bool) {
	if it.stale || checked != it.checked {
		it.checked = checked
		if checked {
			it.mi.Check()
		} else {
			it.mi.Uncheck()
		}
	}
}

func (it *item) setVisible(visible bool) {
	if it.stale || visible != it.visible {
		it.visible = visible
		if visible {
			it.mi.Show()
		} else {
			it.mi.Hide()
		}
	}
}

// markStale forces the next set of each property through. Used after a
// click on a checkbox item, since the tray host may toggle its checkmark
// locally before the app has confirmed the change.
func (it *item) markStale() { it.stale = true }

func (it *item) set(title string, enabled, checked, visible bool) {
	it.setVisible(visible)
	it.setTitle(title)
	it.setEnabled(enabled)
	it.setChecked(checked)
	it.stale = false
}

type choice struct {
	label   string
	value   string
	checked bool
	enabled bool
}

// choiceList is a variable-length list of checkbox items in a submenu. The
// systray library can't remove menu items, so the list grows by appending
// items and shrinks by hiding the surplus.
type choiceList struct {
	parent  *systray.MenuItem
	onClick func(value string) // called without app.mu held

	items []*item

	valuesMu sync.Mutex // guards values and used, which click handlers read
	values   []string
	used     int // number of visible items; clicks on the rest are ignored
}

// set must be called with app.mu held.
func (l *choiceList) set(choices []choice) {
	for len(l.items) < len(choices) {
		idx := len(l.items)
		l.valuesMu.Lock()
		l.values = append(l.values, "")
		l.valuesMu.Unlock()
		mi := l.parent.AddSubMenuItemCheckbox("", "", false)
		mi.Click(func() {
			l.valuesMu.Lock()
			v, ok := l.values[idx], idx < l.used
			l.valuesMu.Unlock()
			// The list may have shrunk while the menu was open.
			if ok {
				l.onClick(v)
			}
		})
		l.items = append(l.items, newItem(mi, ""))
	}

	l.valuesMu.Lock()
	for i := range l.items {
		if i < len(choices) {
			l.values[i] = choices[i].value
		} else {
			l.values[i] = ""
		}
	}
	l.used = len(choices)
	l.valuesMu.Unlock()

	for i, it := range l.items {
		if i < len(choices) {
			c := choices[i]
			it.set(c.label, c.enabled, c.checked, true)
		} else {
			it.setVisible(false)
		}
	}
}

// markStale marks every item stale; see item.markStale.
func (l *choiceList) markStale() {
	for _, it := range l.items {
		it.markStale()
	}
}
