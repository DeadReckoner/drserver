package email

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// resendEndpoint is overridable in tests via the ResendClient field. Public
// API URL is the only one used in production.
const resendEndpoint = "https://api.resend.com/emails"

// defaultMaxAttempts is the retry ceiling for Resend sends. The first attempt
// counts as 1, so 5 = up to 4 retries.
const defaultMaxAttempts = 5

// defaultInitialBackoff is the wait before the first retry; subsequent
// retries double until maxBackoff.
const (
	defaultInitialBackoff = 500 * time.Millisecond
	defaultMaxBackoff     = 16 * time.Second
)

// ResendClient is the thin HTTP client used to talk to Resend's send
// endpoint. Tests construct one with a custom HTTPClient + Endpoint.
type ResendClient struct {
	APIKey         string
	From           string
	HTTPClient     *http.Client
	Endpoint       string
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration

	// Sleep lets tests inject a fast no-op sleep. Production uses time.Sleep.
	Sleep func(time.Duration)
}

// NewResendClient constructs a production-shaped Resend client from env vars:
//   - RESEND_API_KEY  (required for real sends; empty -> Send returns an error)
//   - FROM_EMAIL      (optional; defaults to "noreply@deadreckoner.io")
//
// If RESEND_API_KEY is unset, the client is still constructed but every Send
// fails fast with ErrNotConfigured. This lets the rest of the dispatch path
// run in dev/test environments without polluting Resend, while preserving
// the audit row for visibility.
func NewResendClient() *ResendClient {
	from := os.Getenv("FROM_EMAIL")
	if from == "" {
		from = "noreply@deadreckoner.io"
	}
	return &ResendClient{
		APIKey:         os.Getenv("RESEND_API_KEY"),
		From:           from,
		HTTPClient:     &http.Client{Timeout: 10 * time.Second},
		Endpoint:       resendEndpoint,
		MaxAttempts:    defaultMaxAttempts,
		InitialBackoff: defaultInitialBackoff,
		MaxBackoff:     defaultMaxBackoff,
		Sleep:          time.Sleep,
	}
}

// ErrNotConfigured indicates RESEND_API_KEY was unset at send time. Callers
// log this and move on; the audit row is still written with status=error so
// it is visible in support workflows.
var ErrNotConfigured = errors.New("email: RESEND_API_KEY not configured")

// SendRequest is the payload we POST to Resend.
type SendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
}

// SendResponse is the field of Resend's reply we care about: the message ID
// we record in email_log.provider_message_id.
type SendResponse struct {
	ID string `json:"id"`
}

// errorResponse covers Resend's standard error envelope: {"message":"...","name":"..."}
type errorResponse struct {
	Message string `json:"message"`
	Name    string `json:"name"`
}

// Send delivers one email with exponential backoff. Returns the Resend
// message ID on success. Errors fall into three buckets:
//   - ErrNotConfigured: API key missing; do not retry
//   - context cancelled: caller bailed; do not retry
//   - all other errors: retried up to MaxAttempts with backoff doubling
//
// 4xx responses (except 429) are treated as permanent and not retried, since
// retrying a malformed payload will just hit the same wall.
func (c *ResendClient) Send(ctx context.Context, req SendRequest) (string, error) {
	if c.APIKey == "" {
		return "", ErrNotConfigured
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	backoff := c.InitialBackoff
	if backoff <= 0 {
		backoff = defaultInitialBackoff
	}
	maxBackoff := c.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = defaultMaxBackoff
	}
	maxAttempts := c.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}
	sleep := c.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		id, err, retryable := c.attempt(ctx, payload)
		if err == nil {
			return id, nil
		}
		lastErr = err
		if !retryable || attempt == maxAttempts {
			return "", lastErr
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
			sleep(backoff)
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
	return "", lastErr
}

// attempt does a single POST and classifies the outcome:
//   - (id, nil, _)         -> success
//   - (_, err, true)       -> retry-worthy (5xx, 429, network)
//   - (_, err, false)      -> permanent (4xx other than 429)
func (c *ResendClient) attempt(ctx context.Context, payload []byte) (string, error, bool) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err, false
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		// Network-level error: connection refused, DNS, timeout. Retry-worthy.
		return "", err, true
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		var sr SendResponse
		if err := json.Unmarshal(body, &sr); err != nil || sr.ID == "" {
			return "", fmt.Errorf("resend: unexpected success body: %s", string(body)), false
		}
		return sr.ID, nil, false
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		// Rate limit or upstream error - retry.
		return "", fmt.Errorf("resend: %d %s", resp.StatusCode, summarizeErrorBody(body)), true
	default:
		// 4xx other than 429 - permanent. Don't waste retries.
		return "", fmt.Errorf("resend: %d %s", resp.StatusCode, summarizeErrorBody(body)), false
	}
}

func summarizeErrorBody(b []byte) string {
	var er errorResponse
	if json.Unmarshal(b, &er) == nil && er.Message != "" {
		if er.Name != "" {
			return fmt.Sprintf("%s: %s", er.Name, er.Message)
		}
		return er.Message
	}
	if len(b) > 200 {
		return string(b[:200]) + "..."
	}
	return string(b)
}
