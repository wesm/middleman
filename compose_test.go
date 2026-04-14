package middleman

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeDevServicesUseEntrypointScripts(t *testing.T) {
	req := require.New(t)
	assert := Assert.New(t)

	content, err := os.ReadFile("compose.yml")
	req.NoError(err)

	compose := string(content)
	assert.Contains(compose, "/app/docker/backend-dev-entrypoint.sh")
	assert.Contains(compose, "/app/docker/frontend-dev-entrypoint.sh")
	assert.NotContains(compose, "make dev")
	assert.NotContains(compose, "make frontend-dev BUN_INSTALL_FLAGS=--frozen-lockfile ARGS=\"--host 0.0.0.0 --port 15173\"")
}

func TestBackendDevEntrypointForwardsToConfiguredPort(t *testing.T) {
	tests := []struct {
		name       string
		goExitCode string
		goStdout   string
		wantTarget string
	}{
		{
			name:       "configured port from cli",
			goExitCode: "0",
			goStdout:   "9123\n",
			wantTarget: "TCP:127.0.0.1:9123",
		},
		{
			name:       "cli failure falls back to default port",
			goExitCode: "1",
			wantTarget: "TCP:127.0.0.1:8091",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)
			assert := Assert.New(t)

			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.toml")
			req.NoError(os.WriteFile(configPath, []byte("port = 9123\n"), 0o644))

			argsLog := filepath.Join(dir, "socat-args.log")
			readyFile := filepath.Join(dir, "socat-ready")
			fakeSocat := filepath.Join(dir, "socat")
			req.NoError(os.WriteFile(fakeSocat, []byte("#!/bin/sh\nprintf '%s\n' \"$*\" > \"$SOCAT_ARGS_LOG\"\ntouch \"$SOCAT_READY_FILE\"\ntrap 'exit 0' INT TERM\nwhile :; do sleep 1; done\n"), 0o755))

			fakeAir := filepath.Join(dir, "air")
			req.NoError(os.WriteFile(fakeAir, []byte("#!/bin/sh\nwhile [ ! -f \"$SOCAT_READY_FILE\" ]; do\n  sleep 0.01\ndone\nexit 0\n"), 0o755))

			goArgsLog := filepath.Join(dir, "go-args.log")
			fakeGo := filepath.Join(dir, "go")
			req.NoError(os.WriteFile(fakeGo, []byte("#!/bin/sh\nprintf '%s\n' \"$*\" > \"$GO_ARGS_LOG\"\nif [ -n \"${GO_STDOUT:-}\" ]; then\n  printf '%s' \"$GO_STDOUT\"\nfi\nexit \"${GO_EXIT_CODE:-0}\"\n"), 0o755))

			cmd := exec.Command("sh", "docker/backend-dev-entrypoint.sh")
			cmd.Dir = "."
			cmd.Env = append(
				os.Environ(),
				"AIR_BIN="+fakeAir,
				"GO_BIN="+fakeGo,
				"GO_ARGS_LOG="+goArgsLog,
				"GO_EXIT_CODE="+tc.goExitCode,
				"GO_STDOUT="+tc.goStdout,
				"MIDDLEMAN_CONFIG_PATH="+configPath,
				"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"SOCAT_ARGS_LOG="+argsLog,
				"SOCAT_READY_FILE="+readyFile,
			)

			output, err := cmd.CombinedOutput()
			req.NoError(err, string(output))

			goArgsContent, err := os.ReadFile(goArgsLog)
			req.NoError(err, string(output))
			goArgs := strings.TrimSpace(string(goArgsContent))
			assert.Contains(goArgs, "run ./cmd/middleman config read -config "+configPath+" port")

			argsContent, err := os.ReadFile(argsLog)
			req.NoError(err, string(output))
			args := strings.TrimSpace(string(argsContent))
			assert.Contains(args, "TCP-LISTEN:18091")
			assert.Contains(args, tc.wantTarget)
		})
	}
}
