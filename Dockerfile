# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /openfusion ./cmd/openfusion/

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /openfusion /usr/local/bin/openfusion
COPY --from=builder /build/presets/ /etc/openfusion/presets/
COPY --from=builder /build/config.example.yaml /etc/openfusion/config.example.yaml

EXPOSE 8080

ENTRYPOINT ["openfusion"]
CMD ["-config", "/etc/openfusion/config.yaml"]
