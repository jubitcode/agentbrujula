package build

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"

	"drun/internal/config"
)

type Builder struct {
	client *client.Client
	cache  *Cache
}

func NewBuilder(dockerClient *client.Client, cache *Cache) *Builder {
	return &Builder{
		client: dockerClient,
		cache:  cache,
	}
}

type BuildOptions struct {
	NoCache    bool
	ForceBuild bool
	Output     io.Writer
	Tags       []string
}

type BuildResult struct {
	ImageID    string
	BuildKey   string
	Cached     bool
	Duration   time.Duration
	Dockerfile string
}

type DryRunResult struct {
	Dockerfile string
	BuildKey   string
	Cached     bool
	ImageID    string
}

func (b *Builder) GenerateDockerfile(cfg *config.Config) (string, error) {
	return GenerateDockerfile(cfg)
}

func (b *Builder) GenerateBuildKey(cfg *config.Config) string {
	h := sha256.New()
	h.Write([]byte(cfg.Hash()))

	if cfg.DockerfileInline != "" {
		h.Write([]byte(cfg.DockerfileInline))
	}

	return fmt.Sprintf("drun-%x", h.Sum(nil))[:24]
}

func (b *Builder) IsCached(ctx context.Context, cfg *config.Config) (bool, string) {
	if b.cache == nil {
		return false, ""
	}

	buildKey := b.GenerateBuildKey(cfg)
	entry, err := b.cache.Get(ctx, buildKey)
	if err != nil || entry == nil {
		return false, ""
	}

	return true, entry.ImageID
}

func (b *Builder) DryRun(ctx context.Context, cfg *config.Config) (*DryRunResult, error) {
	dockerfile, err := b.GenerateDockerfile(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to generate Dockerfile: %w", err)
	}

	buildKey := b.GenerateBuildKey(cfg)
	cached, imageID := b.IsCached(ctx, cfg)

	return &DryRunResult{
		Dockerfile: dockerfile,
		BuildKey:   buildKey,
		Cached:     cached,
		ImageID:    imageID,
	}, nil
}

func (b *Builder) Build(ctx context.Context, cfg *config.Config, opts *BuildOptions) (*BuildResult, error) {
	if opts == nil {
		opts = &BuildOptions{Output: os.Stdout}
	}

	startTime := time.Now()

	buildKey := b.GenerateBuildKey(cfg)
	if !opts.ForceBuild && b.cache != nil {
		if entry, err := b.cache.Get(ctx, buildKey); err == nil && entry != nil {
			return &BuildResult{
				ImageID:  entry.ImageID,
				BuildKey: buildKey,
				Cached:   true,
				Duration: time.Since(startTime),
			}, nil
		}
	}

	dockerfile, err := b.GenerateDockerfile(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to generate Dockerfile: %w", err)
	}

	if b.client == nil {
		return nil, fmt.Errorf("no Docker client available")
	}

	imageID, err := b.buildImage(ctx, cfg, dockerfile, opts)
	if err != nil {
		return nil, fmt.Errorf("build failed: %w", err)
	}

	if b.cache != nil {
		entry := &CacheEntry{
			Key:       buildKey,
			ImageID:   imageID,
			CreatedAt: time.Now(),
			LastUsed:  time.Now(),
		}
		if err := b.cache.Put(ctx, entry); err != nil && opts.Output != nil {
			fmt.Fprintf(opts.Output, "Warning: failed to cache build: %v\n", err)
		}
	}

	return &BuildResult{
		ImageID:    imageID,
		BuildKey:   buildKey,
		Cached:     false,
		Duration:   time.Since(startTime),
		Dockerfile: dockerfile,
	}, nil
}

func (b *Builder) buildImage(ctx context.Context, cfg *config.Config, dockerfile string, opts *BuildOptions) (string, error) {
	buildContext, err := CreateBuildContext(dockerfile, cfg.ProjectDir)
	if err != nil {
		return "", fmt.Errorf("failed to create build context: %w", err)
	}

	buildOptions := types.ImageBuildOptions{
		Dockerfile:     "Dockerfile",
		Tags:           opts.Tags,
		NoCache:        opts.NoCache,
		Remove:         true,
		ForceRemove:    true,
		SuppressOutput: false,
	}

	buildResp, err := b.client.ImageBuild(ctx, buildContext, buildOptions)
	if err != nil {
		return "", fmt.Errorf("failed to start image build: %w", err)
	}
	defer buildResp.Body.Close()

	imageID, err := parseBuildOutput(buildResp.Body, opts.Output)
	if err != nil {
		return "", err
	}

	return imageID, nil
}

type buildMessage struct {
	Stream      string          `json:"stream,omitempty"`
	Status      string          `json:"status,omitempty"`
	Error       string          `json:"error,omitempty"`
	ErrorDetail *errorDetail    `json:"errorDetail,omitempty"`
	Aux         json.RawMessage `json:"aux,omitempty"`
}

type errorDetail struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type auxMessage struct {
	ID string `json:"ID,omitempty"`
}

func parseBuildOutput(body io.Reader, output io.Writer) (string, error) {
	var imageID string
	scanner := bufio.NewScanner(body)

	for scanner.Scan() {
		line := scanner.Bytes()

		var msg buildMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		if msg.Error != "" {
			errMsg := msg.Error
			if msg.ErrorDetail != nil && msg.ErrorDetail.Message != "" {
				errMsg = msg.ErrorDetail.Message
			}
			return "", fmt.Errorf("build error: %s", errMsg)
		}

		if msg.Stream != "" && output != nil {
			output.Write([]byte(msg.Stream))
		}

		if msg.Status != "" && output != nil {
			output.Write([]byte(msg.Status + "\n"))
		}

		if len(msg.Aux) > 0 {
			var aux auxMessage
			if err := json.Unmarshal(msg.Aux, &aux); err == nil && aux.ID != "" {
				imageID = aux.ID
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read build output: %w", err)
	}

	if imageID == "" {
		return "", fmt.Errorf("build completed but no image ID found in output")
	}

	return imageID, nil
}

func (b *Builder) InvalidateCache(ctx context.Context, cfg *config.Config) error {
	if b.cache == nil {
		return nil
	}

	buildKey := b.GenerateBuildKey(cfg)
	return b.cache.Delete(ctx, buildKey)
}

func (b *Builder) CleanupCache(ctx context.Context, maxAge time.Duration) (int, error) {
	if b.cache == nil {
		return 0, nil
	}

	return b.cache.Cleanup(ctx, maxAge)
}
