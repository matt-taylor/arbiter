package downstream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// KillswitchResponse represents the response from Killswitch
type KillswitchResponse struct {
	Status       int
	Decision     string
	Rule         string
	Reason       string
	ResponseType string
	RetryAfter   string
	LatencyMs    float64 // Latency of the killswitch check
}

// GatekeeperResponse represents the response from Gatekeeper
type GatekeeperResponse struct {
	Status          int
	IdentityHeaders map[string]string
	LatencyMs       float64 // Latency of the gatekeeper check
}

var gatekeeperIdentityHeaderNames = []string{
	"X-Identity-Type",
	"X-Identity-Id",
	"X-Identity-Scopes",
	"X-Identity-Groups",
	"X-Identity-User-Id",
	"X-Identity-Email",
	"X-Identity-Username",
	"X-Identity-Service-Id",
	"X-Identity-Node-Id",
}

// Client handles HTTP calls to downstream services
type Client struct {
	killswitchClient   *http.Client
	gatekeeperClient   *http.Client
	killswitchBaseURL  string
	gatekeeperBaseURL  string
}

// NewClient creates a new downstream client
func NewClient(killswitchBaseURL, gatekeeperBaseURL string, killswitchTimeout, gatekeeperTimeout time.Duration) *Client {
	return &Client{
		killswitchClient: &http.Client{
			Timeout: killswitchTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:      90 * time.Second,
				DisableKeepAlives:    false,
			},
		},
		gatekeeperClient: &http.Client{
			Timeout: gatekeeperTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:      90 * time.Second,
				DisableKeepAlives:    false,
			},
		},
		killswitchBaseURL: killswitchBaseURL,
		gatekeeperBaseURL: gatekeeperBaseURL,
	}
}

// CheckKillswitch calls the Killswitch check endpoint
func (c *Client) CheckKillswitch(ctx context.Context, headers map[string]string) (*KillswitchResponse, error) {
	url := c.killswitchBaseURL + "/api/v1/check"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Forward headers
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// Track latency
	startTime := time.Now()
	resp, err := c.killswitchClient.Do(req)
	latency := time.Since(startTime)
	latencyMs := float64(latency.Nanoseconds()) / 1e6

	if err != nil {
		// Return error with latency measured up to failure point
		return &KillswitchResponse{
			Status:    0,
			LatencyMs: latencyMs,
		}, fmt.Errorf("killswitch request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read and discard body
	io.Copy(io.Discard, resp.Body)

	// Parse latency from header if available, otherwise use measured latency
	// Try new header name first, fall back to old for compatibility
	if latencyHeader := resp.Header.Get("X-Arbiter-Latency-KS"); latencyHeader != "" {
		if parsed, err := strconv.ParseFloat(latencyHeader, 64); err == nil {
			latencyMs = parsed
		}
	} else if latencyHeader := resp.Header.Get("X-Killswitch-Latency-Ms"); latencyHeader != "" {
		if parsed, err := strconv.ParseFloat(latencyHeader, 64); err == nil {
			latencyMs = parsed
		}
	}

	result := &KillswitchResponse{
		Status:       resp.StatusCode,
		Decision:     resp.Header.Get("X-Killswitch-Decision"),
		Rule:         resp.Header.Get("X-Killswitch-Rule"),
		Reason:       resp.Header.Get("X-Killswitch-Reason"),
		ResponseType: resp.Header.Get("X-Killswitch-Response-Type"),
		RetryAfter:   resp.Header.Get("Retry-After"),
		LatencyMs:    latencyMs,
	}

	return result, nil
}

// AuthorizeGatekeeper calls the Gatekeeper authorize endpoint
func (c *Client) AuthorizeGatekeeper(ctx context.Context, headers map[string]string) (*GatekeeperResponse, error) {
	url := c.gatekeeperBaseURL + "/api/v1/authorize"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Forward headers (including Cookie verbatim)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// Track latency
	startTime := time.Now()
	resp, err := c.gatekeeperClient.Do(req)
	latency := time.Since(startTime)
	latencyMs := float64(latency.Nanoseconds()) / 1e6

	if err != nil {
		// Return error with latency measured up to failure point
		return &GatekeeperResponse{
			Status:   0,
			LatencyMs: latencyMs,
		}, fmt.Errorf("gatekeeper request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read and discard body
	io.Copy(io.Discard, resp.Body)

	// Parse latency from header if available, otherwise use measured latency
	// Try new header name first, fall back to old for compatibility
	if latencyHeader := resp.Header.Get("X-Arbiter-Latency-GK"); latencyHeader != "" {
		if parsed, err := strconv.ParseFloat(latencyHeader, 64); err == nil {
			latencyMs = parsed
		}
	} else if latencyHeader := resp.Header.Get("X-Gatekeeper-Latency-Ms"); latencyHeader != "" {
		if parsed, err := strconv.ParseFloat(latencyHeader, 64); err == nil {
			latencyMs = parsed
		}
	}

	result := &GatekeeperResponse{
		Status:          resp.StatusCode,
		IdentityHeaders: parseGatekeeperIdentityHeaders(resp.Header),
		LatencyMs:       latencyMs,
	}

	return result, nil
}

func parseGatekeeperIdentityHeaders(header http.Header) map[string]string {
	identityHeaders := make(map[string]string)
	for _, name := range gatekeeperIdentityHeaderNames {
		if value := header.Get(name); value != "" {
			identityHeaders[name] = value
		}
	}
	return identityHeaders
}
