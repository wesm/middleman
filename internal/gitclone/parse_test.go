package gitclone

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRawZ(t *testing.T) {
	// git diff --raw -z output: status\0path\0 (NUL-delimited)
	// M for modified, A for added, D for deleted
	// Renames: R100\0oldpath\0newpath\0
	raw := ":100644 100644 abc def M\x00src/main.go\x00" +
		":000000 100644 000 abc A\x00src/new.go\x00" +
		":100644 000000 abc 000 D\x00src/old.go\x00" +
		":100644 100644 abc def R100\x00src/before.go\x00src/after.go\x00"

	files := ParseRawZ([]byte(raw))
	require.Len(t, files, 4)

	assert.Equal(t, "src/main.go", files[0].Path)
	assert.Equal(t, "modified", files[0].Status)

	assert.Equal(t, "src/new.go", files[1].Path)
	assert.Equal(t, "added", files[1].Status)

	assert.Equal(t, "src/old.go", files[2].Path)
	assert.Equal(t, "deleted", files[2].Status)

	assert.Equal(t, "src/after.go", files[3].Path)
	assert.Equal(t, "src/before.go", files[3].OldPath)
	assert.Equal(t, "renamed", files[3].Status)
}

func TestParsePatch(t *testing.T) {
	patch := `diff --git a/src/main.go b/src/main.go
index abc..def 100644
--- a/src/main.go
+++ b/src/main.go
@@ -10,6 +10,8 @@ func main() {
 	fmt.Println("hello")
 	fmt.Println("world")
+	fmt.Println("new line 1")
+	fmt.Println("new line 2")
 	fmt.Println("end")
-	fmt.Println("removed")
 }
`

	// Provide pre-populated file metadata from --raw -z.
	rawFiles := []DiffFile{
		{Path: "src/main.go", OldPath: "src/main.go", Status: "modified"},
	}

	files := ParsePatch([]byte(patch), rawFiles)
	require.Len(t, files, 1)

	f := files[0]
	assert.Equal(t, "src/main.go", f.Path)
	assert.Equal(t, 2, f.Additions)
	assert.Equal(t, 1, f.Deletions)
	assert.False(t, f.IsBinary)

	require.Len(t, f.Hunks, 1)
	h := f.Hunks[0]
	assert.Equal(t, 10, h.OldStart)
	assert.Equal(t, 6, h.OldCount)
	assert.Equal(t, 10, h.NewStart)
	assert.Equal(t, 8, h.NewCount)
	assert.Equal(t, "func main() {", h.Section)

	// Check line types.
	types := make([]string, len(h.Lines))
	for i, l := range h.Lines {
		types[i] = l.Type
	}
	assert.Equal(t, []string{
		"context", "context", "add", "add", "context", "delete", "context",
	}, types)
}

func TestParsePatchNoNewline(t *testing.T) {
	patch := `diff --git a/file.txt b/file.txt
index abc..def 100644
--- a/file.txt
+++ b/file.txt
@@ -1,2 +1,2 @@
 line1
-line2
\ No newline at end of file
+line2-modified
\ No newline at end of file
`
	rawFiles := []DiffFile{
		{Path: "file.txt", OldPath: "file.txt", Status: "modified"},
	}
	files := ParsePatch([]byte(patch), rawFiles)
	require.Len(t, files, 1)
	require.Len(t, files[0].Hunks, 1)

	lines := files[0].Hunks[0].Lines
	require.Len(t, lines, 3) // context + delete + add

	assert.True(t, lines[1].NoNewline, "deleted line should have no_newline")
	assert.True(t, lines[2].NoNewline, "added line should have no_newline")
}
