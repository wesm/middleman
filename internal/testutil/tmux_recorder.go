package testutil

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func WriteTmuxRecorder(t *testing.T, bodySuffix string) (scriptPath, recordPath string) {
	t.Helper()
	dir := t.TempDir()
	recordPath = filepath.Join(dir, "record")
	scriptPath = filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"` + "\n" +
		bodySuffix
	require.NoError(t, os.WriteFile(scriptPath, []byte(body), 0o755))
	t.Setenv("TMUX_RECORD", recordPath)
	return scriptPath, recordPath
}

func ReadRecorderArgv(t *testing.T, path string) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	parts := strings.Split(string(data), "\x00")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	var out [][]string
	for i := 0; i < len(parts); {
		n, err := strconv.Atoi(parts[i])
		require.NoError(t, err)
		i++
		argv := parts[i : i+n]
		out = append(out, argv)
		i += n
	}
	return out
}
