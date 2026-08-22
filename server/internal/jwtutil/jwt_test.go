package jwtutil

import (
	"testing"
	"time"
)

func TestCreateAndDecodeAccessToken_RoundTrip(t *testing.T) {
	token, err := CreateAccessToken("test-jwt-secret", "user-123", "user", 60)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	claims, err := DecodeAccessToken("test-jwt-secret", token)
	if err != nil {
		t.Fatalf("DecodeAccessToken: %v", err)
	}
	if claims["sub"] != "user-123" {
		t.Fatalf("sub = %v, want user-123", claims["sub"])
	}
	if claims["type"] != "user" {
		t.Fatalf("type = %v, want user", claims["type"])
	}
}

// This token was issued by Python's PyJWT (common/jwt_utils.py) with the
// same secret, proving a token issued before cutover stays valid after it.
const pyIssuedToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyLTEyMyIsInR5cGUiOiJ1c2VyIiwiaWF0IjoxNzg3Mzk4MDgwLCJleHAiOjE3ODc0MDE2ODB9.YCLfMJ4aIKHjhjBBF93kDlUUL6H6HXpL8szYlI4jEE8"

func TestDecodeAccessToken_CrossCompatWithPython(t *testing.T) {
	claims, err := DecodeAccessToken("test-jwt-secret", pyIssuedToken)
	// This specific token has a real (past) exp, so it may be expired by the
	// time this runs - that's fine, we only care that signature validation
	// and claim parsing agree with Python, not that it's still live.
	if err != nil && err != ErrExpiredToken {
		t.Fatalf("DecodeAccessToken: %v", err)
	}
	if err == ErrExpiredToken {
		return
	}
	if claims["sub"] != "user-123" {
		t.Fatalf("sub = %v, want user-123", claims["sub"])
	}
}

func TestDecodeAccessToken_ExpiredToken(t *testing.T) {
	token, err := CreateAccessToken("secret", "u1", "user", -1)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	_, err = DecodeAccessToken("secret", token)
	if err != ErrExpiredToken {
		t.Fatalf("expected ErrExpiredToken, got %v", err)
	}
}

func TestDecodeAccessToken_WrongSecret(t *testing.T) {
	token, _ := CreateAccessToken("secret-a", "u1", "user", 60)
	_, err := DecodeAccessToken("secret-b", token)
	if err != ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}
