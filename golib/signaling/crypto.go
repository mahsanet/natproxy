package signaling

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	"golang.org/x/crypto/hkdf"
)

// EncryptPayload encrypts data with AES-256-GCM using a key derived from obfsKey.
// Output: [12B nonce][ciphertext+tag]
func EncryptPayload(data []byte, obfsKey []byte) ([]byte, error) {
	key, err := deriveSignalingKey(obfsKey)
	if err != nil {
		return nil, fmt.Errorf("derive signaling key: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, data, nil)
	// [nonce][ciphertext+tag]
	result := make([]byte, len(nonce)+len(ciphertext))
	copy(result, nonce)
	copy(result[len(nonce):], ciphertext)
	return result, nil
}

// DecryptPayload decrypts data encrypted with EncryptPayload.
// Input: [12B nonce][ciphertext+tag]
func DecryptPayload(data []byte, obfsKey []byte) ([]byte, error) {
	key, err := deriveSignalingKey(obfsKey)
	if err != nil {
		return nil, fmt.Errorf("derive signaling key: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce := data[:nonceSize]
	ciphertext := data[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}

// deriveSignalingKey derives a 32-byte AES key from obfsKey using HKDF.
func deriveSignalingKey(obfsKey []byte) ([]byte, error) {
	hkdfReader := hkdf.New(sha256.New, obfsKey, []byte("natproxy-signaling-key"), []byte("aes-256-gcm"))
	key := make([]byte, 32)
	if _, err := hkdfReader.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}
