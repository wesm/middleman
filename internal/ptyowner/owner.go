package ptyowner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	gopty "github.com/aymanbagabas/go-pty"
)

const maxOutputReplay = 64 * 1024

const (
	maxOwnerConnections      = 32
	ownerFirstRequestTimeout = 5 * time.Second
	maxOwnerFirstRequestSize = 8 * 1024
	maxOwnerRequestSize      = 96 * 1024
	maxOwnerInputSize        = 64 * 1024
)

type Options struct {
	Root    string
	Session string
	Cwd     string
	Command []string
}

type owner struct {
	paths SessionPaths
	state ownerState
	pty   gopty.Pty
	cmd   *gopty.Cmd

	mu           sync.Mutex
	outputBuffer []byte
	subscribers  map[chan []byte]struct{}
	exitCode     int
	exited       bool
	done         chan struct{}
	drainDone    chan struct{}
	stopOnce     sync.Once
	closePtyOnce sync.Once
	connSem      chan struct{}
}

func RunOwner(ctx context.Context, opts Options) error {
	paths, err := NewSessionPaths(opts.Root, opts.Session)
	if err != nil {
		return err
	}
	command := append([]string(nil), opts.Command...)
	if len(command) == 0 {
		command = defaultShellCommand()
	}
	resolved, err := resolveExecutable(command[0])
	if err != nil {
		return err
	}
	command[0] = resolved

	p, err := gopty.New()
	if err != nil {
		return fmt.Errorf("open pty: %w", err)
	}

	cmd := p.Command(command[0], command[1:]...)
	cmd.Dir = opts.Cwd
	cmd.Env = sessionEnvironment(os.Environ(), nil)
	configureOwnerCommand(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start pty command: %w", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		killOwnerProcess(cmd.Process)
		_ = p.Close()
		return err
	}
	defer listener.Close()

	token, err := newToken()
	if err != nil {
		killOwnerProcess(cmd.Process)
		_ = p.Close()
		return err
	}
	o := &owner{
		paths:       paths,
		pty:         p,
		cmd:         cmd,
		subscribers: make(map[chan []byte]struct{}),
		exitCode:    -1,
		done:        make(chan struct{}),
		drainDone:   make(chan struct{}),
		connSem:     make(chan struct{}, maxOwnerConnections),
		state: ownerState{
			Session:   opts.Session,
			Addr:      listener.Addr().String(),
			Token:     token,
			Cwd:       opts.Cwd,
			PID:       os.Getpid(),
			CreatedAt: time.Now().UTC(),
		},
	}
	defer o.closePty()
	if err := writeState(paths, o.state); err != nil {
		killOwnerProcess(cmd.Process)
		return err
	}
	defer os.RemoveAll(paths.Dir)

	go func() {
		defer close(o.drainDone)
		o.drainOutput()
	}()
	go o.wait()

	acceptErr := make(chan error, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				acceptErr <- err
				return
			}
			select {
			case o.connSem <- struct{}{}:
				go func() {
					defer func() { <-o.connSem }()
					o.handleConn(conn)
				}()
			default:
				_ = conn.Close()
			}
		}
	}()

	select {
	case <-ctx.Done():
		o.stop()
		<-o.done
		return ctx.Err()
	case <-o.done:
		return nil
	case err := <-acceptErr:
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		o.stop()
		<-o.done
		return err
	}
}

func (o *owner) handleConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(ownerFirstRequestTimeout))

	reader := bufio.NewReader(conn)
	enc := json.NewEncoder(conn)
	var first initialRequest
	if err := decodeOwnerRequest(
		reader, maxOwnerFirstRequestSize, &first,
	); err != nil {
		return
	}
	if first.Token != o.state.Token {
		_ = enc.Encode(Response{
			Type: ResponseError, Error: "invalid pty owner token",
		})
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	switch first.Type {
	case RequestStatus:
		_ = enc.Encode(Response{
			Type: ResponseOK, OK: true, Output: o.snapshotOutput(),
		})
	case RequestStop:
		_ = enc.Encode(Response{Type: ResponseOK, OK: true})
		go o.stop()
	case RequestAttach:
		o.handleAttach(reader, enc, first.Request())
	default:
		_ = enc.Encode(Response{
			Type: ResponseError, Error: "unknown pty owner request",
		})
	}
}

func (o *owner) handleAttach(
	reader *bufio.Reader,
	enc *json.Encoder,
	first Request,
) {
	if first.Cols > 0 && first.Rows > 0 {
		_ = o.pty.Resize(first.Cols, first.Rows)
	}
	if err := enc.Encode(Response{Type: ResponseOK, OK: true}); err != nil {
		return
	}
	output, unsubscribe := o.subscribe()
	defer unsubscribe()

	writeDone := make(chan struct{})
	var writeMu sync.Mutex
	go func() {
		defer close(writeDone)
		for chunk := range output {
			writeMu.Lock()
			err := enc.Encode(Response{
				Type: ResponseOutput, OK: true, Output: chunk,
			})
			writeMu.Unlock()
			if err != nil {
				return
			}
		}
		code := o.currentExitCode()
		writeMu.Lock()
		_ = enc.Encode(Response{
			Type: ResponseExit, OK: true, ExitCode: &code,
		})
		writeMu.Unlock()
	}()

	for {
		var req Request
		if err := decodeOwnerRequest(
			reader, maxOwnerRequestSize, &req,
		); err != nil {
			return
		}
		if req.Token != o.state.Token {
			return
		}
		switch req.Type {
		case RequestInput:
			if len(req.Data) > maxOwnerInputSize {
				return
			}
			_, _ = o.pty.Write(req.Data)
		case RequestResize:
			if req.Cols > 0 && req.Rows > 0 {
				_ = o.pty.Resize(req.Cols, req.Rows)
			}
		case RequestStop:
			o.stop()
		}

		select {
		case <-writeDone:
			return
		default:
		}
	}
}

type initialRequest struct {
	Type  string `json:"type"`
	Token string `json:"token,omitempty"`
	Cols  int    `json:"cols,omitempty"`
	Rows  int    `json:"rows,omitempty"`
}

func (r initialRequest) Request() Request {
	return Request{
		Type:  r.Type,
		Token: r.Token,
		Cols:  r.Cols,
		Rows:  r.Rows,
	}
}

func decodeOwnerRequest(
	reader *bufio.Reader,
	maxBytes int,
	dst any,
) error {
	var data []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		data = append(data, chunk...)
		if len(data) > maxBytes {
			return fmt.Errorf("pty owner request exceeds %d bytes", maxBytes)
		}
		if err == nil {
			break
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return err
		}
	}
	return json.Unmarshal(data, dst)
}

func (o *owner) drainOutput() {
	buf := make([]byte, 32*1024)
	for {
		n, err := o.pty.Read(buf)
		if n > 0 {
			o.broadcast(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (o *owner) wait() {
	code := waitExitCode(o.cmd.Wait())
	select {
	case <-o.drainDone:
	case <-time.After(500 * time.Millisecond):
	}
	o.mu.Lock()
	o.exitCode = code
	o.exited = true
	for ch := range o.subscribers {
		delete(o.subscribers, ch)
		close(ch)
	}
	o.mu.Unlock()
	close(o.done)
}

func (o *owner) stop() {
	o.stopOnce.Do(func() {
		if o.cmd != nil && o.cmd.Process != nil {
			killOwnerProcess(o.cmd.Process)
		}
		o.closePty()
	})
}

func (o *owner) closePty() {
	o.closePtyOnce.Do(func() {
		_ = o.pty.Close()
	})
}

func (o *owner) broadcast(data []byte) {
	chunk := append([]byte(nil), data...)
	o.mu.Lock()
	defer o.mu.Unlock()
	o.outputBuffer = append(o.outputBuffer, chunk...)
	if extra := len(o.outputBuffer) - maxOutputReplay; extra > 0 {
		o.outputBuffer = append([]byte(nil), o.outputBuffer[extra:]...)
	}
	for ch := range o.subscribers {
		select {
		case ch <- chunk:
		default:
			delete(o.subscribers, ch)
			close(ch)
		}
	}
}

func (o *owner) subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 64)
	o.mu.Lock()
	if len(o.outputBuffer) > 0 {
		ch <- append([]byte(nil), o.outputBuffer...)
	}
	if o.exited {
		close(ch)
		o.mu.Unlock()
		return ch, func() {}
	}
	o.subscribers[ch] = struct{}{}
	o.mu.Unlock()
	return ch, func() {
		o.mu.Lock()
		if _, ok := o.subscribers[ch]; ok {
			delete(o.subscribers, ch)
			close(ch)
		}
		o.mu.Unlock()
	}
}

func (o *owner) snapshotOutput() []byte {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]byte(nil), o.outputBuffer...)
}

func (o *owner) currentExitCode() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.exitCode
}

func resolveExecutable(name string) (string, error) {
	if name == "" {
		return "", errors.New("session command is empty")
	}
	if filepath.IsAbs(name) {
		return name, nil
	}
	if !strings.ContainsRune(name, filepath.Separator) {
		path, err := exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf(
				"resolve session command %q via PATH: %w",
				name, err,
			)
		}
		if !filepath.IsAbs(path) {
			abs, err := filepath.Abs(path)
			if err != nil {
				return "", fmt.Errorf(
					"resolve session command %q via PATH: %w",
					name, err,
				)
			}
			path = abs
		}
		return path, nil
	}
	return "", fmt.Errorf(
		"session command %q must be an absolute path or a "+
			"PATH-resolvable name; relative paths resolve inside "+
			"the workspace worktree, which is untrusted",
		name,
	)
}

func defaultShellCommand() []string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return []string{shell}
	}
	if os.PathSeparator == '\\' {
		return []string{"cmd.exe"}
	}
	return []string{"/bin/sh"}
}

var sessionVarPrefixes = []string{
	"MIDDLEMAN_GITHUB_TOKEN",
	"GITHUB_TOKEN",
	"GH_TOKEN",
	"GH_PAT",
	"GITHUB_PAT",
	"GITHUB_ENTERPRISE_TOKEN",
	"GH_ENTERPRISE_TOKEN",
}

func sessionEnvironment(env []string, extraStrip []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			out = append(out, kv)
			continue
		}
		key := kv[:eq]
		if shouldStripSessionVar(key, extraStrip) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func shouldStripSessionVar(key string, extraStrip []string) bool {
	for _, prefix := range sessionVarPrefixes {
		if key == prefix || strings.HasPrefix(key, prefix+"_") {
			return true
		}
	}
	return slices.Contains(extraStrip, key)
}

func waitExitCode(waitErr error) int {
	if waitErr == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
