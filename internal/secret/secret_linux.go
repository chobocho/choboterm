//go:build linux && !termux

package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// On Linux saved passwords are encrypted with a key kept in the desktop's
// keyring (Secret Service: GNOME Keyring, KWallet...). Without a keyring
// nothing is saved.

var ErrNoStore = errors.New("비밀번호를 저장할 키링(Secret Service)이 없습니다")

const keyringService, keyringUser = "choboterm", "secret-key"

// secretKey returns the AES key from the keyring, making it the first time.
func secretKey() ([]byte, error) {
	s, err := keyring.Get(keyringService, keyringUser)
	if err == nil {
		if k, err := base64.StdEncoding.DecodeString(s); err == nil && len(k) == 32 {
			return k, nil
		}
	} else if !errors.Is(err, keyring.ErrNotFound) {
		return nil, fmt.Errorf("%w: %v", ErrNoStore, err)
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	if err := keyring.Set(keyringService, keyringUser, base64.StdEncoding.EncodeToString(k)); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoStore, err)
	}
	return k, nil
}

func secretAEAD() (cipher.AEAD, error) {
	k, err := secretKey()
	if err != nil {
		return nil, err
	}
	b, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// Protect encrypts data with the key in the keyring: nonce, then sealed data.
func Protect(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, nil
	}
	g, err := secretAEAD()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return g.Seal(nonce, nonce, plain, nil), nil
}

// Unprotect decrypts data produced by protectSecret.
func Unprotect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	g, err := secretAEAD()
	if err != nil {
		return nil, err
	}
	if len(data) < g.NonceSize() {
		return nil, errors.New("저장된 비밀번호가 손상되었습니다")
	}
	return g.Open(nil, data[:g.NonceSize()], data[g.NonceSize():], nil)
}
