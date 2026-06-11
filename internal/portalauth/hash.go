package portalauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
)

// RandomPassword generates a random alphanumeric password of the given length.
// It avoids ambiguous characters (0, O, 1, l, I).
func RandomPassword(length int) (string, error) {
	if length < 8 {
		length = 8
	}
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	buffer := make([]byte, length)
	random := make([]byte, length)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	for idx := range buffer {
		buffer[idx] = alphabet[int(random[idx])%len(alphabet)]
	}
	return string(buffer), nil
}

// HashPassword creates a new salt and hashes the password.
// Returns the hex-encoded hash and salt.
func HashPassword(password string) (hash string, salt string, err error) {
	if password == "" {
		return "", "", errors.New("password cannot be empty")
	}
	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err != nil {
		return "", "", err
	}
	return derivePassword(password, saltBytes), hex.EncodeToString(saltBytes), nil
}

// VerifyPassword checks whether the provided password matches the stored hash.
func VerifyPassword(password, saltHex, expectedHash string) bool {
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return false
	}
	actual := derivePassword(password, salt)
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expectedHash)) == 1
}

// derivePassword applies HMAC-SHA256 with 120,000 iterations.
func derivePassword(password string, salt []byte) string {
	mac := hmac.New(sha256.New, salt)
	_, _ = mac.Write([]byte(password))
	derived := mac.Sum(nil)
	for idx := 0; idx < 120000; idx++ {
		next := hmac.New(sha256.New, salt)
		_, _ = next.Write(derived)
		derived = next.Sum(nil)
	}
	return hex.EncodeToString(derived)
}
