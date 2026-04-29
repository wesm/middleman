package gitclone

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseNumstatZ(t *testing.T) {
	assert := assert.New(t)

	counts := parseNumstatZ([]byte(
		"1\t2\tinternal/cache.go\x00" +
			"4\t5\tinternal/path\twith-tab.go\x00" +
			"3\t0\t\x00old/path.go\x00new/path.go\x00" +
			"-\t-\tassets/logo.png\x00",
	))

	assert.Equal(numstatCount{additions: 1, deletions: 2}, counts["internal/cache.go"])
	assert.Equal(
		numstatCount{additions: 4, deletions: 5},
		counts["internal/path\twith-tab.go"],
	)
	assert.Equal(numstatCount{additions: 3, deletions: 0}, counts["new/path.go"])
	assert.Equal(numstatCount{additions: 0, deletions: 0}, counts["assets/logo.png"])
}
