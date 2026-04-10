package github

import (
	"context"
	"fmt"
	"testing"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchAllPagesSinglePage(t *testing.T) {
	assert := Assert.New(t)

	items, err := fetchAllPages(
		context.Background(),
		func(_ context.Context, cursor *string) ([]int, pageInfo, error) {
			assert.Nil(cursor)
			return []int{1, 2, 3}, pageInfo{HasNextPage: false}, nil
		},
	)
	require.NoError(t, err)
	assert.Equal([]int{1, 2, 3}, items)
}

func TestFetchAllPagesMultiPage(t *testing.T) {
	assert := Assert.New(t)
	calls := 0

	items, err := fetchAllPages(
		context.Background(),
		func(_ context.Context, cursor *string) ([]string, pageInfo, error) {
			calls++
			switch calls {
			case 1:
				assert.Nil(cursor)
				return []string{"a", "b"}, pageInfo{
					HasNextPage: true,
					EndCursor:   "cursor1",
				}, nil
			case 2:
				require.NotNil(t, cursor)
				assert.Equal("cursor1", *cursor)
				return []string{"c"}, pageInfo{
					HasNextPage: false,
				}, nil
			default:
				t.Fatal("too many calls")
				return nil, pageInfo{}, nil
			}
		},
	)
	require.NoError(t, err)
	assert.Equal([]string{"a", "b", "c"}, items)
	assert.Equal(2, calls)
}

func TestFetchAllPagesError(t *testing.T) {
	assert := Assert.New(t)

	// Test error on first page
	_, err := fetchAllPages(
		context.Background(),
		func(_ context.Context, cursor *string) ([]int, pageInfo, error) {
			return nil, pageInfo{}, fmt.Errorf("graphql: rate limited")
		},
	)
	assert.Error(err)
	assert.Contains(err.Error(), "rate limited")
}

func TestFetchAllPagesContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetchAllPages(
		ctx,
		func(ctx context.Context, cursor *string) ([]int, pageInfo, error) {
			return nil, pageInfo{}, ctx.Err()
		},
	)
	Assert.Error(t, err)
}
