package web

import (
	"time"

	"github.com/gongahkia/onibi/internal/ghostty"
)

// Feature separates implementation/automated coverage from actual observations.
// Version support is metadata, never proof of native responsiveness or effects.
type Feature struct {
	ID                 string              `json:"id"`
	Name               string              `json:"name"`
	Group              string              `json:"group"`
	Detail             string              `json:"detail"`
	Implemented        bool                `json:"implemented"`
	Automated          bool                `json:"automated"`
	MinimumVersion     string              `json:"minimumVersion,omitempty"`
	RequiresFocus      bool                `json:"requiresFocus"`
	Availability       string              `json:"availability"`
	NativeVerification string              `json:"nativeVerification"`
	Observation        *FeatureObservation `json:"observation,omitempty"`
}
type FeatureObservation struct {
	Outcome string    `json:"outcome"`
	At      time.Time `json:"at"`
}
type FeatureReport struct {
	NativeBackend bool                 `json:"nativeBackend"`
	Version       ghostty.Capabilities `json:"version"`
	Features      []Feature            `json:"features"`
}

func featureCatalog() []Feature {
	var features []Feature
	add := func(id, name, group, detail, version string, focus bool) {
		features = append(features, Feature{ID: id, Name: name, Group: group, Detail: detail, Implemented: true, Automated: true, MinimumVersion: version, RequiresFocus: focus})
	}
	add("inventory", "Windows, tabs and panes", "Find and read", "Exact existing identities. Titles describe work; they do not prove agent status.", "1.3.0", false)
	add("snapshot", "Visible terminal text", "Find and read", "Bounded Accessibility snapshot of the focused pane. Follow polls snapshots; it is not a terminal stream.", "1.3.0", true)
	add("input", "Send or paste literal text", "Input", "Send adds Enter. Paste does not. Delivery acknowledgement does not prove the program processed it.", "1.3.0", false)
	add("key", "Keys and modifiers", "Input", "Navigation, editing, digits, plain and Ctrl/Alt letters, and function keys, each sent as a key press rather than a paste. The program decides what a key means.", "1.3.0", false)
	add("focus", "Focus an exact pane", "Workspace", "Changes focus on your Mac before reading.", "1.3.0", false)
	add("new_window", "Create a window", "Workspace", "Creates a native Ghostty window.", "1.3.0", false)
	add("new_tab", "Create a tab", "Workspace", "Creates a tab in the explicitly chosen window.", "1.3.0", false)
	add("split", "Split a pane", "Workspace", "Creates a split in any of four directions.", "1.3.0", false)
	for _, item := range []struct{ id, name string }{{"close_pane", "Close a pane"}, {"close_tab", "Close a tab"}, {"close_window", "Close a window"}} {
		add(item.id, item.name, "Workspace", "Requires the exact reviewed pane set. Observes removal without replaying; a local confirmation may keep panes open.", "1.3.0", false)
	}
	add("rename_tab", "Rename a tab", "Workspace", "Requires the selected pane to be focused. Blank restores the default title.", "1.3.1", true)
	add("rename_pane", "Rename a pane", "Workspace", "Requires the selected pane to be focused. Titles are display metadata.", "1.3.1", true)
	for _, control := range ghostty.Controls() {
		detail := "Changes the focused pane on your Mac."
		if control.ID == "prompt_previous" || control.ID == "prompt_next" {
			detail += " Prompt navigation depends on Ghostty shell integration."
		}
		add("control:"+control.ID, control.Label, control.Group, detail, "1.3.0", true)
	}
	add("session_read", "Read persistent sessions", "Persistent sessions", "Scrollback plus screen of daemon-managed tmux, Zellij and Screen sessions, up to 2,000 lines, without changing focus. tmux colours are kept. Requires the sessions daemon, which onibi web starts.", "", false)
	add("session_control", "Control persistent sessions", "Persistent sessions", "Text, keys, interrupt and a confirmed end-session by exact session ID. Works while Ghostty is unavailable.", "", false)
	add("session_new", "Start persistent sessions", "Persistent sessions", "A shell or any program on the Mac's PATH, in a folder of an open pane or session or a typed absolute path. Arguments are split on spaces.", "", false)
	add("push", "Notifications", "This browser", "Opt-in Web Push when a persistent session starts needing input or rings the bell. Names the session and reason, never output. iPhone and iPad need Onibi added to the Home Screen.", "", false)
	add("session_attention", "Session attention signals", "Persistent sessions", "Generic prompt patterns on the cursor line, terminal bells (tmux), and activity or idleness. Heuristics over screen text, not program state.", "", false)
	add("reader", "Phone reader tools", "This browser", "Text size, wrap, expanded reading, snapshot navigation and copy. No native display change.", "", false)
	add("devices", "Pairing and named browsers", "Access", "Optional fixed 30-day remembered access; individual revocation. Local fresh-link and login-service commands.", "", false)
	for _, item := range []struct{ id, name, detail string }{
		{"agent_state", "Agent task and approval state", "Agents can receive text and keys. Persistent sessions show generic prompt and bell signals, but a specific agent's task, approval and completion states are not inferred."},
		{"terminal_stream", "Full terminal emulation", "No PTY stream, cursor rendering, terminal graphics or images. Persistent session colours come from periodic captures."},
		{"mouse", "Remote terminal mouse input", "Mouse reporting is not exposed by this cockpit."},
		{"background_capture", "Unfocused Ghostty pane capture", "Ghostty panes expose the focused pane’s visible text. Persistent sessions are the way to read scrollback without focus."},
		{"configuration", "Arbitrary Ghostty configuration and actions", "Only the listed actions are exposed. Config editing, raw action strings, window positioning and file transfer remain outside this interface."},
	} {
		features = append(features, Feature{ID: item.id, Name: item.name, Group: "Not implemented", Detail: item.detail})
	}
	return features
}
func (s *Server) featureReport() FeatureReport {
	_, native := s.backend.(*ghostty.Client)
	report := FeatureReport{NativeBackend: native, Version: s.capabilities(), Features: featureCatalog()}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range report.Features {
		f := &report.Features[i]
		f.NativeVerification = "not_verified"
		f.Availability = "not_implemented"
		if !f.Implemented {
			f.NativeVerification = "not_applicable"
			continue
		}
		if f.MinimumVersion == "" {
			f.Availability = "available"
			f.NativeVerification = "not_applicable"
			continue
		}
		f.Availability = "version_unknown"
		if report.Version.Known {
			f.Availability = "version_supported"
			if !report.Version.Control || (f.MinimumVersion == "1.3.1" && !report.Version.Rename) {
				f.Availability = "version_blocked"
			}
		}
		if !native {
			continue
		}
		observation := s.featureObservations[f.ID]
		if f.ID == "inventory" {
			observation = FeatureObservation{Outcome: s.inventoryObservation.State, At: s.inventoryObservation.CheckedAt}
		}
		if f.ID == "snapshot" {
			observation = FeatureObservation{Outcome: s.captureObservation.State, At: s.captureObservation.CheckedAt}
		}
		if !observation.At.IsZero() {
			f.Observation = &observation
			if observation.Outcome == "ready" {
				f.NativeVerification = "observed"
			}
			if observation.Outcome == "removed" {
				f.NativeVerification = "scope_removal_observed"
			}
		}
	}
	return report
}
func (s *Server) recordFeature(action ghostty.Action, result ghostty.Result, err error) {
	id := action.Kind
	if id == "control" {
		id += ":" + action.Control
	}
	outcome := "acknowledged"
	if result.CloseState != "" {
		outcome = result.CloseState
	}
	if err != nil {
		outcome = "uncertain"
		if ghostty.IsRejected(err) {
			outcome = "rejected"
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.featureObservations == nil {
		s.featureObservations = make(map[string]FeatureObservation)
	}
	s.featureObservations[id] = FeatureObservation{Outcome: outcome, At: s.now().UTC()}
}
