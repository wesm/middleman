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
	assert.Contains(compose, "MIDDLEMAN_CONFIG_PATH: /app/docker/dev-config.toml")
	assert.NotContains(compose, "${HOME}/.config/middleman/config.toml")
	assert.NotContains(compose, "MIDDLEMAN_HOME: /data")
	assert.NotContains(compose, "make dev")
	assert.NotContains(compose, "make frontend-dev BUN_INSTALL_FLAGS=--frozen-lockfile ARGS=\"--host 0.0.0.0 --port 15173\"")
}

func TestDevStackBackendScriptPreparesEmbedDirAndStartsAir(t *testing.T) {
	req := require.New(t)
	assert := Assert.New(t)

	repoRoot, err := os.Getwd()
	req.NoError(err)

	dir := t.TempDir()
	req.NoError(os.MkdirAll(filepath.Join(dir, "internal", "web", "dist"), 0o755))

	binDir := filepath.Join(dir, "bin")
	req.NoError(os.MkdirAll(binDir, 0o755))

	fakeUname := filepath.Join(binDir, "uname")
	req.NoError(os.WriteFile(fakeUname, []byte("#!/bin/sh\nprintf '%s\\n' MINGW64_NT-10.0\n"), 0o755))

	airLog := filepath.Join(dir, "air.log")
	fakeAir := filepath.Join(binDir, "air")
	req.NoError(os.WriteFile(fakeAir, []byte("#!/bin/sh\nprintf 'args=%s\\n' \"$*\" > \"$AIR_LOG\"\nprintf 'log_level=%s\\nlog_file=%s\\nstderr_level=%s\\n' \"$MIDDLEMAN_LOG_LEVEL\" \"$MIDDLEMAN_LOG_FILE\" \"$MIDDLEMAN_LOG_STDERR_LEVEL\" >> \"$AIR_LOG\"\n"), 0o755))

	configPath := filepath.Join(dir, "config.toml")
	req.NoError(os.WriteFile(configPath, []byte("port = 9123\n"), 0o644))

	scriptPath := filepath.ToSlash(filepath.Join(repoRoot, "scripts", "dev-stack-backend.sh"))
	cmd := exec.Command("sh", scriptPath)
	cmd.Dir = dir
	cmd.Env = append(
		os.Environ(),
		"AIR_BIN="+fakeAir,
		"AIR_LOG="+airLog,
		"BACKEND_ARGS=--sync-on-start=false",
		"MIDDLEMAN_CONFIG="+configPath,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)

	output, err := cmd.CombinedOutput()
	req.NoError(err, string(output))

	stub, err := os.ReadFile(filepath.Join(dir, "internal", "web", "dist", "stub.html"))
	req.NoError(err)
	assert.Equal("ok\n", string(stub))

	airInvocation, err := os.ReadFile(airLog)
	req.NoError(err)
	log := string(airInvocation)
	assert.Contains(log, "-c .air.windows.toml -- -config "+configPath+" --sync-on-start=false")
	assert.Contains(log, "log_level=debug")
	assert.Contains(log, "log_file=tmp/logs/backend-dev.log")
	assert.Contains(log, "stderr_level=info")
}

func TestDevStackFrontendScriptInstallsDepsThenStartsVite(t *testing.T) {
	req := require.New(t)
	assert := Assert.New(t)

	repoRoot, err := os.Getwd()
	req.NoError(err)

	dir := t.TempDir()
	req.NoError(os.MkdirAll(filepath.Join(dir, "frontend"), 0o755))

	binDir := filepath.Join(dir, "bin")
	req.NoError(os.MkdirAll(binDir, 0o755))

	bunLog := filepath.Join(dir, "bun.log")
	fakeBun := filepath.Join(binDir, "bun")
	req.NoError(os.WriteFile(fakeBun, []byte("#!/bin/sh\nprintf '%s|%s\\n' \"$(pwd)\" \"$*\" >> \"$BUN_LOG\"\n"), 0o755))

	scriptPath := filepath.ToSlash(filepath.Join(repoRoot, "scripts", "dev-stack-frontend.sh"))
	cmd := exec.Command("sh", scriptPath)
	cmd.Dir = dir
	cmd.Env = append(
		os.Environ(),
		"BUN_INSTALL_FLAGS=--frozen-lockfile",
		"BUN_LOG="+bunLog,
		"FRONTEND_ARGS=--host 0.0.0.0 --port 15173",
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)

	output, err := cmd.CombinedOutput()
	req.NoError(err, string(output))

	content, err := os.ReadFile(bunLog)
	req.NoError(err)
	lines := strings.Split(strings.TrimSpace(filepath.ToSlash(string(content))), "\n")
	req.Len(lines, 2)
	assert.True(strings.HasSuffix(lines[0], "/frontend|install --frozen-lockfile"), lines[0])
	assert.True(strings.HasSuffix(lines[1], "/frontend|run dev -- --host 0.0.0.0 --port 15173"), lines[1])
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

			airArgsLog := filepath.Join(dir, "air-args.log")
			fakeAir := filepath.Join(dir, "air")
			req.NoError(os.WriteFile(fakeAir, []byte("#!/bin/sh\nprintf '%s\n' \"$*\" > \"$AIR_ARGS_LOG\"\nwhile [ ! -f \"$SOCAT_READY_FILE\" ]; do\n  sleep 0.01\ndone\nexit 0\n"), 0o755))

			goArgsLog := filepath.Join(dir, "go-args.log")
			fakeGo := filepath.Join(dir, "go")
			req.NoError(os.WriteFile(fakeGo, []byte("#!/bin/sh\nprintf '%s\n' \"$*\" > \"$GO_ARGS_LOG\"\nif [ -n \"${GO_STDOUT:-}\" ]; then\n  printf '%s' \"$GO_STDOUT\"\nfi\nexit \"${GO_EXIT_CODE:-0}\"\n"), 0o755))

			cmd := exec.Command("sh", "docker/backend-dev-entrypoint.sh")
			cmd.Dir = "."
			cmd.Env = append(
				os.Environ(),
				"AIR_ARGS_LOG="+airArgsLog,
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

			airArgsContent, err := os.ReadFile(airArgsLog)
			req.NoError(err, string(output))
			airArgs := strings.TrimSpace(string(airArgsContent))
			assert.Contains(airArgs, "-c .air.toml -- -config "+configPath)

			argsContent, err := os.ReadFile(argsLog)
			req.NoError(err, string(output))
			args := strings.TrimSpace(string(argsContent))
			assert.Contains(args, "TCP-LISTEN:18091")
			assert.Contains(args, tc.wantTarget)
		})
	}
}
