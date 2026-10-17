package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"os"
	"testing"
)

func makeMP4(n int) []byte {
	b := make([]byte, n)
	binary.BigEndian.PutUint32(b[0:4], 28)
	copy(b[4:8], "ftyp")
	binary.BigEndian.PutUint32(b[8:12], 0x200)
	copy(b[12:20], "isomiso2")
	for i := 20; i < n; i++ {
		b[i] = byte(i)
	}
	return b
}

func makeLicense(t *testing.T, clipID, glt string, ctrKey, ctrIV []byte) license {
	t.Helper()
	sum := sha256.Sum256([]byte(glt))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	wrap := func(raw []byte) string {
		nonce := make([]byte, gcm.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			t.Fatal(err)
		}
		sealed := gcm.Seal(nil, nonce, raw, []byte(clipID))
		return base64.StdEncoding.EncodeToString(append(nonce, sealed...))
	}
	return license{Key: wrap(ctrKey), IV: wrap(ctrIV), GLT: glt}
}

func encryptCTR(t *testing.T, plain, key, iv []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(plain))
	cipher.NewCTR(block, iv).XORKeyStream(out, plain)
	return out
}

func TestDecryptAudio(t *testing.T) {
	const clipID = "4d632bfd-4f51-4c21-967e-2305ce7a9f5b"
	const glt = "guest-token"
	key := make([]byte, 16)
	iv := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(iv); err != nil {
		t.Fatal(err)
	}

	lic := makeLicense(t, clipID, glt, key, iv)
	plain := makeMP4(64 * 1024)
	ciphertext := encryptCTR(t, plain, key, iv)

	got, err := decryptAudio(ciphertext, lic, clipID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatal("decrypted bytes do not match the original")
	}
	if !looksLikeMP4(got) {
		t.Fatal("decrypted data is not MP4")
	}
}

func TestWrongClipID(t *testing.T) {
	key := make([]byte, 16)
	iv := make([]byte, 16)
	lic := makeLicense(t, "4d632bfd-4f51-4c21-967e-2305ce7a9f5b", "guest-token", key, iv)
	if _, err := decryptAudio(make([]byte, 32), lic, "00000000-0000-0000-0000-000000000000"); err == nil {
		t.Fatal("expected AES-GCM authentication failure")
	}
}

func TestParseTarget(t *testing.T) {
	const uuid = "4d632bfd-4f51-4c21-967e-2305ce7a9f5b"
	tests := []struct {
		in   string
		code string
		clip string
	}{
		{"https://suno.com/s/3poXQo6EldlOhOVw", "3poXQo6EldlOhOVw", ""},
		{"3poXQo6EldlOhOVw", "3poXQo6EldlOhOVw", ""},
		{"https://suno.com/song/" + uuid + "?sh=3poXQo6EldlOhOVw", "", uuid},
		{uuid, "", uuid},
		{"hello world", "", ""},
	}
	for _, tt := range tests {
		code, clip := parseTarget(tt.in)
		if code != tt.code || clip != tt.clip {
			t.Errorf("parseTarget(%q) = (%q, %q), want (%q, %q)", tt.in, code, clip, tt.code, tt.clip)
		}
	}
}

func TestLiveDecrypt(t *testing.T) {
	if os.Getenv("SUNO_LIVE") != "1" {
		t.Skip("set SUNO_LIVE=1 to hit the real API")
	}
	clipID, mediaURL, err := resolveClip("https://suno.com/s/3poXQo6EldlOhOVw")
	if err != nil {
		t.Fatal(err)
	}
	lic, err := fetchLicense(clipID)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := get(mediaURL)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := decryptAudio(ciphertext, lic, clipID)
	if err != nil {
		t.Fatal(err)
	}
	if !looksLikeMP4(plain) || len(plain) != len(ciphertext) {
		t.Fatal("unexpected decrypted payload")
	}
}
