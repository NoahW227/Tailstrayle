package main

import (
	"cmp"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "embed"

	"github.com/energye/systray"
)

//go:embed assets/on-icon.png
var onIcon []byte

//go:embed assets/off-icon.png
var offIcon []byte

// preference is a toggle in the Preferences submenu, mirroring the options in
// the Windows client's Preferences menu that apply on Linux.
type preference struct {
	title   string
	checked func(*tsPrefs) bool
	flag    func(checked bool) string // `tailscale set` flag that makes checked true/false
}

func boolFlag(name string) func(bool) string {
	return func(v bool) string { return fmt.Sprintf("--%s=%t", name, v) }
}

var preferences = []preference{
	{
		title:   "Use Tailscale DNS settings",
		checked: func(p *tsPrefs) bool { return p.CorpDNS },
		flag:    boolFlag("accept-dns"),
	},
	{
		title:   "Use Tailscale subnets",
		checked: func(p *tsPrefs) bool { return p.RouteAll },
		flag:    boolFlag("accept-routes"),
	},
	{
		title:   "Allow incoming connections",
		checked: func(p *tsPrefs) bool { return !p.ShieldsUp },
		flag:    func(v bool) string { return boolFlag("shields-up")(!v) },
	},
	{
		title:   "Allow local network access via exit node",
		checked: func(p *tsPrefs) bool { return p.ExitNodeAllowLANAccess },
		flag:    boolFlag("exit-node-allow-lan-access"),
	},
	{
		title:   "Automatically install updates",
		checked: func(p *tsPrefs) bool { return p.AutoUpdate.Apply != nil && *p.AutoUpdate.Apply },
		flag:    boolFlag("auto-update"),
	},
}

type trayState struct {
	connected bool
	tooltip   string
}

type app struct {
	// mu serializes refreshes and guards everything below. Menu clicks and
	// the poller run on separate goroutines.
	mu   sync.Mutex
	snap snapshot
	tray *trayState // last applied; nil until first render

	account   *item
	tailnet   *item
	tailnets  *choiceList
	connect   *item
	exitNode  *item
	exitNodes *choiceList
	copyIP    *item
	prefsMenu *item
	prefItems []*item
}

func main() {
	systray.Run(onReady, onExit)
}

func addItem(title string) *item {
	return newItem(systray.AddMenuItem(title, ""), title)
}

func onReady() {
	a := &app{}

	systray.SetTitle("Tailstrayle")

	a.account = addItem("")
	a.tailnet = addItem("Tailnet")
	a.tailnets = &choiceList{parent: a.tailnet.mi, onClick: a.switchProfile}
	systray.AddSeparator()

	a.connect = addItem("Connect")
	a.exitNode = addItem("Exit node")
	a.exitNodes = &choiceList{parent: a.exitNode.mi, onClick: a.setExitNode}
	a.copyIP = addItem("Copy IP")
	a.prefsMenu = addItem("Preferences")
	for _, p := range preferences {
		it := newItem(a.prefsMenu.mi.AddSubMenuItemCheckbox(p.title, "", false), p.title)
		it.mi.Click(func() { a.togglePref(p, it) })
		a.prefItems = append(a.prefItems, it)
	}
	systray.AddSeparator()

	refresh := addItem("Refresh")
	quit := addItem("Quit")

	a.connect.mi.Click(a.toggleConnect)
	a.copyIP.mi.Click(a.copyIPToClipboard)
	refresh.mi.Click(a.refresh)
	quit.mi.Click(systray.Quit)

	systray.SetOnClick(func(systray.IMenu) { a.toggleConnect() })

	a.refresh()
	go a.pollLoop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("received signal: %v", sig)
		systray.Quit()
	}()
}

func onExit() {
	log.Println("shutting down")
}

// pollLoop refreshes status on a ticker
func (a *app) pollLoop() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		a.refresh()
	}
}

// refresh pulls fresh state and re-renders the menu
func (a *app) refresh() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.snap = fetchSnapshot()
	a.render()
}

// render updates all menu items and the tray icon to reflect a.snap.
// Must be called with a.mu held.
func (a *app) render() {
	s, p := a.snap.status, a.snap.prefs
	tailnet := a.currentTailnet()

	switch {
	case s == nil:
		a.account.set("Tailscale daemon unavailable", false, false, true)
	case !s.loggedIn():
		a.account.set("Not logged in — run `sudo tailscale login`", false, false, true)
	default:
		name := s.accountName()
		if name == "" {
			name = "Logged in"
		}
		a.account.set(name, false, false, true)
	}

	a.renderTailnets(tailnet)

	switch {
	case !s.loggedIn():
		a.connect.set("Connect", false, false, true)
	case s.connected():
		a.connect.set("Disconnect", true, false, true)
	default:
		a.connect.set("Connect", true, false, true)
	}

	a.renderExitNodes(s, p)

	if ip := s.ip(); s.connected() && ip != "" {
		a.copyIP.set("Copy IP ("+ip+")", true, false, true)
	} else {
		a.copyIP.set("Copy IP", false, false, true)
	}

	prefsEnabled := s.loggedIn() && p != nil
	a.prefsMenu.set("Preferences", prefsEnabled, false, true)
	for i, pref := range preferences {
		a.prefItems[i].set(pref.title, prefsEnabled, p != nil && pref.checked(p), true)
	}

	tray := trayState{connected: s.connected()}
	switch {
	case s == nil:
		tray.tooltip = "Tailscale: daemon unavailable"
	case !s.loggedIn():
		tray.tooltip = "Tailscale: not logged in"
	case !s.connected():
		tray.tooltip = "Tailscale: disconnected"
	default:
		tray.tooltip = "Tailscale: connected"
		if tailnet != "" {
			tray.tooltip += " to " + tailnet
		}
		if ip := s.ip(); ip != "" {
			tray.tooltip += " (" + ip + ")"
		}
	}
	if a.tray == nil || *a.tray != tray {
		if tray.connected {
			systray.SetIcon(onIcon)
		} else {
			systray.SetIcon(offIcon)
		}
		systray.SetTooltip(tray.tooltip)
		a.tray = &tray
	}
}

// currentTailnet returns the name of the active tailnet, or "" if unknown.
// Must be called with a.mu held.
func (a *app) currentTailnet() string {
	if name := a.snap.status.tailnetName(); name != "" {
		return name
	}
	for _, pr := range a.snap.profiles {
		if pr.ID == a.snap.currentProfile {
			return pr.tailnet()
		}
	}
	return ""
}

// renderTailnets lists every logged-in profile (tailnet + account) so the
// user can switch between them, like the account menu in the Windows client.
func (a *app) renderTailnets(current string) {
	var choices []choice
	for _, pr := range a.snap.profiles {
		label := pr.tailnet()
		if pr.Name != "" && pr.Name != label {
			label += " (" + pr.Name + ")"
		}
		choices = append(choices, choice{
			label:   label,
			value:   pr.ID,
			checked: pr.ID == a.snap.currentProfile,
			enabled: true,
		})
	}
	slices.SortFunc(choices, func(x, y choice) int { return cmp.Compare(x.label, y.label) })

	title := "Tailnet: " + current
	if current == "" {
		title = "Switch tailnet"
	}
	a.tailnet.set(title, a.snap.status != nil && len(choices) > 0, false, true)
	a.tailnets.set(choices)
}

func (a *app) renderExitNodes(s *tsStatus, p *tsPrefs) {
	type node struct {
		name    string
		ip      string
		online  bool
		current bool
	}
	var nodes []node
	var current string
	anyOnline := false
	if s != nil {
		for _, peer := range s.Peer {
			isCurrent := peer.ExitNode || (p != nil && p.ExitNodeID != "" && peer.ID == p.ExitNodeID)
			if (!peer.ExitNodeOption && !isCurrent) || len(peer.TailscaleIPs) == 0 {
				continue
			}
			n := node{name: peer.machineName(), ip: peer.TailscaleIPs[0], online: peer.Online, current: isCurrent}
			nodes = append(nodes, n)
			if n.current {
				current = n.name
			}
			if n.online {
				anyOnline = true
			}
		}
	}
	// s.Peer is a map; sort so entries don't shuffle on every poll.
	slices.SortFunc(nodes, func(x, y node) int { return cmp.Compare(x.name, y.name) })

	choices := []choice{{label: "None", value: "", checked: current == "", enabled: true}}
	for _, n := range nodes {
		label := n.name
		if !n.online {
			label += " (offline)"
		}
		// Select by IP: HostName isn't unique within a tailnet.
		choices = append(choices, choice{label: label, value: n.ip, checked: n.current, enabled: n.online})
	}
	a.exitNodes.set(choices)

	// Menu item tooltips aren't part of the D-Bus menu protocol, so the
	// reason the submenu is disabled goes in its label instead.
	switch {
	case !s.loggedIn():
		a.exitNode.set("Exit node", false, false, true)
	case current != "":
		a.exitNode.set("Exit node: "+current, true, false, true)
	case !anyOnline:
		a.exitNode.set("Exit node (none available)", false, false, true)
	default:
		a.exitNode.set("Exit node", true, false, true)
	}
}

// run executes a tailscale CLI command in the background, reports failure as
// a desktop notification, and refreshes the menu when done.
func (a *app) run(what string, args ...string) {
	go func() {
		if err := tailscale(args...); err != nil {
			notifyError(what+" failed", err)
		}
		a.refresh()
	}()
}

func (a *app) toggleConnect() {
	a.mu.Lock()
	s := a.snap.status
	a.mu.Unlock()

	switch {
	case !s.loggedIn():
		log.Println("not logged in; ignoring toggle")
	case s.connected():
		a.run("Disconnect", "down")
	default:
		a.run("Connect", "up")
	}
}

func (a *app) switchProfile(id string) {
	a.mu.Lock()
	a.tailnets.markStale()
	current := a.snap.currentProfile
	a.mu.Unlock()

	if id == current {
		go a.refresh()
		return
	}
	a.run("Switch tailnet", "switch", id)
}

func (a *app) setExitNode(ip string) {
	a.mu.Lock()
	a.exitNodes.markStale()
	a.mu.Unlock()

	a.run("Set exit node", "set", "--exit-node="+ip)
}

func (a *app) togglePref(pref preference, it *item) {
	a.mu.Lock()
	it.markStale()
	p := a.snap.prefs
	a.mu.Unlock()

	if p == nil {
		go a.refresh()
		return
	}
	a.run("Change preference", "set", pref.flag(!pref.checked(p)))
}

func (a *app) copyIPToClipboard() {
	a.mu.Lock()
	ip := a.snap.status.ip()
	a.mu.Unlock()
	if ip == "" {
		return
	}
	if err := copyToClipboard(ip); err != nil {
		notifyError("Copy IP failed", err)
	}
}

func copyToClipboard(text string) error {
	// Try wl-copy first, then xclip.
	for _, candidate := range [][]string{
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
	} {
		cmd := exec.Command(candidate[0], candidate[1:]...)
		cmd.Stdin = strings.NewReader(text)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no clipboard tool worked (install wl-clipboard or xclip)")
}
