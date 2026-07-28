package storage_test

// Tests for the Supabase Storage HTTP wrapper. Everything needed is exported,
// so these run as an external test package against an httptest server that
// captures each request (method, path, headers, body) and answers with a
// scripted status/body. No network, no real Supabase project.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dr/server/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const infraServiceKey = "test-service-key"

// infraCapturedReq snapshots one request seen by the fake storage server.
type infraCapturedReq struct {
	method string
	path   string
	header http.Header
	body   []byte
}

// infraNewStorageServer runs a fake storage endpoint that records every
// request on the returned channel and always responds status/respBody.
// The returned client points at it with the test service key.
func infraNewStorageServer(t *testing.T, status int, respBody string) (*storage.StorageClient, chan infraCapturedReq) {
	t.Helper()
	reqs := make(chan infraCapturedReq, 4)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reqs <- infraCapturedReq{
			method: r.Method,
			path:   r.URL.Path,
			header: r.Header.Clone(),
			body:   body,
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(ts.Close)
	return storage.NewStorageClient(ts.URL, infraServiceKey), reqs
}

// infraRecvReq pops the next captured request (bounded wait).
func infraRecvReq(t *testing.T, ch chan infraCapturedReq) infraCapturedReq {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for captured request")
		return infraCapturedReq{}
	}
}

// infraAssertAuthHeaders checks the service-role key is sent both as apikey
// and as the Authorization bearer (the RLS-bypass contract).
func infraAssertAuthHeaders(t *testing.T, h http.Header) {
	t.Helper()
	assert.Equal(t, infraServiceKey, h.Get("apikey"))
	assert.Equal(t, "Bearer "+infraServiceKey, h.Get("Authorization"))
}

func TestInfraStorageUpload_HappyPath(t *testing.T) {
	client, reqs := infraNewStorageServer(t, http.StatusOK, "")
	payload := []byte("png-bytes-here")

	err := client.Upload(context.Background(), "avatars", "u1/profile.png", "image/png", payload)
	require.NoError(t, err)

	req := infraRecvReq(t, reqs)
	assert.Equal(t, http.MethodPost, req.method)
	assert.Equal(t, "/storage/v1/object/avatars/u1/profile.png", req.path)
	infraAssertAuthHeaders(t, req.header)
	assert.Equal(t, "image/png", req.header.Get("Content-Type"))
	// x-upsert lets a re-upload replace an existing profile image (commit 8292522).
	assert.Equal(t, "true", req.header.Get("x-upsert"))
	assert.Equal(t, payload, req.body)
}

func TestInfraStorageUpload_Non2xx(t *testing.T) {
	rlsBody := `{"message":"new row violates row-level security policy"}`
	client, reqs := infraNewStorageServer(t, http.StatusBadRequest, rlsBody)

	err := client.Upload(context.Background(), "avatars", "u1.png", "image/png", []byte("x"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upload failed")
	assert.Contains(t, err.Error(), "row-level security policy")
	infraRecvReq(t, reqs) // drain
}

func TestInfraStorageDownload_HappyPath(t *testing.T) {
	client, reqs := infraNewStorageServer(t, http.StatusOK, "file-content-bytes")

	data, err := client.Download(context.Background(), "avatars", "u1.png")
	require.NoError(t, err)
	assert.Equal(t, []byte("file-content-bytes"), data)

	req := infraRecvReq(t, reqs)
	assert.Equal(t, http.MethodGet, req.method)
	assert.Equal(t, "/storage/v1/object/avatars/u1.png", req.path)
	infraAssertAuthHeaders(t, req.header)
}

func TestInfraStorageDownload_Non2xx(t *testing.T) {
	client, reqs := infraNewStorageServer(t, http.StatusNotFound, "Object not found")

	data, err := client.Download(context.Background(), "avatars", "missing.png")
	require.Error(t, err)
	assert.Nil(t, data)
	assert.Contains(t, err.Error(), "download failed")
	assert.Contains(t, err.Error(), "Object not found")
	infraRecvReq(t, reqs) // drain
}

func TestInfraStorageDelete_HappyPath(t *testing.T) {
	client, reqs := infraNewStorageServer(t, http.StatusOK, "")

	err := client.Delete(context.Background(), "avatars", "u1.png")
	require.NoError(t, err)

	req := infraRecvReq(t, reqs)
	assert.Equal(t, http.MethodDelete, req.method)
	assert.Equal(t, "/storage/v1/object/avatars/u1.png", req.path)
	infraAssertAuthHeaders(t, req.header)
}

func TestInfraStorageDelete_Non2xx(t *testing.T) {
	client, reqs := infraNewStorageServer(t, http.StatusInternalServerError, "boom")

	err := client.Delete(context.Background(), "avatars", "u1.png")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete failed")
	infraRecvReq(t, reqs) // drain
}

func TestInfraStorageTransportErrors(t *testing.T) {
	// A closed server yields a connection-refused from the transport; each
	// method must surface it rather than swallow it.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ts.Close()
	client := storage.NewStorageClient(ts.URL, infraServiceKey)
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		{"Upload", func() error { return client.Upload(ctx, "b", "p", "text/plain", []byte("x")) }},
		{"Download", func() error { _, err := client.Download(ctx, "b", "p"); return err }},
		{"Delete", func() error { return client.Delete(ctx, "b", "p") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Error(t, tc.call())
		})
	}
}

func TestInfraStorageRequestCreationErrors(t *testing.T) {
	// A control character in the path makes http.NewRequestWithContext fail
	// before any request is sent.
	client, reqs := infraNewStorageServer(t, http.StatusOK, "")
	ctx := context.Background()
	const badPath = "bad\npath"

	cases := []struct {
		name string
		call func() error
	}{
		{"Upload", func() error { return client.Upload(ctx, "b", badPath, "text/plain", []byte("x")) }},
		{"Download", func() error { _, err := client.Download(ctx, "b", badPath); return err }},
		{"Delete", func() error { return client.Delete(ctx, "b", badPath) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Error(t, tc.call())
		})
	}

	// Nothing should ever have reached the server.
	select {
	case req := <-reqs:
		t.Fatalf("unexpected request reached the server: %s %s", req.method, req.path)
	default:
	}
}

func TestInfraStorageContextCanceled(t *testing.T) {
	client, _ := infraNewStorageServer(t, http.StatusOK, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled: Do must fail with the context error

	cases := []struct {
		name string
		call func() error
	}{
		{"Upload", func() error { return client.Upload(ctx, "b", "p", "text/plain", []byte("x")) }},
		{"Download", func() error { _, err := client.Download(ctx, "b", "p"); return err }},
		{"Delete", func() error { return client.Delete(ctx, "b", "p") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			require.Error(t, err)
			assert.True(t, errors.Is(err, context.Canceled), "want context.Canceled, got %v", err)
		})
	}
}

func TestInfraStoragePublicURL(t *testing.T) {
	client := storage.NewStorageClient("https://proj.supabase.co", infraServiceKey)
	assert.Equal(t,
		"https://proj.supabase.co/storage/v1/object/public/avatars/u1.png",
		client.PublicURL("avatars", "u1.png"))
}
