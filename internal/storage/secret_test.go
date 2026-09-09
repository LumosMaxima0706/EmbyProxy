package storage

import "testing"

func TestSecretEncryptionRoundTrip(t *testing.T) {
	t.Setenv("SPACESHIP_SECRET_ENCRYPTION_KEY", "test-master-key-which-is-long")
	ciphertext, err := EncryptSecret("spaceship-secret")
	if err != nil {
		t.Fatal(err)
	}
	if ciphertext == "spaceship-secret" {
		t.Fatal("secret stored in plaintext")
	}
	plaintext, err := DecryptSecret(ciphertext)
	if err != nil || plaintext != "spaceship-secret" {
		t.Fatalf("roundtrip=%q err=%v", plaintext, err)
	}
}

func TestSecretEncryptionRequiresMasterKey(t *testing.T) {
	t.Setenv("SPACESHIP_SECRET_ENCRYPTION_KEY", "")
	if _, err := EncryptSecret("secret"); err == nil {
		t.Fatal("expected missing master key error")
	}
}
