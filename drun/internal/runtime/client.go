package runtime

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

type Client struct {
	*client.Client
	Runtime *Runtime
}

type ClientOptions struct {
	Runtime       *Runtime
	APIVersion    string
	DetectOptions *DetectOptions
}

func NewClient(ctx context.Context, opts *ClientOptions) (*Client, error) {
	if opts == nil {
		opts = &ClientOptions{}
	}

	rt := opts.Runtime
	if rt == nil {
		detector := NewDetector(opts.DetectOptions)
		detected, err := detector.Detect(ctx)
		if err != nil {
			return nil, err
		}
		rt = detected
	}

	clientOpts := []client.Opt{
		client.WithHost(rt.Host),
		client.WithAPIVersionNegotiation(),
	}

	if opts.APIVersion != "" {
		clientOpts = append(clientOpts, client.WithVersion(opts.APIVersion))
	}

	c, err := client.NewClientWithOpts(clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	if _, err := c.Ping(ctx); err != nil {
		c.Close()
		return nil, fmt.Errorf("failed to connect to docker: %w", err)
	}

	return &Client{
		Client:  c,
		Runtime: rt,
	}, nil
}

func (c *Client) Ping(ctx context.Context) (types.Ping, error) {
	return c.Client.Ping(ctx)
}

func (c *Client) Close() error {
	return c.Client.Close()
}
