package switcher

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	droidEncryptedAuthIVSize  = 16
	droidEncryptedAuthTagSize = 16
	droidAuthKeySize          = 32
)

type droidCredentials struct {
	AccessToken          string `json:"access_token"`
	RefreshToken         string `json:"refresh_token"`
	ActiveOrganizationID string `json:"active_organization_id,omitempty"`
}

func loadDroidCredentials(factoryHome string) (droidCredentials, error) {
	key, err := readDroidAuthKey(factoryHome)
	if err != nil {
		return droidCredentials{}, err
	}
	encrypted, err := os.ReadFile(filepath.Join(factoryHome, authFileName))
	if err != nil {
		return droidCredentials{}, err
	}
	plain, err := decryptDroidCredentials(strings.TrimSpace(string(encrypted)), key)
	if err != nil {
		return droidCredentials{}, err
	}
	var creds droidCredentials
	if err := json.Unmarshal(plain, &creds); err != nil {
		return droidCredentials{}, fmt.Errorf("parse Droid auth credentials: %w", err)
	}
	if strings.TrimSpace(creds.AccessToken) == "" {
		return droidCredentials{}, fmt.Errorf("Droid auth credentials are missing access_token")
	}
	if strings.TrimSpace(creds.RefreshToken) == "" {
		return droidCredentials{}, fmt.Errorf("Droid auth credentials are missing refresh_token")
	}
	creds.ActiveOrganizationID = strings.TrimSpace(creds.ActiveOrganizationID)
	return creds, nil
}

func saveDroidCredentials(factoryHome string, creds droidCredentials) error {
	key, err := readDroidAuthKey(factoryHome)
	if err != nil {
		return err
	}
	encrypted, err := encryptDroidCredentials(creds, key)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(factoryHome, authFileName), []byte(encrypted), 0o600)
}

func readDroidAuthKey(factoryHome string) ([]byte, error) {
	raw, err := os.ReadFile(filepath.Join(factoryHome, authKeyFileName))
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("decode Droid auth key: %w", err)
	}
	if len(key) != droidAuthKeySize {
		return nil, fmt.Errorf("invalid Droid auth key length: got %d, want %d", len(key), droidAuthKeySize)
	}
	return key, nil
}

func decryptDroidCredentials(encrypted string, key []byte) ([]byte, error) {
	parts := strings.Split(encrypted, ":")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid Droid auth file format")
	}
	iv, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode Droid auth IV: %w", err)
	}
	tag, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode Droid auth tag: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode Droid auth payload: %w", err)
	}
	if len(iv) != droidEncryptedAuthIVSize {
		return nil, fmt.Errorf("invalid Droid auth IV length: got %d, want %d", len(iv), droidEncryptedAuthIVSize)
	}
	if len(tag) != droidEncryptedAuthTagSize {
		return nil, fmt.Errorf("invalid Droid auth tag length: got %d, want %d", len(tag), droidEncryptedAuthTagSize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, len(iv))
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, iv, append(ciphertext, tag...), nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt Droid auth credentials: %w", err)
	}
	return plain, nil
}

func encryptDroidCredentials(creds droidCredentials, key []byte) (string, error) {
	plain, err := json.Marshal(creds)
	if err != nil {
		return "", err
	}
	iv := make([]byte, droidEncryptedAuthIVSize)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, len(iv))
	if err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, iv, plain, nil)
	ciphertext := sealed[:len(sealed)-droidEncryptedAuthTagSize]
	tag := sealed[len(sealed)-droidEncryptedAuthTagSize:]
	return strings.Join([]string{
		base64.StdEncoding.EncodeToString(iv),
		base64.StdEncoding.EncodeToString(tag),
		base64.StdEncoding.EncodeToString(ciphertext),
	}, ":"), nil
}

func prettyJSON(raw []byte) string {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return strings.TrimSpace(string(raw))
	}
	return out.String()
}
