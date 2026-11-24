package publisher

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type credentialStore struct {
	dir string
}

type encryptedPayload struct {
	Version int    `json:"v"`
	Nonce   string `json:"nonce"`
	Data    string `json:"data"`
}

func newCredentialStore(settingsPath string) *credentialStore {
	if strings.TrimSpace(settingsPath) == "" {
		return nil
	}
	return &credentialStore{dir: filepath.Dir(settingsPath)}
}

func normalizeOrigin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimRight(strings.TrimSpace(raw), "/")
	}
	return strings.ToLower(u.Scheme+"://"+u.Host) + strings.TrimRight(u.EscapedPath(), "/")
}

func (s *credentialStore) filePath() string { return filepath.Join(s.dir, "credentials.enc") }
func (s *credentialStore) keyPath() string  { return filepath.Join(s.dir, ".credentials.key") }

func (s *credentialStore) deriveKey(create bool) ([]byte, error) {
	if s == nil {
		return nil, errors.New("未配置凭据存储目录")
	}
	raw, err := os.ReadFile(s.keyPath())
	if err != nil {
		if !os.IsNotExist(err) || !create {
			return nil, err
		}
		if err := os.MkdirAll(s.dir, 0700); err != nil {
			return nil, err
		}
		raw = make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, raw); err != nil {
			return nil, err
		}
		if err := writePrivateFile(s.keyPath(), raw); err != nil {
			return nil, err
		}
	}
	if len(raw) < 16 {
		return nil, errors.New("本地凭据密钥已损坏")
	}
	host, _ := os.Hostname()
	h := sha256.New()
	h.Write([]byte("hive-bridge-cred-v1:"))
	h.Write([]byte(host))
	h.Write([]byte(":"))
	h.Write([]byte(s.dir))
	h.Write([]byte(":"))
	h.Write(raw)
	return h.Sum(nil), nil
}

func (s *credentialStore) loadAll() (map[string]Credentials, error) {
	out := map[string]Credentials{}
	if s == nil {
		return out, nil
	}
	raw, err := os.ReadFile(s.filePath())
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	var env encryptedPayload
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errors.New("本地凭据文件格式无效")
	}
	if env.Version != 1 {
		return nil, errors.New("不支持的本地凭据版本")
	}
	key, err := s.deriveKey(false)
	if err != nil {
		return nil, err
	}
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil {
		return nil, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(env.Data)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("本地凭据校验参数无效")
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte("hive-bridge-v1"))
	if err != nil {
		return nil, errors.New("无法解密本地凭据，请重新登录")
	}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *credentialStore) saveAll(items map[string]Credentials) error {
	if s == nil {
		return errors.New("未配置凭据存储目录")
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	key, err := s.deriveKey(true)
	if err != nil {
		return err
	}
	plain, err := json.Marshal(items)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	sealed := gcm.Seal(nil, nonce, plain, []byte("hive-bridge-v1"))
	payload, err := json.Marshal(encryptedPayload{
		Version: 1,
		Nonce:   base64.StdEncoding.EncodeToString(nonce),
		Data:    base64.StdEncoding.EncodeToString(sealed),
	})
	if err != nil {
		return err
	}
	return writePrivateFile(s.filePath(), append(payload, '\n'))
}

func (s *credentialStore) get(rawURL string) (Credentials, bool) {
	items, err := s.loadAll()
	if err != nil {
		return Credentials{}, false
	}
	c, ok := items[normalizeOrigin(rawURL)]
	if !ok || strings.TrimSpace(c.Name) == "" || c.Password == "" {
		return Credentials{}, false
	}
	return c, true
}

func (s *credentialStore) set(rawURL string, cred Credentials) error {
	items, err := s.loadAll()
	if err != nil {
		// 密钥丢失或密文损坏时允许用户通过重新登录覆盖修复。
		items = map[string]Credentials{}
	}
	items[normalizeOrigin(rawURL)] = Credentials{
		Name:     strings.TrimSpace(cred.Name),
		Password: cred.Password,
	}
	return s.saveAll(items)
}

func (s *credentialStore) delete(rawURL string) error {
	items, err := s.loadAll()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		_ = os.Remove(s.filePath())
		return nil
	}
	origin := normalizeOrigin(rawURL)
	if _, ok := items[origin]; !ok {
		return nil
	}
	delete(items, origin)
	return s.saveAll(items)
}

func writePrivateFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".hb-secret-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
