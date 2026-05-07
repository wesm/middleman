package forgejo

import (
	"context"
	"net/http"
	"strings"
	"time"

	forgejosdk "codeberg.org/mvdkleijn/forgejo-sdk/forgejo/v3"
	"github.com/wesm/middleman/internal/platform"
	"github.com/wesm/middleman/internal/ratelimit"
)

type ClientOption func(*clientOptions)

type clientOptions struct {
	baseURL           string
	foregroundTimeout time.Duration
	rateTracker       *ratelimit.RateTracker
	skipVersionProbe  bool
}

type Client struct {
	host              string
	baseURL           string
	transport         *transport
	api               *forgejosdk.Client
	foregroundTimeout time.Duration
}

func WithBaseURLForTesting(baseURL string) ClientOption {
	return func(opts *clientOptions) {
		opts.baseURL = strings.TrimRight(baseURL, "/")
		opts.skipVersionProbe = true
	}
}

func WithForegroundTimeoutForTesting(timeout time.Duration) ClientOption {
	return func(opts *clientOptions) {
		opts.foregroundTimeout = timeout
	}
}

func WithRateTracker(rateTracker *ratelimit.RateTracker) ClientOption {
	return func(opts *clientOptions) {
		opts.rateTracker = rateTracker
	}
}

func NewClient(host, token string, options ...ClientOption) (*Client, error) {
	opts := clientOptions{
		baseURL:           "https://" + strings.TrimRight(host, "/"),
		foregroundTimeout: 20 * time.Second,
	}
	for _, option := range options {
		option(&opts)
	}

	clientOptions := []forgejosdk.ClientOption{
		forgejosdk.SetToken(token),
		forgejosdk.SetUserAgent("middleman"),
	}
	if opts.skipVersionProbe {
		clientOptions = append(
			clientOptions,
			forgejosdk.SetForgejoVersion("13.0.0+gitea-1.26.0"),
		)
	}
	httpTransport := http.DefaultTransport
	if opts.rateTracker != nil {
		httpTransport = &rateTrackingTransport{
			base:        httpTransport,
			rateTracker: opts.rateTracker,
		}
	}
	clientOptions = append(clientOptions, forgejosdk.SetHTTPClient(&http.Client{
		Timeout:   opts.foregroundTimeout,
		Transport: httpTransport,
	}))

	api, err := forgejosdk.NewClient(opts.baseURL, clientOptions...)
	if err != nil {
		return nil, err
	}
	return &Client{
		host:              host,
		baseURL:           opts.baseURL,
		api:               api,
		transport:         &transport{api: api, requestContextLock: make(chan struct{}, 1)},
		foregroundTimeout: opts.foregroundTimeout,
	}, nil
}

func (c *Client) Platform() platform.Kind {
	return platform.KindForgejo
}

func (c *Client) Host() string {
	return c.host
}

func (c *Client) Capabilities() platform.Capabilities {
	return platform.Capabilities{}
}

type transport struct {
	api                *forgejosdk.Client
	requestContextLock chan struct{}
}

func (t *transport) getRepositoryRaw(
	ctx context.Context, owner, repo string,
) (*forgejosdk.Repository, error) {
	var repository *forgejosdk.Repository
	err := t.withRequestContext(ctx, func() error {
		var err error
		repository, _, err = t.api.GetRepo(owner, repo)
		return err
	})
	return repository, err
}

func (t *transport) withRequestContext(ctx context.Context, request func() error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	select {
	case t.requestContextLock <- struct{}{}:
		defer func() { <-t.requestContextLock }()
	case <-ctx.Done():
		return ctx.Err()
	}

	t.api.SetContext(ctx)
	defer t.api.SetContext(context.Background())
	return request()
}

type rateTrackingTransport struct {
	base        http.RoundTripper
	rateTracker *ratelimit.RateTracker
}

func (t *rateTrackingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if resp != nil && t.rateTracker != nil {
		t.rateTracker.RecordRequest()
	}
	return resp, err
}
