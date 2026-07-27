package embedbuf

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"

	"github.com/go-kratos/kratos/v3/config"
)

var _ config.Source = (*file)(nil)

var decKey = ""

type file struct {
	buf []byte
}

// NewSource new a file source.
func NewSource(buf []byte) config.Source {
	return &file{buf: buf}
}

func (f *file) loadFile() (*config.KeyValue, error) {
	key, err := hex.DecodeString(decKey)
	if err != nil {
		return nil, err
	}
	plain, err := decrypt(f.buf, key)
	if err != nil {
		return nil, err
	}
	return &config.KeyValue{
		Key:    "config.yaml",
		Format: "yaml",
		Value:  plain,
	}, nil
}

func (f *file) Load() (kvs []*config.KeyValue, err error) {
	kv, err := f.loadFile()
	if err != nil {
		return nil, err
	}
	return []*config.KeyValue{kv}, nil
}

func (f *file) Watch() (config.Watcher, error) {
	return &watcher{}, nil
}

var _ config.Watcher = (*watcher)(nil)

type watcher struct {
}

// Next implements [config.Watcher].
func (w *watcher) Next() ([]*config.KeyValue, error) {
	return []*config.KeyValue{}, nil
}

// Stop implements [config.Watcher].
func (w *watcher) Stop() error {
	return nil
}

func decrypt(cipherText []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher error: %v", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM error: %v", err)
	}

	nonceSize := gcm.NonceSize()
	if len(cipherText) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, cipherTextOnly := cipherText[:nonceSize], cipherText[nonceSize:]
	plain, err := gcm.Open(nil, nonce, cipherTextOnly, nil)
	if err != nil {
		return nil, fmt.Errorf("gcm.Open error: %v", err)
	}
	return plain, nil
}
