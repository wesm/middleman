// Package server exposes HTTP routes and, in this file, tracks passive tmux
// activity for workspace responses.
//
// The workspace sidebar polls /workspaces frequently, but tmux has no durable
// "agent is working" flag for a pane. The tracker turns cheap observations from
// a tmux pane snapshot into a short-lived activity signal for the UI. A snapshot
// includes the active pane title and recent scrollback. The title path catches
// explicit spinner titles set by tools such as Codex; the output path catches
// tools that do not update the title but are still producing terminal output.
//
// Samples are keyed by tmux session. Each sample stores the last pane title, a
// fingerprint of normalized recent output, and the time that fingerprint last
// changed. A title that matches the known spinner protocol marks the workspace
// as working immediately. Otherwise, output is considered active when its
// fingerprint changed within tmuxActivityTTL. Cached samples remain usable for
// tmuxSampleMinInterval so the UI poll loop does not shell out to tmux on every
// request unless the previous sample is old enough to refresh.
package server

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

const (
	tmuxActivityTTL            = 30 * time.Second
	tmuxSampleMinInterval      = 4 * time.Second
	tmuxCaptureScrollbackLines = 160

	tmuxActivitySourceTitle   = "title"
	tmuxActivitySourceOutput  = "output"
	tmuxActivitySourceNone    = "none"
	tmuxActivitySourceUnknown = "unknown"
)

type TmuxActivitySample struct {
	Session        string
	PaneTitle      string
	Fingerprint    string
	HasFingerprint bool
	LastChangedAt  time.Time
	LastSampledAt  time.Time
	Source         string
	Working        bool
}

type tmuxActivityObservation struct {
	PaneTitle string
	Output    string
	HasOutput bool
}

type tmuxActivityResult struct {
	PaneTitle    string
	Working      bool
	Source       string
	LastOutputAt *time.Time
}

type tmuxActivityTracker struct {
	mu      sync.Mutex
	clock   func() time.Time
	samples map[string]TmuxActivitySample
}

func newTmuxActivityTracker(clock func() time.Time) *tmuxActivityTracker {
	if clock == nil {
		clock = time.Now
	}
	return &tmuxActivityTracker{
		clock:   clock,
		samples: make(map[string]TmuxActivitySample),
	}
}

func (t *tmuxActivityTracker) Cached(
	session string,
) (tmuxActivityResult, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	sample, ok := t.samples[session]
	if !ok {
		return tmuxActivityResult{}, false
	}
	now := t.clock().UTC()
	if now.Sub(sample.LastSampledAt) >= tmuxSampleMinInterval {
		return tmuxActivityResult{}, false
	}
	return tmuxActivityResultFromSample(sample, now), true
}

func (t *tmuxActivityTracker) Update(
	session string,
	obs tmuxActivityObservation,
) tmuxActivityResult {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.clock().UTC()
	sample := t.samples[session]
	sample.Session = session
	sample.PaneTitle = strings.TrimSpace(obs.PaneTitle)
	sample.LastSampledAt = now

	if obs.HasOutput {
		nextFingerprint := tmuxOutputFingerprint(obs.Output)
		if !sample.HasFingerprint {
			sample.Fingerprint = nextFingerprint
			sample.HasFingerprint = true
		} else if sample.Fingerprint != nextFingerprint {
			sample.Fingerprint = nextFingerprint
			sample.LastChangedAt = now
		}
	}

	result := tmuxActivityResultFromSample(sample, now)
	sample.Source = result.Source
	sample.Working = result.Working
	t.samples[session] = sample
	return result
}

func tmuxActivityResultFromSample(
	sample TmuxActivitySample,
	now time.Time,
) tmuxActivityResult {
	result := tmuxActivityResult{
		PaneTitle: sample.PaneTitle,
		Source:    tmuxActivitySourceNone,
	}
	if !sample.LastChangedAt.IsZero() {
		lastOutputAt := sample.LastChangedAt.UTC()
		result.LastOutputAt = &lastOutputAt
	}

	if isWorkingTmuxTitle(sample.PaneTitle) {
		result.Working = true
		result.Source = tmuxActivitySourceTitle
		return result
	}

	if !sample.LastChangedAt.IsZero() &&
		now.Sub(sample.LastChangedAt) <= tmuxActivityTTL {
		result.Working = true
		result.Source = tmuxActivitySourceOutput
		return result
	}

	return result
}

func normalizeTmuxOutput(output string) string {
	output = strings.ReplaceAll(output, "\r\n", "\n")
	output = strings.ReplaceAll(output, "\r", "\n")

	lines := strings.Split(output, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n")
}

func tmuxOutputFingerprint(output string) string {
	sum := sha256.Sum256([]byte(normalizeTmuxOutput(output)))
	return hex.EncodeToString(sum[:])
}
