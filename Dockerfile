FROM golang:1.26-alpine AS builder

WORKDIR /app

ENV GONOSUMDB=github.com/jtclarkjr/router-go
ENV GONOSUMCHECK=github.com/jtclarkjr/router-go
ENV GOPROXY=direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o webrtc-server .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/webrtc-server /usr/local/bin/webrtc-server

EXPOSE 8080

ENTRYPOINT ["webrtc-server"]
