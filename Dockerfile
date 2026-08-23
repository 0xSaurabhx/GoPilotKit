# Build stage
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -trimpath -o /gopilotkit ./cmd/engine/

# Final stage — scratch image for minimal size
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /gopilotkit /gopilotkit

# Create non-root user
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group

# Default config directory
VOLUME /data

EXPOSE 8080

ENTRYPOINT ["/gopilotkit"]
CMD ["run", "--config", "/data/intelligence.yaml"]
