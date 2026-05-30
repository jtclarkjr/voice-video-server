# WebRTC Backend

A WebRTC signaling, room, media, and live translation backend in Go using
[Pion WebRTC](https://github.com/pion/webrtc), [router-go](https://github.com/jtclarkjr/router-go),
Supabase auth, and OpenAI Realtime translation.

## Features

- **Data Channel Echo** — send text messages over a WebRTC data channel, receive them echoed back
- **Audio/Video Mirror** — stream audio and video tracks to the server, receive them looped back in real time
- **Room Signaling** — create/list rooms, receive room events, and connect peers through WebSocket signaling
- **Single HTTP Signaling** — full SDP offer/answer exchange for `/offer` and `/media` in one POST request
- **Backend-Managed Live Translation Sessions** — authenticated users create persisted translation
  sessions, send SDP offers to the backend, and receive SDP answers without exposing OpenAI tokens

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
| `DATABASE_URL` | Postgres connection string. Optional for room storage, required for backend-managed translation sessions. |
| `ALLOWED_CORS` | Comma-separated frontend origins allowed by CORS. Include deployed origins such as `https://video-voice-app-v2.vercel.app`. |
| `SUPABASE_URL` | Supabase project URL used to validate authenticated requests. |
| `SUPABASE_URL_DOCKER` | Docker-only Supabase URL override, usually `http://host.docker.internal:54321` for local Supabase. |
| `SUPABASE_SECRET_KEY` | Supabase service role or secret key used by backend auth validation. |
| `OPENAI_API_KEY` | Server-side OpenAI API key used to create Realtime translation calls. |

The live translation endpoints are protected when Supabase auth is configured. They return `401`
for missing/anonymous users, `503` when `OPENAI_API_KEY` or the translation session store is not
available, and sanitized `502` responses for OpenAI upstream failures.

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

### POST /translation/sessions

Create a backend-owned translation session for an authenticated, non-anonymous Supabase user.

Query parameters:

```text
lang=ja
```

Supported target languages are `en`, `es`, `pt`, `fr`, `ja`, `ru`, `zh`, `de`, `ko`, `hi`, `id`,
`vi`, and `it`.

Response body:

```json
{
  "id": "session-id",
  "expiresAt": "2026-05-30T12:10:00Z"
}
```

### POST /translation/sessions/{id}/offer

Exchange a browser SDP offer for an OpenAI SDP answer. The request body must be raw SDP with
`Content-Type: application/sdp`; the response body is raw SDP with `Content-Type: application/sdp`.

### DELETE /translation/sessions/{id}

Mark the backend-owned translation session ended. This is local cleanup only; the browser still ends
the live media session by closing its `RTCPeerConnection`.

The backend calls OpenAI with model `gpt-realtime-translate`, sets `audio.output.language` to the
requested `lang`, sends `OpenAI-Safety-Identifier` as a SHA-256 hash of the Supabase user ID, and
keeps the OpenAI API key and OpenAI tokens off the frontend.

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
