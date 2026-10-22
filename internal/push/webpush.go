package push

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// recordSize is the aes128gcm record size; a push message is one record.
const recordSize = 4096

// MaxPayload leaves room in the single record for the delimiter and tag.
const MaxPayload = recordSize - 1 - 16

const maxEndpoint = 2048

// decodeClientKeys checks a browser's p256dh (an uncompressed P-256 point) and
// 16-byte auth secret.
func decodeClientKeys(p256dh, auth string) (*ecdh.PublicKey, []byte, error) {
	rawKey, err := b64.DecodeString(strings.TrimRight(p256dh, "="))
	if err != nil || len(rawKey) != 65 {
		return nil, nil, errors.New("keys.p256dh 必须是 base64url 编码的 65 字节 P-256 公钥")
	}
	key, err := ecdh.P256().NewPublicKey(rawKey)
	if err != nil {
		return nil, nil, errors.New("keys.p256dh 不是 P-256 曲线上的点")
	}
	secret, err := b64.DecodeString(strings.TrimRight(auth, "="))
	if err != nil || len(secret) != 16 {
		return nil, nil, errors.New("keys.auth 必须是 base64url 编码的 16 字节")
	}
	return key, secret, nil
}

// checkEndpoint accepts an https push service URL without credentials.
func checkEndpoint(endpoint string) error {
	if len(endpoint) > maxEndpoint {
		return errors.New("endpoint 过长")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return errors.New("endpoint 必须是不含凭据的 https:// URL")
	}
	return nil
}

// encrypt produces an RFC 8291 aes128gcm body. salt and ephemeral are
// injected so the RFC test vectors are reproducible; send passes fresh ones.
func encrypt(payload []byte, uaPublic *ecdh.PublicKey, authSecret, salt []byte, ephemeral *ecdh.PrivateKey) ([]byte, error) {
	if len(payload) > MaxPayload {
		return nil, fmt.Errorf("payload %d bytes exceeds %d", len(payload), MaxPayload)
	}
	if len(salt) != 16 {
		return nil, errors.New("salt must be 16 bytes")
	}
	shared, err := ephemeral.ECDH(uaPublic)
	if err != nil {
		return nil, err
	}
	asPublic := ephemeral.PublicKey().Bytes()
	keyInfo := "WebPush: info\x00" + string(uaPublic.Bytes()) + string(asPublic)
	ikm, err := hkdf.Key(sha256.New, shared, authSecret, keyInfo, 32)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 0, 16+4+1+len(asPublic))
	header = append(header, salt...)
	header = binary.BigEndian.AppendUint32(header, recordSize)
	header = append(header, byte(len(asPublic)))
	header = append(header, asPublic...)
	plain := append(append(make([]byte, 0, len(payload)+1), payload...), 0x02)
	return gcm.Seal(header, nonce, plain, nil), nil
}

// encryptFresh encrypts with a random salt and ephemeral key.
func encryptFresh(payload []byte, sub Subscription) ([]byte, error) {
	uaPublic, authSecret, err := decodeClientKeys(sub.P256dh, sub.Auth)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return encrypt(payload, uaPublic, authSecret, salt, ephemeral)
}

// vapidHeader returns the RFC 8292 Authorization value for endpoint.
func vapidHeader(endpoint, subject string, key *ecdsa.PrivateKey, publicKey string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(map[string]any{
		"aud": u.Scheme + "://" + u.Host,
		"exp": now.Add(12 * time.Hour).Unix(),
		"sub": subject,
	})
	if err != nil {
		return "", err
	}
	unsigned := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`)) + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", err
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return "vapid t=" + unsigned + "." + b64.EncodeToString(signature) + ", k=" + publicKey, nil
}
