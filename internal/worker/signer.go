package worker

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

type Signer struct{}

func NewSigner() *Signer {
	return &Signer{}
}

func (s *Signer) Sign(payload []byte, secret string, timestamp time.Time) string {
	ts := strconv.FormatInt(timestamp.Unix(), 10)
	signedPayload := ts + "." + string(payload)

	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(signedPayload))
	signature := hex.EncodeToString(h.Sum(nil))

	return fmt.Sprintf("t=%s,v1=%s", ts, signature)
}

func (s *Signer) Verify(payload []byte, secret, signatureHeader string, maxAge time.Duration) bool {
	ts, _, err := parseSignatureHeader(signatureHeader)
	if err != nil {
		return false
	}

	timestamp := time.Unix(ts, 0)
	if time.Since(timestamp) > maxAge {
		return false
	}

	expectedSig := s.Sign(payload, secret, timestamp)
	return hmac.Equal([]byte(signatureHeader), []byte(expectedSig))
}

func parseSignatureHeader(header string) (int64, string, error) {
	var ts int64
	var sig string

	_, err := fmt.Sscanf(header, "t=%d,v1=%s", &ts, &sig)
	if err != nil {
		return 0, "", err
	}

	return ts, sig, nil
}
