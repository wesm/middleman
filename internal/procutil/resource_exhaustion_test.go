package procutil

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"testing"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsResourceExhausted(t *testing.T) {
	assert := Assert.New(t)

	assert.True(IsResourceExhausted(syscall.EAGAIN))
	assert.True(IsResourceExhausted(fmt.Errorf("wrap: %w", syscall.EAGAIN)))
	assert.True(IsResourceExhausted(errors.New("forkpty: Resource temporarily unavailable")))
	assert.True(IsResourceExhausted(errors.New("zsh: fork failed: resource temporarily unavailable")))
	assert.False(IsResourceExhausted(errors.New("permission denied")))
	assert.False(IsResourceExhausted(nil))
}

func TestLimiterTryAcquireAndRelease(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	limiter := NewLimiter(1)
	release, err := limiter.TryAcquire(context.Background(), "first")
	require.NoError(err)
	assert.NotNil(release)

	release2, err := limiter.TryAcquire(context.Background(), "second")
	require.ErrorIs(err, ErrProcessLimitReached)
	assert.Nil(release2)

	release()

	release3, err := limiter.TryAcquire(context.Background(), "third")
	require.NoError(err)
	assert.NotNil(release3)
	release3()
}

func TestWrapResourceExhaustion(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	err := WrapResourceExhaustion(
		errors.New("forkpty: Resource temporarily unavailable"),
		"attach workspace terminal",
	)
	require.ErrorIs(err, ErrProcessLimitReached)
	assert.Contains(err.Error(), "attach workspace terminal")
	assert.Contains(err.Error(), "forkpty")

	other := errors.New("permission denied")
	assert.Same(other, WrapResourceExhaustion(other, "git fetch"))
	require.NoError(WrapResourceExhaustion(nil, "git fetch"))
}
