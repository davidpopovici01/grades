package portalserver

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// PortalClaims represents the JWT claims for an authenticated student.
type PortalClaims struct {
	StudentID int    `json:"studentId"`
	Username  string `json:"username"`
	jwt.RegisteredClaims
}

// JWTHelper handles signing and verification of portal tokens.
type JWTHelper struct {
	secret []byte
}

// NewJWTHelper creates a new JWT helper with the given secret.
func NewJWTHelper(secret []byte) *JWTHelper {
	return &JWTHelper{secret: secret}
}

// Sign creates a new JWT token for the given student.
func (j *JWTHelper) Sign(studentID int, username string, duration time.Duration) (string, error) {
	claims := PortalClaims{
		StudentID: studentID,
		Username:  username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secret)
}

// Verify parses and validates a JWT token string.
func (j *JWTHelper) Verify(tokenString string) (*PortalClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &PortalClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return j.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*PortalClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, errors.New("invalid token claims")
}
