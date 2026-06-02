package email

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newClient returns a ResendClient pointed at the given test server, with
// retry timings zeroed out so the suite runs fast.
func newClient(serverURL, apiKey string) *ResendClient {
	return &ResendClient{
		APIKey:         apiKey,
		From:           "test@deadreckoner.io",
		HTTPClient:     &http.Client{Timeout: 2 * time.Second},
		Endpoint:       serverURL,
		MaxAttempts:    5,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     2 * time.Millisecond,
		Sleep:          func(time.Duration) {},
	}
}

func TestResend_NoAPIKey_ReturnsErrNotConfigured(t *testing.T) {
	c := newClient("http://unused", "")
	_, err := c.Send(context.Background(), SendRequest{To: []string{"a@b.com"}})
	require.ErrorIs(t, err, ErrNotConfigured)
}

func TestResend_HappyPath_FirstTry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), `"to":["a@b.com"]`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resend-msg-123"}`))
	}))
	defer srv.Close()

	c := newClient(srv.URL, "k")
	id, err := c.Send(context.Background(), SendRequest{
		From:    "f@x.com",
		To:      []string{"a@b.com"},
		Subject: "Hi",
		HTML:    "<p>hi</p>",
	})
	require.NoError(t, err)
	assert.Equal(t, "resend-msg-123", id)
	assert.EqualValues(t, 1, calls)
}

func TestResend_429ThenSuccess_Retries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"rate limit","name":"rate_limit_exceeded"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"after-retry"}`))
	}))
	defer srv.Close()

	c := newClient(srv.URL, "k")
	id, err := c.Send(context.Background(), SendRequest{To: []string{"a@b.com"}})
	require.NoError(t, err)
	assert.Equal(t, "after-retry", id)
	assert.EqualValues(t, 3, calls, "should retry twice before succeeding on the 3rd")
}

func TestResend_500ExhaustsAllRetries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"oops"}`))
	}))
	defer srv.Close()

	c := newClient(srv.URL, "k")
	_, err := c.Send(context.Background(), SendRequest{To: []string{"a@b.com"}})
	require.Error(t, err)
	assert.EqualValues(t, 5, calls, "MaxAttempts=5 should send 5 requests total")
	assert.True(t, strings.Contains(err.Error(), "500"))
}

func TestResend_400_PermanentNoRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"bad email","name":"validation_error"}`))
	}))
	defer srv.Close()

	c := newClient(srv.URL, "k")
	_, err := c.Send(context.Background(), SendRequest{To: []string{"bad"}})
	require.Error(t, err)
	assert.EqualValues(t, 1, calls, "4xx (non-429) should not be retried")
	assert.True(t, strings.Contains(err.Error(), "validation_error"))
}

func TestResend_NetworkError_Retries(t *testing.T) {
	// Build server, close it immediately so Do() gets a connection error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	var sleeps int32
	c := newClient(srv.URL, "k")
	c.Sleep = func(time.Duration) { atomic.AddInt32(&sleeps, 1) }
	_, err := c.Send(context.Background(), SendRequest{To: []string{"a@b.com"}})
	require.Error(t, err)
	assert.EqualValues(t, 4, sleeps, "5 attempts -> 4 sleeps between them")
}

func TestResend_ContextCancelled_StopsImmediately(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling

	c := newClient(srv.URL, "k")
	_, err := c.Send(ctx, SendRequest{To: []string{"a@b.com"}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled), "should bubble context.Canceled")
}

func TestResend_AuthHeaderSet(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"x"}`))
	}))
	defer srv.Close()

	c := newClient(srv.URL, "supersecret")
	_, err := c.Send(context.Background(), SendRequest{To: []string{"a@b.com"}})
	require.NoError(t, err)
	assert.Equal(t, "Bearer supersecret", gotAuth)
}
