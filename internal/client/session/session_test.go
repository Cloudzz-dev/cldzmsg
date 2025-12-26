package session

import (
	"encoding/json"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	originalData := "This is a secret message"
	
	// Test basic string
	encrypted, err := encrypt([]byte(originalData))
	if err != nil {
		t.Fatalf("Failed to encrypt: %v", err)
	}
	
	if encrypted == "" {
		t.Fatal("Encrypted string is empty")
	}
	
	decrypted, err := decrypt(encrypted)
	if err != nil {
		t.Fatalf("Failed to decrypt: %v", err)
	}
	
	if string(decrypted) != originalData {
		t.Errorf("Expected %q, got %q", originalData, string(decrypted))
	}
}

func TestSessionSerialization(t *testing.T) {
	originalSession := Session{
		ServerURL: "wss://test.com",
		Username:  "testuser",
		Password:  "secretpassword",
	}
	
	data, err := json.Marshal(originalSession)
	if err != nil {
		t.Fatalf("Failed to marshal session: %v", err)
	}
	
	encrypted, err := encrypt(data)
	if err != nil {
		t.Fatalf("Failed to encrypt session: %v", err)
	}
	
	decryptedData, err := decrypt(encrypted)
	if err != nil {
		t.Fatalf("Failed to decrypt session: %v", err)
	}
	
	var restoredSession Session
	if err := json.Unmarshal(decryptedData, &restoredSession); err != nil {
		t.Fatalf("Failed to unmarshal restored session: %v", err)
	}
	
	if restoredSession != originalSession {
		t.Errorf("Expected %+v, got %+v", originalSession, restoredSession)
	}
}
