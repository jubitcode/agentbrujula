package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// PushConfig holds the webhook configuration for a task.
type PushConfig struct {
	URL   string `json:"url"`
	Token string `json:"token,omitempty"`
}

// PushNotifierConfig configures push notification behavior.
type PushNotifierConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
	Timeout    time.Duration // per-request timeout
}

// DefaultPushNotifierConfig returns sensible defaults.
func DefaultPushNotifierConfig() PushNotifierConfig {
	return PushNotifierConfig{
		MaxRetries: 3,
		BaseDelay:  500 * time.Millisecond,
		MaxDelay:   10 * time.Second,
		Timeout:    10 * time.Second,
	}
}

// PushNotifier delivers webhook notifications when tasks reach terminal states.
type PushNotifier struct {
	mu      sync.RWMutex
	configs map[string]*PushConfig // taskID → push config
	client  *http.Client
	cfg     PushNotifierConfig
	logger  *slog.Logger
}

// NewPushNotifier creates a push notifier.
func NewPushNotifier(cfg PushNotifierConfig, logger *slog.Logger) *PushNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &PushNotifier{
		configs: make(map[string]*PushConfig),
		client:  &http.Client{Timeout: cfg.Timeout},
		cfg:     cfg,
		logger:  logger,
	}
}

// SetConfig registers a push notification webhook for a task.
func (n *PushNotifier) SetConfig(taskID string, cfg *PushConfig) error {
	if cfg == nil || cfg.URL == "" {
		return fmt.Errorf("push notifier: webhook url is required")
	}
	if err := validateWebhookURL(cfg.URL); err != nil {
		return fmt.Errorf("push notifier: %w", err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.configs[taskID] = cfg
	return nil
}

// privateRanges defines IP ranges that webhooks must not target.
var privateRanges = func() []*net.IPNet {
	cidrs := []string{
		"127.0.0.0/8",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16",
		"::1/128",
	}
	var nets []*net.IPNet
	for _, c := range cidrs {
		_, n, _ := net.ParseCIDR(c)
		nets = append(nets, n)
	}
	return nets
}()

// validateWebhookURL ensures the URL is safe to use as a webhook target,
// rejecting non-HTTP schemes and destinations that resolve to private/internal IPs.
func validateWebhookURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid webhook URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("webhook URL scheme %q not allowed; only http and https are permitted", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("webhook URL has no hostname")
	}

	ips, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("webhook URL host lookup failed: %w", err)
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		for _, cidr := range privateRanges {
			if cidr.Contains(ip) {
				return fmt.Errorf("webhook URL resolves to private/internal IP %s (blocked)", ipStr)
			}
		}
	}

	return nil
}

// GetConfig returns the push config for a task, or nil if none.
func (n *PushNotifier) GetConfig(taskID string) *PushConfig {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.configs[taskID]
}

// DeleteConfig removes the push config for a task.
func (n *PushNotifier) DeleteConfig(taskID string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.configs, taskID)
}

// Notify sends the task payload to the configured webhook URL.
// Retries with exponential backoff on failure. Fire-and-forget: errors
// are logged but do not affect the task state.
func (n *PushNotifier) Notify(taskID string, task *Task) error {
	n.mu.RLock()
	cfg := n.configs[taskID]
	n.mu.RUnlock()

	if cfg == nil {
		return nil // no push configured, not an error
	}

	payload, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("push notifier: marshal task: %w", err)
	}

	var lastErr error
	for attempt := range n.cfg.MaxRetries + 1 {
		if attempt > 0 {
			delay := n.backoff(attempt)
			time.Sleep(delay)
			n.logger.Info("push notification retry",
				"task_id", taskID,
				"attempt", attempt,
				"url", cfg.URL,
			)
		}

		lastErr = n.send(cfg, payload)
		if lastErr == nil {
			n.logger.Info("push notification delivered",
				"task_id", taskID,
				"url", cfg.URL,
			)
			return nil
		}

		n.logger.Warn("push notification failed",
			"task_id", taskID,
			"attempt", attempt,
			"url", cfg.URL,
			"error", lastErr,
		)
	}

	n.logger.Error("push notification exhausted retries",
		"task_id", taskID,
		"url", cfg.URL,
		"error", lastErr,
	)
	return fmt.Errorf("push notifier: exhausted %d retries: %w", n.cfg.MaxRetries, lastErr)
}

func (n *PushNotifier) send(cfg *PushConfig, payload []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), n.cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("post webhook: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Printf("failed to close webhook response body: %v", err)
		}
	}(resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	return fmt.Errorf("webhook returned status %d", resp.StatusCode)
}

func (n *PushNotifier) backoff(attempt int) time.Duration {
	delay := n.cfg.BaseDelay
	for range attempt {
		delay *= 2
		if delay > n.cfg.MaxDelay {
			delay = n.cfg.MaxDelay
			break
		}
	}
	// Full jitter
	if delay > 0 {
		delay = time.Duration(rand.Int64N(int64(delay)))
	}
	return delay
}
