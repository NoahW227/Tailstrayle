package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Reads go through tailscaled's LocalAPI socket: it's the same JSON the CLI
// prints, without spawning a `tailscale` process on every poll. Writes go
// through the tailscale CLI, which validates input and produces readable
// error messages (e.g. missing operator permission).

const localAPISocket = "/var/run/tailscale/tailscaled.sock"

var localAPI = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", localAPISocket)
		},
	},
}

func localAPIGet(path string, v any) error {
	req, err := http.NewRequest("GET", "http://local-tailscaled.sock/localapi/v0/"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Sec-Tailscale", "localapi")
	resp, err := localAPI.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// Subset of ipnstate.Status (`tailscale status --json`)
type tsStatus struct {
	BackendState   string // "Running", "Stopped", "NeedsLogin", "NoState", "Starting"
	Self           tsPeer
	Peer           map[string]tsPeer
	User           map[int64]tsUser
	CurrentTailnet *tsTailnet
}

type tsPeer struct {
	ID             string
	HostName       string
	DNSName        string
	TailscaleIPs   []string
	Online         bool
	ExitNode       bool
	ExitNodeOption bool
	UserID         int64
}

type tsUser struct {
	LoginName   string
	DisplayName string
}

type tsTailnet struct {
	Name string
}

// Subset of ipn.Prefs (`tailscale debug prefs`)
type tsPrefs struct {
	ExitNodeID             string
	ExitNodeAllowLANAccess bool
	CorpDNS                bool
	RouteAll               bool
	ShieldsUp              bool
	AutoUpdate             struct {
		Apply *bool // null when never set
	}
}

// Subset of ipn.LoginProfile (`tailscale switch --list --json`)
type tsProfile struct {
	ID             string
	Name           string // login name
	NetworkProfile struct {
		DomainName  string
		DisplayName string
	}
}

func (p tsProfile) tailnet() string {
	if p.NetworkProfile.DisplayName != "" {
		return p.NetworkProfile.DisplayName
	}
	if p.NetworkProfile.DomainName != "" {
		return p.NetworkProfile.DomainName
	}
	return p.Name
}

// snapshot is everything the menu is rendered from. status is nil when the
// daemon is unreachable; prefs and profiles are nil when they couldn't be read.
type snapshot struct {
	status         *tsStatus
	prefs          *tsPrefs
	profiles       []tsProfile
	currentProfile string
}

func fetchSnapshot() snapshot {
	var snap snapshot
	var st tsStatus
	if err := localAPIGet("status", &st); err != nil {
		log.Printf("status fetch failed: %v", err)
		return snap
	}
	snap.status = &st

	var p tsPrefs
	if err := localAPIGet("prefs", &p); err != nil {
		log.Printf("prefs fetch failed: %v", err)
	} else {
		snap.prefs = &p
	}

	var cur tsProfile
	if err := localAPIGet("profiles/", &snap.profiles); err != nil {
		log.Printf("profiles fetch failed: %v", err)
	} else if err := localAPIGet("profiles/current", &cur); err != nil {
		log.Printf("current profile fetch failed: %v", err)
	} else {
		snap.currentProfile = cur.ID
	}
	return snap
}

func (s *tsStatus) accountName() string {
	if s == nil {
		return ""
	}
	if u, ok := s.User[s.Self.UserID]; ok {
		if u.LoginName != "" {
			return u.LoginName
		}
		return u.DisplayName
	}
	return ""
}

func (s *tsStatus) tailnetName() string {
	if s == nil || s.CurrentTailnet == nil {
		return ""
	}
	return s.CurrentTailnet.Name
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

func (s *tsStatus) ip() string {
	if s == nil || len(s.Self.TailscaleIPs) == 0 {
		return ""
	}
	return s.Self.TailscaleIPs[0]
}

// machineName is the peer's MagicDNS name without the tailnet suffix. Unlike
// HostName it is unique within a tailnet (e.g. "fedora", "fedora-1").
func (p tsPeer) machineName() string {
	if name, _, _ := strings.Cut(p.DNSName, "."); name != "" {
		return name
	}
	return p.HostName
}

// tailscale runs the tailscale CLI, returning its output as the error on failure.
func tailscale(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tailscale", args...).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
}
