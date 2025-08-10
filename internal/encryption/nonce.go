package encryption

import "github.com/sethvargo/go-password/password"

const fileNonceSize = 24

type nonce [fileNonceSize]byte

func (n nonce) ToBytes() []byte {
	return n[:]
}

func (n *nonce) ToString() string {
	return string(n[:])
}

// fromReader fills the nonce from an io.Reader - normally the OSes
// crypto random number generator
func GenerateRandomNonce() (nonce, error) {
	pass, err := password.Generate(24, 10, 0, false, false)
	if err != nil {
		return nonce{}, err
	}

	var n nonce
	copy(n[:], pass)

	return n, nil
}
