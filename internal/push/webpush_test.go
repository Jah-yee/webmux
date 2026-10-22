package push

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"
)

func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	raw, err := b64.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// RFC 8291 Section 5 and Appendix A.
func TestEncryptMatchesRFC8291Vector(t *testing.T) {
	asPrivate, err := ecdh.P256().NewPrivateKey(mustB64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	uaPublic, auth, err := decodeClientKeys(
		"BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4",
		"BTBZMqHH6r4Tts7J_aSIgg")
	if err != nil {
		t.Fatal(err)
	}
	got, err := encrypt([]byte("When I grow up, I want to be a watermelon"), uaPublic, auth, mustB64(t, "DGv6ra1nlYgDCS1FRnbzlw"), asPrivate)
	if err != nil {
		t.Fatal(err)
	}
	want := "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml" +
		"mlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPT" +
		"pK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	if b64.EncodeToString(got) != want {
		t.Fatalf("encrypt =\n%s\nwant\n%s", b64.EncodeToString(got), want)
	}
}

// decryptForTest plays the browser side of RFC 8291.
func decryptForTest(t *testing.T, body []byte, ua *ecdh.PrivateKey, auth string) []byte {
	t.Helper()
	salt, idlen := body[:16], int(body[20])
	if binary.BigEndian.Uint32(body[16:20]) != recordSize {
		t.Fatalf("record size %d", binary.BigEndian.Uint32(body[16:20]))
	}
	asPublic, err := ecdh.P256().NewPublicKey(body[21 : 21+idlen])
	if err != nil {
		t.Fatal(err)
	}
	shared, err := ua.ECDH(asPublic)
	if err != nil {
		t.Fatal(err)
	}
	info := "WebPush: info\x00" + string(ua.PublicKey().Bytes()) + string(asPublic.Bytes())
	ikm, _ := hkdf.Key(sha256.New, shared, mustB64(t, auth), info, 32)
	cek, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, body[21+idlen:], nil)
	if err != nil {
		t.Fatal(err)
	}
	if plain[len(plain)-1] != 0x02 {
		t.Fatalf("missing last-record delimiter")
	}
	return plain[:len(plain)-1]
}

func TestEncryptFreshRoundTripAndLimit(t *testing.T) {
	p256dh, auth, ua := browserKeys(t)
	sub := Subscription{P256dh: p256dh, Auth: auth}
	body, err := encryptFresh([]byte(`{"x":1}`), sub)
	if err != nil {
		t.Fatal(err)
	}
	if got := decryptForTest(t, body, ua, auth); string(got) != `{"x":1}` {
		t.Fatalf("round trip %q", got)
	}
	if _, err := encryptFresh(make([]byte, MaxPayload+1), sub); err == nil {
		t.Fatal("oversized payload accepted")
	}
}

func TestVAPIDHeader(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rawPublic, _ := key.PublicKey.Bytes()
	publicKey := b64.EncodeToString(rawPublic)
	now := time.Unix(1_700_000_000, 0)
	header, err := vapidHeader("https://fcm.googleapis.com/fcm/send/abc?x=1", "mailto:me@example.com", key, publicKey, now)
	if err != nil {
		t.Fatal(err)
	}
	token, k, ok := strings.Cut(strings.TrimPrefix(header, "vapid t="), ", k=")
	if !ok || !strings.HasPrefix(header, "vapid t=") || k != publicKey {
		t.Fatalf("header %q", header)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt %q", token)
	}
	var head, claims map[string]any
	_ = json.Unmarshal(mustB64(t, parts[0]), &head)
	_ = json.Unmarshal(mustB64(t, parts[1]), &claims)
	if head["alg"] != "ES256" || head["typ"] != "JWT" {
		t.Fatalf("jwt header %v", head)
	}
	if claims["aud"] != "https://fcm.googleapis.com" || claims["sub"] != "mailto:me@example.com" || claims["exp"] != float64(now.Add(12*time.Hour).Unix()) {
		t.Fatalf("claims %v", claims)
	}
	signature := mustB64(t, parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r, s := new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])
	verifier, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), mustB64(t, k))
	if err != nil {
		t.Fatal(err)
	}
	if len(signature) != 64 || !ecdsa.Verify(verifier, digest[:], r, s) {
		t.Fatal("signature does not verify against k")
	}
}

func TestValidateSubscription(t *testing.T) {
	p256dh, auth, _ := browserKeys(t)
	offCurve := make([]byte, 65)
	offCurve[0] = 4
	for _, row := range []struct {
		name, endpoint, p256dh, auth string
		ok                           bool
	}{
		{"valid", "https://push.example/x", p256dh, auth, true},
		{"padded keys", "https://push.example/x", p256dh + "=", auth + "==", true},
		{"http endpoint", "http://push.example/x", p256dh, auth, false},
		{"credentials", "https://u:p@push.example/x", p256dh, auth, false},
		{"too long", "https://push.example/" + strings.Repeat("a", maxEndpoint), p256dh, auth, false},
		{"off curve", "https://push.example/x", b64.EncodeToString(offCurve), auth, false},
		{"short auth", "https://push.example/x", p256dh, b64.EncodeToString(make([]byte, 8)), false},
		{"garbage", "https://push.example/x", "!!", auth, false},
	} {
		if err := Validate(row.endpoint, row.p256dh, row.auth); (err == nil) != row.ok {
			t.Errorf("%s: Validate = %v", row.name, err)
		}
	}
}
