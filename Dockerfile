# syntax=docker/dockerfile:1.7
#
# GoReleaser 用 Dockerfile。
# GoReleaser の dockers セクションは事前にビルドされたバイナリ (x) のみを
# build context に渡すため、ここでは Go ビルドを行わずバイナリをそのまま COPY する。
#
# ローカルで `docker build` する場合は `goreleaser release --snapshot --clean` で
# artifact を生成してから実行するか、別途 multi-stage Dockerfile を用意すること。

FROM gcr.io/distroless/static-debian12:nonroot

COPY x /usr/local/bin/x

USER 65532:65532
WORKDIR /home/nonroot

ENTRYPOINT ["/usr/local/bin/x"]
