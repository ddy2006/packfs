FROM golang:1.25.2

LABEL maintainer="ddy2006"

RUN \
    apt update \
    && apt-get install -y git \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

WORKDIR /go/src
COPY go.* /go/src/
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY examples/astro/scalebox/router/*.go /go/src/

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    # glibc included.
    CGO_ENABLED=0 \
    go build -o router

FROM debian:13-slim

COPY --from=0 /go/src/router /app/bin/

COPY --from=hub.cstcloud.cn/scalebox/agent /usr/local /usr/local
WORKDIR /work
ENTRYPOINT ["goagent"]
