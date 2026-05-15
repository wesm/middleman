package ptyowner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SessionPaths struct {
	Root      string
	Session   string
	Dir       string
	StatePath string
}

type ownerState struct {
	Session   string    `json:"session"`
	Addr      string    `json:"addr"`
	Token     string    `json:"token"`
	Cwd       string    `json:"cwd"`
	PID       int       `json:"pid"`
	CreatedAt time.Time `json:"created_at"`
}

func NewSessionPaths(root, session string) (SessionPaths, error) {
	if err := validateSessionName(session); err != nil {
		return SessionPaths{}, err
	}
	dir := filepath.Join(root, session)
	return SessionPaths{
		Root:      root,
		Session:   session,
		Dir:       dir,
		StatePath: filepath.Join(dir, "owner.json"),
	}, nil
}

func validateSessionName(session string) error {
	if session == "" {
		return fmt.Errorf("pty owner session name is empty")
	}
	if strings.Contains(session, "..") ||
		strings.ContainsAny(session, `/\`) ||
		strings.ContainsRune(session, 0) {
		return fmt.Errorf("unsafe pty owner session name %q", session)
	}
	return nil
}

func readState(paths SessionPaths) (ownerState, error) {
	data, err := os.ReadFile(paths.StatePath)
	if err != nil {
		return ownerState{}, err
	}
	var state ownerState
	if err := json.Unmarshal(data, &state); err != nil {
		return ownerState{}, err
	}
	if state.Addr == "" || state.Token == "" {
		return ownerState{}, fmt.Errorf("pty owner state is incomplete")
	}
	return state, nil
}

func writeState(paths SessionPaths, state ownerState) error {
	if err := os.MkdirAll(paths.Dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(paths.Dir, ".owner-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, paths.StatePath)
}
