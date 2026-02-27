package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// GenerateWebhookSignature creates an HMAC-SHA256 signature for webhook payload
// This signature allows the receiver to verify that the webhook came from our system
// and the payload has not been tampered with
func GenerateWebhookSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyWebhookSignature validates an HMAC signature against the payload and secret
// Returns true if the signature is valid, false otherwise
// This can be used for testing or if we need to verify webhooks from external systems
func VerifyWebhookSignature(payload []byte, secret string, signature string) bool {
	expectedSignature := GenerateWebhookSignature(payload, secret)
	// Use constant-time comparison to prevent timing attacks
	return hmac.Equal([]byte(expectedSignature), []byte(signature))
}
