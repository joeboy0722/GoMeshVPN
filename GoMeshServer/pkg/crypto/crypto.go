package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

// GenerateKeyPair
func GenerateKeyPair() (*ecdh.PrivateKey, *ecdh.PublicKey, error) {
	curve := ecdh.X25519()
	privateKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return privateKey, privateKey.PublicKey(), nil
}

// ComputeSharedSecret計�??�享密鑰
func ComputeSharedSecret(privateKey *ecdh.PrivateKey, peerPublicKeyBytes []byte) ([]byte, error) {
	curve := ecdh.X25519()
	peerPublicKey, err := curve.NewPublicKey(peerPublicKeyBytes)
	if err != nil {
		return nil, err
	}
	return privateKey.ECDH(peerPublicKey)
}

// DeriveSessionKey 使用 SHA256 從共享�??��???Session Key (32 bytes for AES-256)
func DeriveSessionKey(sharedSecret []byte) []byte {
	hash := sha256.Sum256(sharedSecret)
	return hash[:]
}

// Encrypt 使用 AES-256-GCM ?��??��?
func Encrypt(plaintext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt 使用 AES-256-GCM �???��?
func Decrypt(ciphertext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("malformed ciphertext")
	}

	nonce := ciphertext[:gcm.NonceSize()]
	encryptedData := ciphertext[gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}
