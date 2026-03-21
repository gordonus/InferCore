# Build
FROM golang:1.22-alpine AS build
WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/infercore ./cmd/infercore

# Runtime
FROM alpine:3.20
LABEL org.opencontainers.image.url="https://infercore.dev" \
	org.opencontainers.image.source="https://github.com/infercore/infercore"
RUN apk add --no-cache ca-certificates \
	&& adduser -D -H -u 10001 appuser

WORKDIR /app
COPY --from=build /out/infercore /app/infercore
COPY configs/ /app/configs/

USER appuser
ENV INFERCORE_CONFIG=/app/configs/infercore.example.yaml

EXPOSE 8080
ENTRYPOINT ["/app/infercore"]
