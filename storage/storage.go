package storage

import (
	"bytes"
	"context"
	"dr/server/log"
	"fmt"
	"io"
	"net/http"
)

type StorageClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewStorageClient(projectURL, apiKey string) *StorageClient {
	return &StorageClient{
		baseURL:    projectURL + "/storage/v1",
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}

}

func (s *StorageClient) Upload(ctx context.Context, bucket, path, contentType string, data []byte) error {
	url := fmt.Sprintf("%s/object/%s/%s", s.baseURL, bucket, path)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		log.Error(ctx, "POST storage request error", err)
		return err
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", contentType)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Error(ctx, "POST storage response error", err)
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("upload failed: %s", body)
		log.Error(ctx, "POST storage error", err)
		return err
	}

	return nil
}

func (s *StorageClient) Download(ctx context.Context, bucket, path string) ([]byte, error) {
	url := fmt.Sprintf("%s/object/%s/%s", s.baseURL, bucket, path)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		log.Error(ctx, "GET storage request error", err)
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Error(ctx, "GET storage response error", err)
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("download failed: %s", body)
		log.Error(ctx, "GET storage error", err)
		return nil, err
	}

	return io.ReadAll(resp.Body)
}

func (s *StorageClient) Delete(ctx context.Context, bucket, path string) error {
	url := fmt.Sprintf("%s/object/%s/%s", s.baseURL, bucket, path)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		log.Error(ctx, "DELETE storage request error", err)
		return err
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Error(ctx, "DELETE storage response error", err)
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("delete failed: %s", body)
		log.Error(ctx, "DELETE storage error", err)
		return err
	}

	return nil
}

// PublicURL returns the public URL for a file (bucket must be public)
func (s *StorageClient) PublicURL(bucket, path string) string {
	return fmt.Sprintf("%s/object/public/%s/%s", s.baseURL, bucket, path)
}
