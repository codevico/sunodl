# sunodl

> [!NOTE]
> This repository was created purely as a proof of concept. It is not intended for real-world use and should not be used for malicious, illegal, or unethical purposes. We do not condone or encourage misuse of this code in any form.

Download and decrypt audio from suno.

[Suno admits in court filing it used yt-dlp to scrape YouTube audio for AI training](https://propelmusic.co/articles/suno-admits-youtube-scraping-court-filing-2026) and yet encrypts their audio files to make them harder to download.

## Why not yt-dlp

yt-dlp willingly [doesn't support it](https://github.com/yt-dlp/yt-dlp/issues/10368).

## Build

```bash
go build -trimpath -ldflags "-s -w" -o sunodl .
```

Cross-compile (all targets):

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o sunodl-linux-x64 .
```

Or download a binary from the Releases page (`windows/linux/darwin` × `x64/arm64`,
plus `SHA256SUMS`).

## Usage

```bash
# download from a share link (writes <clipId>.m4a)
sunodl https://suno.com/s/3poXQo6EldlOhOVw

# explicit clip id / output
sunodl 4d632bfd-4f51-4c21-967e-2305ce7a9f5b -o song.m4a

# convert a file you already downloaded (license fetched automatically)
sunodl unplayable.m4a --clip 4d632bfd-4f51-4c21-967e-2305ce7a9f5b

# offline, with a saved license response
sunodl unplayable.m4a --clip <uuid> --license license.json

# also transcode Opus -> AAC (requires ffmpeg)
sunodl <url> --aac
```

| Option | Meaning |
| --- | --- |
| `-o, --out <path>` | output file |
| `--clip <uuid>` | clip id / AES-GCM additional data |
| `--key`, `--iv`, `--glt` | wrapped key, IV, guest token |
| `--license <json>` | JSON file with `{key, iv, glt}` |
| `--aac` | transcode to 192 kbps AAC with `ffmpeg` |
| `--keep-encrypted` | keep the downloaded ciphertext |
| `-V, --version` | print version |

## How it works

1. `POST /api/mango/rights` with the clip id returns `{key, iv, glt}`.
2. The AES-GCM key is `SHA-256(glt)`; `key` and `iv` are unwrapped with the clip
   UUID as additional authenticated data.
3. The payload is decrypted with AES-CTR using the unwrapped 16-byte counter.
4. The result is a standard MP4 containing Opus at 48 kHz.

## Development

```bash
go test ./...            # unit tests
SUNO_LIVE=1 go test ./...  # also fetch and decrypt a real clip
go vet ./...
```
