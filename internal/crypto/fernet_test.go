package crypto

import "testing"

// These vectors were generated with Python's `cryptography.fernet.Fernet`
// to prove Go and Python are byte-for-byte compatible on the same key.
const (
	pyKey   = "ojZqX4SzAxtx1F8PkfjXsVaTffW0vmHFbwkSei3gMmw="
	pyToken = "gAAAAABqiYeJMbUQhiGVlQnAIWxmPPONLc6fJM6gWbbm_NgD2JCZVSt8KQUDkCFDktx2vqfw2dgeyxyOmMXOj0eubUmbaC5Z0F1b_grhLozXL2-nCaVWTEs="
	pyPlain = "hello from python"
)

func TestDecryptValue_CrossCompatWithPython(t *testing.T) {
	got, err := DecryptValue(pyToken, pyKey)
	if err != nil {
		t.Fatalf("DecryptValue failed: %v", err)
	}
	if got != pyPlain {
		t.Fatalf("got %q, want %q", got, pyPlain)
	}
}

func TestEncryptDecryptValue_RoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	encrypted, err := EncryptValue("some secret value", key)
	if err != nil {
		t.Fatalf("EncryptValue: %v", err)
	}

	decrypted, err := DecryptValue(encrypted, key)
	if err != nil {
		t.Fatalf("DecryptValue: %v", err)
	}
	if decrypted != "some secret value" {
		t.Fatalf("got %q, want %q", decrypted, "some secret value")
	}
}

func TestEncryptValue_EmptyStringPassthrough(t *testing.T) {
	key, _ := GenerateKey()
	got, err := EncryptValue("", key)
	if err != nil {
		t.Fatalf("EncryptValue: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty passthrough, got %q", got)
	}
}

func TestDecryptValue_InvalidToken(t *testing.T) {
	key, _ := GenerateKey()
	_, err := DecryptValue("not-a-valid-token", key)
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestEncryptionService_ReEncryptForClient(t *testing.T) {
	masterKey, _ := GenerateKey()
	clientKey, _ := GenerateKey()
	svc := NewEncryptionService(masterKey)

	stored, err := svc.EncryptForStorage("db-secret")
	if err != nil {
		t.Fatalf("EncryptForStorage: %v", err)
	}

	decrypted, err := svc.DecryptFromStorage(stored)
	if err != nil {
		t.Fatalf("DecryptFromStorage: %v", err)
	}
	if decrypted != "db-secret" {
		t.Fatalf("got %q, want %q", decrypted, "db-secret")
	}

	clientEncrypted, err := svc.ReEncryptForClient(decrypted, clientKey)
	if err != nil {
		t.Fatalf("ReEncryptForClient: %v", err)
	}

	clientDecrypted, err := DecryptValue(clientEncrypted, clientKey)
	if err != nil {
		t.Fatalf("client-side DecryptValue: %v", err)
	}
	if clientDecrypted != "db-secret" {
		t.Fatalf("got %q, want %q", clientDecrypted, "db-secret")
	}
}
