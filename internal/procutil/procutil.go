package procutil

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

const DefaultMaxProcesses = 32

var ErrProcessLimitReached = errors.New("host process limit reached")

type Limiter struct {
	slots chan struct{}
}

func NewLimiter(max int) *Limiter {
	if max <= 0 {
		max = 1
	}
	return &Limiter{
		slots: make(chan struct{}, max),
	}
}

func (l *Limiter) TryAcquire(
	_ context.Context, reason string,
) (func(), error) {
	select {
	case l.slots <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() {
				<-l.slots
			})
		}, nil
	default:
		if reason != "" {
			return nil, fmt.Errorf(
				"%w: %s", ErrProcessLimitReached, reason,
			)
		}
		return nil, ErrProcessLimitReached
	}
}

var defaultLimiter = NewLimiter(DefaultMaxProcesses)

func TryAcquire(
	ctx context.Context, reason string,
) (func(), error) {
	return defaultLimiter.TryAcquire(ctx, reason)
}

func CombinedOutput(
	ctx context.Context, cmd *exec.Cmd, reason string,
) ([]byte, error) {
	release, err := TryAcquire(ctx, reason)
	if err != nil {
		return nil, err
	}
	defer release()
	out, err := cmd.CombinedOutput()
	return out, WrapResourceExhaustion(err, reason)
}

func Output(
	ctx context.Context, cmd *exec.Cmd, reason string,
) ([]byte, error) {
	release, err := TryAcquire(ctx, reason)
	if err != nil {
		return nil, err
	}
	defer release()
	out, err := cmd.Output()
	return out, WrapResourceExhaustion(err, reason)
}

func Run(
	ctx context.Context, cmd *exec.Cmd, reason string,
) error {
	release, err := TryAcquire(ctx, reason)
	if err != nil {
		return err
	}
	defer release()
	return WrapResourceExhaustion(cmd.Run(), reason)
}

func IsResourceExhausted(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrProcessLimitReached) ||
		errors.Is(err, syscall.EAGAIN) ||
		errors.Is(err, syscall.ENOMEM) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "resource temporarily unavailable") ||
		strings.Contains(msg, "fork failed") ||
		strings.Contains(msg, "forkpty") ||
		strings.Contains(msg, "cannot allocate memory")
}

func WrapResourceExhaustion(err error, action string) error {
	if err == nil || !IsResourceExhausted(err) {
		return err
	}
	if action == "" {
		return fmt.Errorf("%w: %v", ErrProcessLimitReached, err)
	}
	return fmt.Errorf(
		"%w while %s: %v",
		ErrProcessLimitReached, action, err,
	)
}
