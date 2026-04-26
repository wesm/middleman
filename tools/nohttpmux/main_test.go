package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckSourceFlagsApplicationServeMuxRegistrations(t *testing.T) {
	src := `package server

import "net/http"

func routes() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/pulls", nil)
	mux.Handle("/api/v1/repos", nil)
	http.HandleFunc("/api/v1/issues", nil)
	http.DefaultServeMux.Handle("/api/v1/stacks", nil)
	mux.HandleFunc("/metrics", nil)
}
`

	diagnostics, err := checkSource("internal/server/bad.go", src)

	require.NoError(t, err)
	require.Len(t, diagnostics, 5)
	assert.Equal(t, "internal/server/bad.go", diagnostics[0].Path)
	assert.Equal(t, 7, diagnostics[0].Line)
}

func TestCheckSourceAllowsHumaAdapterAndCurrentServerWrappers(t *testing.T) {
	src := `package server

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

func newServer(basePath string) {
	mux := http.NewServeMux()
	api.Adapter().Handle(&huma.Operation{}, nil)
	mux.HandleFunc("/", nil)
	outer := http.NewServeMux()
	outer.Handle("/healthz", mux)
	outer.Handle("/livez", mux)
	outer.Handle(basePath, http.StripPrefix("", mux))
}
`

	diagnostics, err := checkSource("/tmp/worktree/internal/server/server.go", src)

	require.NoError(t, err)
	assert.Empty(t, diagnostics)
}

func TestCheckSourceIgnoresTestsAndUntrackedHttptestMuxes(t *testing.T) {
	testSource := `package github

import "net/http"

func TestClient() {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", nil)
}
`
	nonServerSource := `package webhooktest

func newMux(mux interface{ HandleFunc(string, any) }) {
	mux.HandleFunc("/callback", nil)
}
`

	testDiagnostics, err := checkSource("internal/github/client_test.go", testSource)
	require.NoError(t, err)
	nonServerDiagnostics, err := checkSource("internal/github/httptest_helper.go", nonServerSource)
	require.NoError(t, err)

	assert.Empty(t, testDiagnostics)
	assert.Empty(t, nonServerDiagnostics)
}
