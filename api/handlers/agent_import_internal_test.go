package handlers

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"strings"
	"testing"

	"dr/server/agent/anthropic"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPDF(t *testing.T) {
	assert.True(t, isPDF([]byte("%PDF-1.7\n...")))
	assert.False(t, isPDF([]byte("PK\x03\x04")), "a docx/zip is not a PDF")
	assert.False(t, isPDF([]byte("{\"not\": \"a pdf\"}")))
	assert.False(t, isPDF(nil))
	assert.False(t, isPDF([]byte("%PDF")), "a truncated magic number is not enough")
}

func TestPlanUploadLimit(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want int64
	}{
		{name: "unset falls back to the default", env: "", want: defaultPlanUploadBytes},
		{name: "honors a valid override", env: "15728640", want: 15 << 20},
		// Above the ceiling the Messages API would reject the request outright,
		// so we clamp rather than honor it.
		{name: "clamps above the ceiling", env: "104857600", want: planUploadCeilingBytes},
		// A typo in one environment's config must not take plan import down.
		{name: "junk falls back to the default", env: "20MB", want: defaultPlanUploadBytes},
		{name: "zero falls back to the default", env: "0", want: defaultPlanUploadBytes},
		{name: "negative falls back to the default", env: "-1", want: defaultPlanUploadBytes},
		{name: "whitespace is trimmed", env: "  2097152  ", want: 2 << 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(planUploadEnvVar, tt.env)
			assert.Equal(t, tt.want, planUploadLimit(context.Background()))
		})
	}
}

func TestReadCappedUpload(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		limit   int64
		wantErr error
		wantLen int
	}{
		{name: "under the limit", body: "abc", limit: 10, wantLen: 3},
		{name: "exactly at the limit", body: "abcde", limit: 5, wantLen: 5},
		{name: "over the limit", body: "abcdef", limit: 5, wantErr: errUploadTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A header.Size that understates the body must not get past the
			// cap, so the reader itself has to enforce it.
			got, err := readCappedUpload(fakeMultipartFile{strings.NewReader(tt.body)}, tt.limit)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tt.wantLen)
		})
	}
}

func TestSanitizeUploadName(t *testing.T) {
	assert.Equal(t, "plan.pdf", sanitizeUploadName(&multipart.FileHeader{Filename: ""}))
	assert.Equal(t, "q3-plan.pdf", sanitizeUploadName(&multipart.FileHeader{Filename: "q3-plan.pdf"}))

	long := strings.Repeat("a", 300) + ".pdf"
	assert.Len(t, sanitizeUploadName(&multipart.FileHeader{Filename: long}), 120)
}

func TestImportFailureMessage(t *testing.T) {
	// A declined or oversized document is the user's to fix, so the copy has to
	// say what to do differently rather than inviting an identical retry.
	assert.Contains(t, importFailureMessage(anthropic.ErrPlanRefused), "without unrelated material")
	assert.Contains(t, importFailureMessage(anthropic.ErrPlanTooLarge), "splitting it")
	assert.Contains(t, importFailureMessage(errors.New("connection reset")), "Please try again")

	// Wrapped errors still map to their specific copy.
	wrapped := errors.Join(errors.New("import step 2"), anthropic.ErrPlanRefused)
	assert.Contains(t, importFailureMessage(wrapped), "without unrelated material")
}

// fakeMultipartFile adapts a reader to multipart.File for readCappedUpload.
// Only Read is exercised.
type fakeMultipartFile struct {
	*strings.Reader
}

func (fakeMultipartFile) Close() error { return nil }

var _ multipart.File = fakeMultipartFile{strings.NewReader("")}

// TestPDFMagicMatchesRealHeader documents the byte sequence we gate on.
func TestPDFMagicMatchesRealHeader(t *testing.T) {
	assert.True(t, bytes.HasPrefix([]byte("%PDF-1.4"), pdfMagic))
}
