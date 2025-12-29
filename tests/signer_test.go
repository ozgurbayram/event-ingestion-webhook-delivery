package tests

import (
	"testing"
	"time"

	"event-intestion/internal/worker"
)

func TestSignerSign(t *testing.T) {
	signer := worker.NewSigner()

	payload := []byte(`{"event_id":"123","event_type":"order.created"}`)
	secret := "test-secret"
	timestamp := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	signature := signer.Sign(payload, secret, timestamp)

	if signature == "" {
		t.Error("expected signature to be non-empty")
	}

	if len(signature) < 20 {
		t.Error("signature seems too short")
	}

	expectedPrefix := "t=1704110400,v1="
	if signature[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("expected signature to start with '%s', got '%s'", expectedPrefix, signature[:len(expectedPrefix)])
	}
}

func TestSignerDeterministic(t *testing.T) {
	signer := worker.NewSigner()

	payload := []byte(`{"test":"data"}`)
	secret := "my-secret"
	timestamp := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)

	sig1 := signer.Sign(payload, secret, timestamp)
	sig2 := signer.Sign(payload, secret, timestamp)

	if sig1 != sig2 {
		t.Errorf("signatures should be deterministic: '%s' != '%s'", sig1, sig2)
	}
}

func TestSignerDifferentPayloads(t *testing.T) {
	signer := worker.NewSigner()

	secret := "my-secret"
	timestamp := time.Now()

	sig1 := signer.Sign([]byte(`{"data":"one"}`), secret, timestamp)
	sig2 := signer.Sign([]byte(`{"data":"two"}`), secret, timestamp)

	if sig1 == sig2 {
		t.Error("different payloads should produce different signatures")
	}
}

func TestSignerDifferentSecrets(t *testing.T) {
	signer := worker.NewSigner()

	payload := []byte(`{"test":"data"}`)
	timestamp := time.Now()

	sig1 := signer.Sign(payload, "secret-one", timestamp)
	sig2 := signer.Sign(payload, "secret-two", timestamp)

	if sig1 == sig2 {
		t.Error("different secrets should produce different signatures")
	}
}

func TestSignerDifferentTimestamps(t *testing.T) {
	signer := worker.NewSigner()

	payload := []byte(`{"test":"data"}`)
	secret := "my-secret"

	sig1 := signer.Sign(payload, secret, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sig2 := signer.Sign(payload, secret, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC))

	if sig1 == sig2 {
		t.Error("different timestamps should produce different signatures")
	}
}

func TestSignerVerify(t *testing.T) {
	signer := worker.NewSigner()

	payload := []byte(`{"event":"test"}`)
	secret := "verify-secret"
	timestamp := time.Now()

	signature := signer.Sign(payload, secret, timestamp)

	valid := signer.Verify(payload, secret, signature, 5*time.Minute)
	if !valid {
		t.Error("signature should be valid")
	}
}

func TestSignerVerifyWrongSecret(t *testing.T) {
	signer := worker.NewSigner()

	payload := []byte(`{"event":"test"}`)
	timestamp := time.Now()

	signature := signer.Sign(payload, "correct-secret", timestamp)

	valid := signer.Verify(payload, "wrong-secret", signature, 5*time.Minute)
	if valid {
		t.Error("signature should be invalid with wrong secret")
	}
}

func TestSignerVerifyExpired(t *testing.T) {
	signer := worker.NewSigner()

	payload := []byte(`{"event":"test"}`)
	secret := "my-secret"
	oldTimestamp := time.Now().Add(-10 * time.Minute)

	signature := signer.Sign(payload, secret, oldTimestamp)

	valid := signer.Verify(payload, secret, signature, 5*time.Minute)
	if valid {
		t.Error("signature should be invalid when expired")
	}
}
