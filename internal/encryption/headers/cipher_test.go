package headers

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/sethvargo/go-password/password"
	"github.com/stretchr/testify/assert"
)

func TestEncryptDecryptStream(t *testing.T) {
	ctx := context.Background()

	// Create a buffer for the input data
	inputData := []byte("This is some test data")
	input := bytes.NewBuffer(inputData)

	// Derive a random key and nonce for encryption
	pass, err := password.Generate(15, 5, 0, false, false)
	if err != nil {
		t.Errorf("Failed to generate password: %v", err)
	}

	salt, err := password.Generate(8, 4, 0, false, false)
	if err != nil {
		t.Errorf("Failed to generate salt: %v", err)
	}

	key, err := deriveKey(pass, salt)
	if err != nil {
		t.Errorf("Failed to derive key: %v", err)
	}

	nonce, err := GenerateRandomNonce()
	assert.NoError(t, err)

	// Encrypt the input data
	outputEncrypt := encryptStream(ctx, key, nonce.ToBytes(), input)
	if err != nil {
		t.Errorf("Failed to encrypt stream: %v", err)
	}

	// Decrypt the encrypted data
	outputDecrypt := decryptStream(ctx, key, nonce.ToBytes(), outputEncrypt)
	if err != nil {
		t.Errorf("Failed to decrypt stream: %v", err)
	}

	// Check that the decrypted data matches the input data
	decrypted, err := io.ReadAll(outputDecrypt)
	if err != nil {
		t.Errorf("Failed to read decrypted data: %v", err)
	}

	assert.Equal(t, inputData, decrypted)
}
