package gitclone

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseLinguistGeneratedAttributesKeepsExplicitStates(t *testing.T) {
	assert := assert.New(t)

	attrs := ParseLinguistGeneratedAttributes([]byte(
		"dist/api.ts\x00linguist-generated\x00set\x00" +
			"bun.lock\x00linguist-generated\x00unset\x00" +
			"src/app.ts\x00linguist-generated\x00unspecified\x00",
	))

	assert.Equal(map[string]bool{
		"dist/api.ts": true,
		"bun.lock":    false,
	}, attrs)
}

func TestMarkGeneratedFilesHonorsExplicitLinguistUnset(t *testing.T) {
	files := []DiffFile{
		{Path: "dist/api.ts"},
		{Path: "bun.lock"},
		{Path: "src/app.ts"},
	}

	MarkGeneratedFiles(files, map[string]bool{
		"dist/api.ts": true,
		"bun.lock":    false,
	})

	assert.True(t, files[0].IsGenerated)
	assert.False(t, files[1].IsGenerated)
	assert.False(t, files[2].IsGenerated)
}
