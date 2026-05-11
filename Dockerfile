# syntax=docker/dockerfile:1.7
#
# x — X (Twitter) API CLI & Remote MCP
#
# Multi-stage Docker build:
#   1. builder: golang:1.26.1-alpine で完全静的バイナリをコンパイル
#   2. final: gcr.io/distroless/static-debian12:nonroot で UID 65532 実行
#
# ldflags 注入用 build args:
#   --build-arg VERSION=v0.1.0
#   --build-arg COMMIT=$(git rev-parse --short HEAD)
#   --build-arg DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)

# --- builder stage ---
FROM golang:1.26.1-alpine AS builder

ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

WORKDIR /src

# 依存だけ先に取得してキャッシュ効率化
# (COPY . . より前に置くことで go.mod/go.sum 変更がない限りキャッシュヒット)
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO=0 で完全静的バイナリを生成し distroless static にデプロイ可能にする
ENV CGO_ENABLED=0 GOOS=linux

# -trimpath で絶対パスをバイナリから除去 (再現性 + プライバシー)
# -ldflags "-s -w" でデバッグシンボル削除 (バイナリサイズ削減)
RUN go build \
    -trimpath \
    -ldflags "-s -w \
        -X github.com/youyo/x/internal/version.Version=${VERSION} \
        -X github.com/youyo/x/internal/version.Commit=${COMMIT} \
        -X github.com/youyo/x/internal/version.Date=${DATE}" \
    -o /out/x ./cmd/x

# --- final stage ---
# nonroot タグは UID 65532 で実行され CVE 攻撃面を最小化する
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/x /x

USER nonroot:nonroot
ENTRYPOINT ["/x"]
