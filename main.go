package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var version = "dev"

const (
	apiBase   = "https://studio-api-prod.suno.com"
	webBase   = "https://suno.com"
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Safari/537.36"
)

var (
	httpClient = &http.Client{Timeout: 2 * time.Minute}
	uuidRe     = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	codeRe     = regexp.MustCompile(`^[A-Za-z0-9_-]{6,20}$`)
	shareRe    = regexp.MustCompile(`/s/([A-Za-z0-9_-]+)`)
	mediaRe    = regexp.MustCompile(`https://[^"'\\\s]+?\.m4a(?:\?[^"'\\\s]*)?`)
)

type license struct {
	Key string `json:"key"`
	IV  string `json:"iv"`
	GLT string `json:"glt"`
}

type options struct {
	input       string
	out         string
	key         string
	iv          string
	glt         string
	clip        string
	licenseFile string
	aac         bool
	keep        bool
	help        bool
	showVersion bool
}

func main() {
	o, err := parseArgs(os.Args[1:])
	if err != nil {
		fail(err)
	}
	if o.showVersion {
		fmt.Println(version)
		return
	}
	if o.help || o.input == "" {
		usage()
		if !o.help {
			os.Exit(1)
		}
		return
	}
	if err := run(o); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func usage() {
	fmt.Printf(`sunodl %s

usage: sunodl <share-url | clip-id | encrypted.m4a> [options]

  -o, --out <path>     output file
      --key <b64>      wrapped key   (local-file mode)
      --iv <b64>       wrapped iv    (local-file mode)
      --glt <token>    guest token   (local-file mode)
      --clip <uuid>    clip id / AES-GCM additional data
      --license <json> file with {key, iv, glt}
      --aac            also transcode to AAC with ffmpeg
      --keep-encrypted keep ciphertext
  -V, --version        print version
  -h, --help           print this help
`, version)
}

func parseArgs(args []string) (options, error) {
	var o options
	for i := 0; i < len(args); i++ {
		arg := args[i]
		value := func() (string, error) {
			i++
			if i >= len(args) {
				return "", fmt.Errorf("%s needs a value", arg)
			}
			return args[i], nil
		}
		switch arg {
		case "-h", "--help":
			o.help = true
		case "-V", "--version":
			o.showVersion = true
		case "-o", "--out":
			v, err := value()
			if err != nil {
				return o, err
			}
			o.out = v
		case "--key":
			v, err := value()
			if err != nil {
				return o, err
			}
			o.key = v
		case "--iv":
			v, err := value()
			if err != nil {
				return o, err
			}
			o.iv = v
		case "--glt":
			v, err := value()
			if err != nil {
				return o, err
			}
			o.glt = v
		case "--clip":
			v, err := value()
			if err != nil {
				return o, err
			}
			o.clip = v
		case "--license":
			v, err := value()
			if err != nil {
				return o, err
			}
			o.licenseFile = v
		case "--aac":
			o.aac = true
		case "--keep-encrypted":
			o.keep = true
		default:
			if strings.HasPrefix(arg, "-") {
				return o, fmt.Errorf("unknown option %s", arg)
			}
			if o.input != "" {
				return o, errors.New("only one input is allowed")
			}
			o.input = arg
		}
	}
	return o, nil
}

func run(o options) error {
	var plain []byte
	outPath := o.out

	if st, err := os.Stat(o.input); err == nil && !st.IsDir() {
		raw, err := os.ReadFile(o.input)
		if err != nil {
			return err
		}
		if outPath == "" {
			outPath = strings.TrimSuffix(o.input, filepath.Ext(o.input)) + ".decrypted.m4a"
		}
		if looksLikeMP4(raw) {
			fmt.Println("input already looks like plain MP4; nothing to decrypt")
			plain = raw
		} else {
			if o.clip == "" {
				return errors.New("local-file mode needs --clip <uuid>, or --license <json>, or --key/--iv/--glt")
			}
			lic, err := loadLicense(o, o.clip)
			if err != nil {
				return err
			}
			plain, err = decryptAudio(raw, lic, o.clip)
			if err != nil {
				return err
			}
		}
	} else {
		clipID, mediaURL, err := resolveClip(o.input)
		if err != nil {
			return err
		}
		fmt.Printf("clip:  %s\nmedia: %s\n", clipID, mediaURL)

		lic, err := fetchLicense(clipID)
		if err != nil {
			return err
		}
		ciphertext, err := get(mediaURL)
		if err != nil {
			return fmt.Errorf("media download: %w", err)
		}
		fmt.Printf("downloaded %d bytes (encrypted)\n", len(ciphertext))

		if o.keep {
			encPath := clipID + ".encrypted.m4a"
			if err := os.WriteFile(encPath, ciphertext, 0o644); err != nil {
				return err
			}
			fmt.Printf("kept ciphertext at %s\n", encPath)
		}
		plain, err = decryptAudio(ciphertext, lic, clipID)
		if err != nil {
			return err
		}
		if outPath == "" {
			outPath = clipID + ".m4a"
		}
	}

	if !looksLikeMP4(plain) {
		fmt.Fprintln(os.Stderr, "warning: decrypted data does not start with an MP4 ftyp box; key/iv/clip mismatch?")
	}
	if err := os.WriteFile(outPath, plain, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d bytes)\n", outPath, len(plain))

	if o.aac {
		aacPath := strings.TrimSuffix(outPath, ".m4a") + ".aac.m4a"
		maybeAAC(outPath, aacPath)
	}
	return nil
}

func loadLicense(o options, clipID string) (license, error) {
	if o.licenseFile != "" {
		raw, err := os.ReadFile(o.licenseFile)
		if err != nil {
			return license{}, err
		}
		var lic license
		if err := json.Unmarshal(raw, &lic); err != nil {
			return lic, err
		}
		return lic, nil
	}
	if o.key != "" && o.iv != "" && o.glt != "" {
		return license{Key: o.key, IV: o.iv, GLT: o.glt}, nil
	}
	fmt.Printf("fetching license for %s\n", clipID)
	return fetchLicense(clipID)
}

func maybeAAC(in, out string) {
	cmd := exec.Command("ffmpeg", "-y", "-v", "error", "-i", in, "-c:a", "aac", "-b:a", "192k", out)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: ffmpeg failed (%v); keeping Opus MP4\n", err)
		return
	}
	fmt.Printf("wrote %s\n", out)
}

func looksLikeMP4(b []byte) bool {
	return len(b) >= 12 && string(b[4:8]) == "ftyp"
}

func decryptAudio(ciphertext []byte, lic license, clipID string) ([]byte, error) {
	if lic.Key == "" || lic.IV == "" || lic.GLT == "" {
		return nil, errors.New("license response missing key/iv/glt")
	}

	sum := sha256.Sum256([]byte(lic.GLT))
	gcmBlock, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(gcmBlock)
	if err != nil {
		return nil, err
	}
	aad := []byte(clipID)
	unwrap := func(s string) ([]byte, error) {
		raw, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, err
		}
		if len(raw) < gcm.NonceSize()+gcm.Overhead() {
			return nil, errors.New("wrapped key too short")
		}
		nonce, sealed := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
		return gcm.Open(nil, nonce, sealed, aad)
	}

	key, err := unwrap(lic.Key)
	if err != nil {
		return nil, err
	}
	iv, err := unwrap(lic.IV)
	if err != nil {
		return nil, err
	}
	if len(iv) < aes.BlockSize {
		return nil, errors.New("invalid IV")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	counter := make([]byte, aes.BlockSize)
	copy(counter, iv)
	plain := make([]byte, len(ciphertext))
	cipher.NewCTR(block, counter).XORKeyStream(plain, ciphertext)
	return plain, nil
}

func parseTarget(input string) (code, clipID string) {
	if m := shareRe.FindStringSubmatch(input); m != nil {
		return m[1], ""
	}
	if !strings.ContainsAny(input, "/.") && codeRe.MatchString(input) {
		return input, ""
	}
	if m := uuidRe.FindString(input); m != "" {
		return "", m
	}
	return "", ""
}

func resolveClip(input string) (clipID, mediaURL string, err error) {
	code, clipID := parseTarget(input)
	pageURL := ""

	if code != "" {
		var share struct {
			ContentID string `json:"content_id"`
		}
		if err := getJSON(apiBase+"/api/share/code/"+code, &share); err != nil {
			return "", "", fmt.Errorf("share code %s: %w", code, err)
		}
		clipID = share.ContentID
		pageURL = webBase + "/s/" + code
	} else if clipID != "" {
		pageURL = webBase + "/song/" + clipID
	} else {
		return "", "", fmt.Errorf("cannot parse a clip id from %q", input)
	}

	if pageURL != "" {
		body, err := get(pageURL)
		if err != nil {
			return "", "", err
		}
		for _, candidate := range mediaRe.FindAllString(string(body), -1) {
			candidate = strings.ReplaceAll(candidate, `\u0026`, "&")
			candidate = strings.ReplaceAll(candidate, "&amp;", "&")
			if mediaURL == "" {
				mediaURL = candidate
			}
			if strings.Contains(candidate, clipID) {
				mediaURL = candidate
				break
			}
		}
	}
	if mediaURL == "" {
		mediaURL = "https://d2lwuy8qc234o3.cloudfront.net/1/clip/" + clipID + ".m4a"
	}
	return clipID, mediaURL, nil
}

func fetchLicense(clipID string) (license, error) {
	var lic license
	payload, err := json.Marshal(map[string]any{
		"content_params": map[string]string{"content_id": clipID, "content_type": "clip"},
	})
	if err != nil {
		return lic, err
	}
	req, err := http.NewRequest(http.MethodPost, apiBase+"/api/mango/rights", bytes.NewReader(payload))
	if err != nil {
		return lic, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	res, err := httpClient.Do(req)
	if err != nil {
		return lic, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return lic, fmt.Errorf("license request: HTTP %d", res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(&lic); err != nil {
		return lic, err
	}
	if lic.Key == "" || lic.IV == "" || lic.GLT == "" {
		return lic, errors.New("license response missing key/iv/glt")
	}
	return lic, nil
}

func getJSON(url string, v any) error {
	body, err := get(url)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

func get(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return io.ReadAll(res.Body)
}
