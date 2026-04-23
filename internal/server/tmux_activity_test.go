package server

import (
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
)

func TestTmuxActivityTrackerUsesOutputFingerprintChanges(t *testing.T) {
	assert := Assert.New(t)
	now := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	tracker := newTmuxActivityTracker(func() time.Time { return now })

	first := tracker.Update("session-a", tmuxActivityObservation{
		PaneTitle: "workspace",
		Output:    "initial line\n",
		HasOutput: true,
	})
	assert.False(first.Working)
	assert.Equal(tmuxActivitySourceNone, first.Source)
	assert.Nil(first.LastOutputAt)

	now = now.Add(tmuxSampleMinInterval + time.Second)
	changed := tracker.Update("session-a", tmuxActivityObservation{
		PaneTitle: "workspace",
		Output:    "initial line\nnew line\n",
		HasOutput: true,
	})
	assert.True(changed.Working)
	assert.Equal(tmuxActivitySourceOutput, changed.Source)
	assert.NotNil(changed.LastOutputAt)
	assert.Equal(now, *changed.LastOutputAt)

	now = now.Add(5 * time.Second)
	stillRecent := tracker.Update("session-a", tmuxActivityObservation{
		PaneTitle: "workspace",
		Output:    "initial line\nnew line\n",
		HasOutput: true,
	})
	assert.True(stillRecent.Working)
	assert.Equal(tmuxActivitySourceOutput, stillRecent.Source)
	assert.NotNil(stillRecent.LastOutputAt)
	assert.Equal(*changed.LastOutputAt, *stillRecent.LastOutputAt)

	now = now.Add(tmuxActivityTTL + time.Second)
	expired := tracker.Update("session-a", tmuxActivityObservation{
		PaneTitle: "workspace",
		Output:    "initial line\nnew line\n",
		HasOutput: true,
	})
	assert.False(expired.Working)
	assert.Equal(tmuxActivitySourceNone, expired.Source)
	assert.NotNil(expired.LastOutputAt)
	assert.Equal(*changed.LastOutputAt, *expired.LastOutputAt)
}

func TestTmuxActivityTrackerPrefersTitleProtocol(t *testing.T) {
	assert := Assert.New(t)
	now := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	tracker := newTmuxActivityTracker(func() time.Time { return now })

	result := tracker.Update("session-a", tmuxActivityObservation{
		PaneTitle: "⠴ t3code-b5014b03",
		Output:    "stable\n",
		HasOutput: true,
	})

	assert.True(result.Working)
	assert.Equal(tmuxActivitySourceTitle, result.Source)
	assert.Nil(result.LastOutputAt)
}

func TestTmuxActivityTrackerCachesFreshSamples(t *testing.T) {
	assert := Assert.New(t)
	now := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	tracker := newTmuxActivityTracker(func() time.Time { return now })

	_, ok := tracker.Cached("session-a")
	assert.False(ok)

	baseline := tracker.Update("session-a", tmuxActivityObservation{
		PaneTitle: "workspace",
		Output:    "baseline\n",
		HasOutput: true,
	})

	now = now.Add(tmuxSampleMinInterval - time.Second)
	cached, ok := tracker.Cached("session-a")
	assert.True(ok)
	assert.Equal(baseline, cached)

	now = now.Add(2 * time.Second)
	_, ok = tracker.Cached("session-a")
	assert.False(ok)
}

func TestNormalizeTmuxOutputForFingerprinting(t *testing.T) {
	assert := Assert.New(t)

	assert.Equal(
		"one\ntwo\t\nthree\n",
		normalizeTmuxOutput("one  \r\ntwo\t \rthree\n"),
	)
	assert.Equal(
		tmuxOutputFingerprint("one\ntwo\n"),
		tmuxOutputFingerprint("one  \r\ntwo  \n"),
	)
}
