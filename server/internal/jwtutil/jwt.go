// Package jwtutil issues and validates the same HS256 JWTs the Python
// service used (common/jwt_utils.py), with identical claims ("sub", "type",
// "exp", "iat") so tokens issued before the cutover remain valid after it.
package jwtutil

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrExpiredToken = errors.New("token has expired")
	ErrInvalidToken = errors.New("invalid token")
)

type Claims struct {
	Subject string `json:"sub"`
	Type    string `json:"type"`
	jwt.RegisteredClaims
}

// CreateAccessToken creates an HS256 JWT with subject/type/exp/iat claims,
// mirroring common/jwt_utils.py's create_access_token().
func CreateAccessToken(secret string, subject, tokenType string, expireMinutes int) (string, error) {
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"sub":  subject,
		"type": tokenType,
		"iat":  now.Unix(),
		"exp":  now.Add(time.Duration(expireMinutes) * time.Minute).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// DecodeAccessToken validates and decodes a JWT, mirroring
// common/jwt_utils.py's decode_access_token().
func DecodeAccessToken(secret, tokenString string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
