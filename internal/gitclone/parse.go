package gitclone

import (
	"bytes"
	"strings"

	godiff "github.com/sourcegraph/go-diff/diff"
)

// ParseRawZ parses the output of `git diff --raw -z` into file metadata.
// Returns files in output order (same order as patch output).
func ParseRawZ(data []byte) []DiffFile {
	// Split on NUL. The format is:
	//   :oldmode newmode oldhash newhash status\0path\0
	//   For renames/copies: :... R100\0oldpath\0newpath\0
	parts := bytes.Split(data, []byte{0})
	var files []DiffFile

	i := 0
	for i < len(parts) {
		part := string(parts[i])
		if !strings.HasPrefix(part, ":") {
			i++
			continue
		}

		// Parse the status letter from the end of the header.
		fields := strings.Fields(part)
		if len(fields) < 5 {
			i++
			continue
		}
		statusRaw := fields[4]
		status, isRenameOrCopy := rawStatusToString(statusRaw)

		i++ // move to path
		if i >= len(parts) {
			break
		}
		path := string(parts[i])

		var oldPath string
		if isRenameOrCopy {
			oldPath = path
			i++ // move to new path
			if i >= len(parts) {
				break
			}
			path = string(parts[i])
		}

		if oldPath == "" {
			oldPath = path
		}

		files = append(files, DiffFile{
			Path:    path,
			OldPath: oldPath,
			Status:  status,
		})
		i++
	}
	return files
}

func rawStatusToString(s string) (status string, isRenameOrCopy bool) {
	if len(s) == 0 {
		return "modified", false
	}
	switch s[0] {
	case 'A':
		return "added", false
	case 'D':
		return "deleted", false
	case 'M':
		return "modified", false
	case 'R':
		return "renamed", true
	case 'C':
		return "copied", true
	case 'T':
		return "modified", false // type change
	default:
		return "modified", false
	}
}

// ParsePatch parses unified diff patch output and merges it with
// pre-populated file metadata from ParseRawZ. Files are correlated by
// output order (git emits them in the same order).
func ParsePatch(patch []byte, rawFiles []DiffFile) []DiffFile {
	fileDiffs, _ := godiff.ParseMultiFileDiff(patch)
	if len(fileDiffs) == 0 {
		return rawFiles
	}

	for i, fd := range fileDiffs {
		if i >= len(rawFiles) {
			break
		}
		mergeFileDiff(&rawFiles[i], fd)
	}

	return rawFiles
}

func mergeFileDiff(file *DiffFile, fd *godiff.FileDiff) {
	file.IsBinary = file.IsBinary || fileDiffIsBinary(fd)
	for _, h := range fd.Hunks {
		file.Hunks = append(file.Hunks, parseHunk(h, file))
	}
}

func fileDiffIsBinary(fd *godiff.FileDiff) bool {
	for _, ext := range fd.Extended {
		if strings.HasPrefix(ext, "Binary files ") || strings.HasPrefix(ext, "GIT binary patch") {
			return true
		}
	}
	return false
}

func parseHunk(h *godiff.Hunk, file *DiffFile) Hunk {
	hunk := Hunk{
		OldStart: int(h.OrigStartLine),
		OldCount: int(h.OrigLines),
		NewStart: int(h.NewStartLine),
		NewCount: int(h.NewLines),
		Section:  h.Section,
	}
	state := hunkLineState{
		oldNum:           int(h.OrigStartLine),
		newNum:           int(h.NewStartLine),
		newSideNoNewline: len(h.Body) > 0 && h.Body[len(h.Body)-1] != '\n',
	}
	bodyLines := strings.Split(string(h.Body), "\n")
	for j, line := range bodyLines {
		if j == len(bodyLines)-1 && line == "" {
			continue
		}
		state.addLine(&hunk, file, h, bodyLines, j, line)
	}
	return hunk
}

type hunkLineState struct {
	oldNum           int
	newNum           int
	byteOffset       int
	newSideNoNewline bool
}

func (s *hunkLineState) addLine(
	hunk *Hunk,
	file *DiffFile,
	h *godiff.Hunk,
	bodyLines []string,
	j int,
	line string,
) {
	lineByteEnd := s.byteOffset + len(line) + 1 // +1 for \n separator
	defer func() { s.byteOffset = lineByteEnd }()

	if len(line) == 0 {
		hunk.Lines = append(hunk.Lines, Line{
			Type: "context", Content: "", OldNum: s.oldNum, NewNum: s.newNum,
		})
		s.oldNum++
		s.newNum++
		return
	}

	isLastRealLine := j == len(bodyLines)-1 ||
		(j == len(bodyLines)-2 && bodyLines[len(bodyLines)-1] == "")
	switch line[0] {
	case ' ':
		noNL := (s.newSideNoNewline && isLastRealLine) ||
			(h.OrigNoNewlineAt > 0 && int32(lineByteEnd) == h.OrigNoNewlineAt)
		hunk.Lines = append(hunk.Lines, Line{
			Type: "context", Content: line[1:], OldNum: s.oldNum, NewNum: s.newNum, NoNewline: noNL,
		})
		s.oldNum++
		s.newNum++
	case '+':
		noNL := s.newSideNoNewline && isLastRealLine
		hunk.Lines = append(hunk.Lines, Line{
			Type: "add", Content: line[1:], NewNum: s.newNum, NoNewline: noNL,
		})
		s.newNum++
		file.Additions++
	case '-':
		noNL := h.OrigNoNewlineAt > 0 && int32(lineByteEnd) == h.OrigNoNewlineAt
		hunk.Lines = append(hunk.Lines, Line{
			Type: "delete", Content: line[1:], OldNum: s.oldNum, NoNewline: noNL,
		})
		s.oldNum++
		file.Deletions++
	}
}
