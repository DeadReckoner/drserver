package revenuecat

import (
	"errors"
	"testing"
)

func TestVerifyAuth_NoSecret(t *testing.T) {
	t.Setenv("REVENUECAT_WEBHOOK_SECRET", "")
	if err := VerifyAuth("anything"); !errors.Is(err, ErrSecretNotConfigured) {
		t.Fatalf("want ErrSecretNotConfigured, got %v", err)
	}
}

func TestVerifyAuth_Mismatch(t *testing.T) {
	t.Setenv("REVENUECAT_WEBHOOK_SECRET", "s3cret")
	if err := VerifyAuth("wrong"); !errors.Is(err, ErrAuthMismatch) {
		t.Fatalf("want ErrAuthMismatch, got %v", err)
	}
}

func TestVerifyAuth_Empty(t *testing.T) {
	t.Setenv("REVENUECAT_WEBHOOK_SECRET", "s3cret")
	if err := VerifyAuth(""); !errors.Is(err, ErrAuthMismatch) {
		t.Fatalf("want ErrAuthMismatch on empty header, got %v", err)
	}
}

func TestVerifyAuth_Match(t *testing.T) {
	t.Setenv("REVENUECAT_WEBHOOK_SECRET", "s3cret")
	if err := VerifyAuth("s3cret"); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}
