package session

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Session struct {
	ServerURL string `json:"server_url"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

func GetConfigDir(profileName string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "cldzmsg", profileName)
}

func getEncryptionKey() []byte {
	paths := []string{"/etc/machine-id", "/var/lib/dbus/machine-id"}
	var id string
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			id = strings.TrimSpace(string(data))
			break
		}
	}

	if id == "" {
		hostname, _ := os.Hostname()
		id = hostname
	}

	hash := sha256.Sum256([]byte(id))
	return hash[:]
}

func encrypt(data []byte) (string, error) {
	key := getEncryptionKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func decrypt(encoded string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}

	key := getEncryptionKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func Load(profileName string) *Session {
	configDir := GetConfigDir(profileName)
	if configDir == "" {
		return nil
	}

	data, err := os.ReadFile(filepath.Join(configDir, "session.json"))
	if err != nil {
		return nil
	}

	decrypted, err := decrypt(string(data))
	if err != nil {
		var session Session
		if err := json.Unmarshal(data, &session); err == nil {
			Save(profileName, session.ServerURL, session.Username, session.Password)
			return &session
		}
		return nil
	}

	var session Session
	if err := json.Unmarshal(decrypted, &session); err != nil {
		return nil
	}
	return &session
}

func Save(profileName, serverURL, username, password string) error {
	configDir := GetConfigDir(profileName)
	if configDir == "" {
		return fmt.Errorf("could not get config directory")
	}

	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}

	session := Session{ServerURL: serverURL, Username: username, Password: password}
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}

	encrypted, err := encrypt(data)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(configDir, "session.json"), []byte(encrypted), 0600)
}

func Clear(profileName string) {
	configDir := GetConfigDir(profileName)
	if configDir != "" {
		os.Remove(filepath.Join(configDir, "session.json"))
	}
}
