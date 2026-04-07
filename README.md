# webrtc-server-go

A WebRTC signaling and media server in Go using [Pion WebRTC](https://github.com/pion/webrtc) and [router-go](https://github.com/jtclarkjr/router-go).

## Features

- **Data Channel Echo** — send text messages over a WebRTC data channel, receive them echoed back
- **Audio/Video Mirror** — stream audio and video tracks to the server, receive them looped back in real time
- **Single HTTP Signaling** — no WebSocket or trickle ICE required; full SDP offer/answer exchange in one POST request

## Requirements

- Go 1.26+ (for running natively)
- Docker & Docker Compose (for containerized usage)

## Getting Started

```bash
git clone https://github.com/jtclarkjr/webrtc-server-go.git
cd webrtc-server-go
go mod tidy
go run .
```

The server starts on `http://localhost:8080`.

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

## Testing Locally

Start the server in one terminal, then run the interactive test client in another:

```bash
# Terminal 1
go run .

# Terminal 2
go run ./cmd/client
```

The client connects via `/offer`, opens a data channel, and lets you type messages interactively.

## Linting

```bash
golangci-lint run ./...
```

## License

MIT
