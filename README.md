# WebRTC Backend

A WebRTC signaling, room, media, and live translation backend in Go using
[Pion WebRTC](https://github.com/pion/webrtc), [router-go](https://github.com/jtclarkjr/router-go),
Supabase auth, and OpenAI Realtime translation.

## Features

- **Data Channel Echo** — send text messages over a WebRTC data channel, receive them echoed back
- **Audio/Video Mirror** — stream audio and video tracks to the server, receive them looped back in real time
- **Room Signaling** — create/list rooms, receive room events, and connect peers through WebSocket signaling
- **Single HTTP Signaling** — full SDP offer/answer exchange for `/offer` and `/media` in one POST request
- **Live Translation Session Secrets** — authenticated users can request short-lived OpenAI Realtime
  translation client secrets for the frontend `/translate` flow

## Requirements

- Go 1.26+ (for running natively)
- Docker & Docker Compose (for containerized usage)
- Supabase project credentials for protected routes
- OpenAI API key for live translation

## Getting Started

```bash
git clone https://github.com/jtclarkjr/voice-video-server.git
cd voice-video-server
go mod tidy
go run .
```

The server starts on `http://localhost:8080`.

## Configuration

Copy `.env.example` to `.env` for local development and set the values needed by your environment.

| Variable | Purpose |
| --- | --- |
| `PORT` | HTTP listen port. Defaults to `8080`. |
| `DATABASE_URL` | Optional Postgres connection string for room storage. The server keeps running without it. |
| `ALLOWED_CORS` | Comma-separated frontend origins allowed by CORS. Include deployed origins such as `https://video-voice-app-v2.vercel.app`. |
| `SUPABASE_URL` | Supabase project URL used to validate authenticated requests. |
| `SUPABASE_URL_DOCKER` | Docker-only Supabase URL override, usually `http://host.docker.internal:54321` for local Supabase. |
| `SUPABASE_SECRET_KEY` | Supabase service role or secret key used by backend auth validation. |
| `OPENAI_API_KEY` | Server-side OpenAI API key used to create Realtime translation client secrets. |

The live translation endpoint is protected when Supabase auth is configured. It returns `401` for
missing/anonymous users, `503` when `OPENAI_API_KEY` is not set, and sanitized `502` responses for
OpenAI upstream failures.

## Docker

### Build and run with Docker Compose

```bash
docker compose up --build
```

### Build and run with Docker directly

```bash
docker build -t webrtc-server-go .
docker run -p 8080:8080 --network host webrtc-server-go
```

> **Note:** `network_mode: host` is used for WebRTC UDP media traffic. This works natively on Linux. On macOS, Docker Desktop does not support host networking — for full media testing on macOS, run natively with `go run .` instead.

## API

### GET /health

Health check endpoint. Returns `200 OK` when the server process is running.

### GET /rooms

List active rooms.

### GET /rooms/events

Server-sent room updates for lobby clients.

### GET /ws

WebSocket signaling endpoint for room-based calls.

### POST /offer

Data channel echo. Send an SDP offer, get an answer back. Any messages sent over the data channel are echoed with an `"echo: "` prefix.

```bash
curl -X POST http://localhost:8080/offer \
  -H "Content-Type: application/json" \
  -d '{"type":"offer","sdp":"..."}'
```

### POST /media

Audio/video mirror + data channel echo. Send an SDP offer containing audio/video tracks. The server mirrors all media streams back to the sender.

```bash
curl -X POST http://localhost:8080/media \
  -H "Content-Type: application/json" \
  -d '{"type":"offer","sdp":"..."}'
```

### POST /translation/client-secret

Create a short-lived OpenAI Realtime translation client secret for an authenticated, non-anonymous
Supabase user. The frontend uses this secret to open a browser WebRTC translation session directly
with OpenAI.

Request body:

```json
{
  "targetLanguage": "ja"
}
```

Supported target languages are `en`, `es`, `fr`, `de`, `it`, `pt`, `ja`, `ko`, `zh`, `ar`, and
`hi`.

The backend calls OpenAI with model `gpt-realtime-translate`, sets `audio.output.language` to the
requested target language, and sends `OpenAI-Safety-Identifier` as a SHA-256 hash of the Supabase
user ID. The OpenAI API key never leaves the backend.

## Testing Locally

Start the server in one terminal, then run the interactive test client in another:

```bash
# Terminal 1
go run .

# Terminal 2
go run ./cmd/client
```

The client connects via `/offer`, opens a data channel, and lets you type messages interactively.

Run the backend tests with:

```bash
go test ./...
```

## Linting

```bash
golangci-lint run ./...
```

## License

MIT
