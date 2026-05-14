package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
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

// Subset of `tailscale status --json` output
type tsStatus struct {
	BackendState string // "Running", "Stopped", "NeedsLogin", "NoState", "Starting"
	Self         tsPeer
	Peer         map[string]tsPeer
	User         map[string]tsUser
	CurrentTailnet *tsTailnet `json:",omitempty"`
}

type tsPeer struct {
	HostName       string
	DNSName        string
	TailscaleIPs   []string
	Online         bool
	ExitNode       bool
	ExitNodeOption bool
	UserID         int64
}

const maxExitNodes = 5

type tsUser struct {
	LoginName   string
	DisplayName string
}

type tsTailnet struct {
	Name string
}

func (s *tsStatus) accountName() string {
	if s == nil {
		return ""
	}
	if u, ok := s.User[fmt.Sprintf("%d", s.Self.UserID)]; ok {
		if u.LoginName != "" {
			return u.LoginName
		}
		return u.DisplayName
	}
	return ""
}

func (s *tsStatus) connected() bool {
	return s != nil && s.BackendState == "Running"
}

func (s *tsStatus) loggedIn() bool {
	if s == nil {
		return false
	}
	switch s.BackendState {
	case "NeedsLogin", "NoState":
		return false
	}
	return true
}

func (s *tsStatus) currentExitNode() string {
	if s == nil {
		return ""
	}
	for _, p := range s.Peer {
		if p.ExitNode {
			return p.HostName
		}
	}
	return ""
}

func fetchStatus() (*tsStatus, error) {
	out, err := exec.Command("tailscale", "status", "--json").Output()
	if err != nil {
		return nil, err
	}
	var s tsStatus
	if err := json.Unmarshal(out, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

type app struct {
	mu     sync.Mutex
	status *tsStatus

	mAccount  *systray.MenuItem
	mConnect  *systray.MenuItem
	mExitMenu *systray.MenuItem
	mCopyIP   *systray.MenuItem
	mRefresh  *systray.MenuItem
	mQuit     *systray.MenuItem

	exitItems   []*systray.MenuItem
	exitTargets []string // parallel slice: which hostname each slot currently targets
}

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	a := &app{}

	systray.SetIcon(offIcon)
	systray.SetTitle("Tailstrayle")
	systray.SetTooltip("Tailscale")

	a.mAccount = systray.AddMenuItem("Not logged in", "")
	a.mAccount.Disable()
	systray.AddSeparator()

	a.mConnect = systray.AddMenuItem("Connect", "Bring Tailscale up")
	a.mExitMenu = systray.AddMenuItem("Exit node", "Choose an exit node")
	a.mCopyIP = systray.AddMenuItem("Copy IP", "Copy this device's Tailscale IP")
	systray.AddSeparator()
	a.mRefresh = systray.AddMenuItem("Refresh", "Refresh status now")
	a.mQuit = systray.AddMenuItem("Quit", "Exit the app")

	a.mConnect.Click(a.toggleConnect)
	a.mCopyIP.Click(a.copyIP)
	a.mRefresh.Click(func() { a.refresh() })
	a.mQuit.Click(func() { systray.Quit() })

	systray.SetOnClick(func(menu systray.IMenu) {
		a.toggleConnect()
	})

	// Initial fetch + render
	a.refresh()

	// Background poller
	go a.pollLoop()

	// Signal handling
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

// refresh pulls fresh status and re-renders the menu
func (a *app) refresh() {
	s, err := fetchStatus()
	if err != nil {
		log.Printf("status fetch failed: %v", err)
		a.mu.Lock()
		a.status = nil
		a.mu.Unlock()
		a.render()
		return
	}
	a.mu.Lock()
	a.status = s
	a.mu.Unlock()
	a.render()
}

// render updates all menu items and the tray icon to reflect current state
func (a *app) render() {
	a.mu.Lock()
	s := a.status
	a.mu.Unlock()

	if s == nil {
		a.mAccount.SetTitle("Tailscale daemon unavailable")
		a.mConnect.Disable()
		a.mExitMenu.Disable()
		a.mCopyIP.Disable()
		systray.SetIcon(offIcon)
		systray.SetTooltip("Tailscale: unavailable")
		return
	}

	// Account label
	if s.loggedIn() {
		name := s.accountName()
		if name == "" {
			name = "Logged in"
		}
		a.mAccount.SetTitle(name)
	} else {
		a.mAccount.SetTitle("Not logged in — run `sudo tailscale login`")
	}

	// Connect toggle
	if !s.loggedIn() {
		a.mConnect.Disable()
		a.mConnect.SetTitle("Connect")
	} else {
		a.mConnect.Enable()
		if s.connected() {
			a.mConnect.SetTitle("Disconnect")
		} else {
			a.mConnect.SetTitle("Connect")
		}
	}

	// Icon + tooltip
	if s.connected() {
		systray.SetIcon(onIcon)
		ip := ""
		if len(s.Self.TailscaleIPs) > 0 {
			ip = s.Self.TailscaleIPs[0]
		}
		if ip != "" {
			systray.SetTooltip("Tailscale: connected (" + ip + ")")
		} else {
			systray.SetTooltip("Tailscale: connected")
		}
		a.mCopyIP.Enable()
	} else {
		systray.SetIcon(offIcon)
		systray.SetTooltip("Tailscale: disconnected")
		a.mCopyIP.Disable()
	}

	// Exit node submenu
	a.rebuildExitNodes(s)
}

func (a *app) rebuildExitNodes(s *tsStatus) {
	if !s.loggedIn() {
		a.mExitMenu.Disable()
		return
	}
	a.mExitMenu.Enable()

	// One-time pre-allocate a fixed pool of submenu items.
	// Slot 0 is always "None" (clear exit node). The rest fill in as
	// exit-node-capable peers appear.
	if len(a.exitItems) == 0 {
		a.exitItems = make([]*systray.MenuItem, maxExitNodes+1)
		a.exitTargets = make([]string, maxExitNodes+1)
		for i := 0; i <= maxExitNodes; i++ {
			mi := a.mExitMenu.AddSubMenuItem("", "")
			idx := i // capture for closure
			mi.Click(func() {
				a.mu.Lock()
				target := a.exitTargets[idx]
				a.mu.Unlock()
				a.setExitNode(target)
			})
			mi.Disable()
			a.exitItems[i] = mi
		}
	}

	// Build the desired list of options
	current := s.currentExitNode()
	type opt struct {
		host string
		cur  bool
	}
	opts := []opt{{host: "", cur: current == ""}} // None
	for _, p := range s.Peer {
		if p.ExitNodeOption {
			opts = append(opts, opt{host: p.HostName, cur: p.HostName == current})
		}
	}

	// Fill in the slots
	a.mu.Lock()
	for i, mi := range a.exitItems {
		if i < len(opts) {
			o := opts[i]
			label := o.host
			if label == "" {
				label = "None"
			}
			if o.cur {
				label = "✓ " + label
			}
			mi.SetTitle(label)
			a.exitTargets[i] = o.host
			mi.Enable()
		} else {
			// Unused slot: attempt to hide
			mi.SetTitle(" ")
			a.exitTargets[i] = ""
			mi.Disable()
		}
	}
	a.mu.Unlock()

	// Log when tailnet has more exit-node-capable peers than pre-allocated slots
	if len(opts) > len(a.exitItems) {
		log.Printf("warning: %d exit nodes available but only %d slots",
			len(opts), len(a.exitItems))
	}
}

func (a *app) toggleConnect() {
	a.mu.Lock()
	s := a.status
	a.mu.Unlock()

	if s == nil || !s.loggedIn() {
		log.Println("not logged in; ignoring toggle")
		return
	}
	if s.connected() {
		if err := run("tailscale", "down"); err != nil {
			log.Printf("down failed: %v", err)
		}
	} else {
		if err := run("tailscale", "up"); err != nil {
			log.Printf("up failed: %v", err)
		}
	}
	a.refresh()
}

func (a *app) copyIP() {
	a.mu.Lock()
	s := a.status
	a.mu.Unlock()
	if s == nil || len(s.Self.TailscaleIPs) == 0 {
		return
	}
	ip := s.Self.TailscaleIPs[0]

	if err := copyToClipboard(ip); err != nil {
		log.Printf("copy failed: %v", err)
	}
}

func copyToClipboard(text string) error {
	// Try wl-copy first, then xclip.
	for _, candidate := range [][]string{
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
	} {
		cmd := exec.Command(candidate[0], candidate[1:]...)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			continue
		}
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			continue
		}
		stdin.Write([]byte(text))
		stdin.Close()
		if err := cmd.Wait(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no clipboard tool worked (install wl-clipboard or xclip)")
}

func (a *app) setExitNode(host string) {
	if host == "" {
		if err := run("tailscale", "set", "--exit-node="); err != nil {
			log.Printf("clear exit node failed: %v", err)
		}
	} else {
		if err := run("tailscale", "set", "--exit-node="+host); err != nil {
			log.Printf("set exit node failed: %v", err)
		}
	}
	a.refresh()
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
