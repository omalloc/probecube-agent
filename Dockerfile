FROM --platform=$BUILDPLATFORM golang:1.26 AS builder

WORKDIR /src

COPY go.mod go.sum ./

RUN go mod download

COPY . .

ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH

# No build-time secrets: the agent reads its configuration from disk at runtime
# and authenticates to NATS with the mTLS client certificate referenced by
# leafnode.tls, so nothing sensitive is baked into the binary.
RUN GOOS="$TARGETOS" GOARCH="$TARGETARCH" make build VERSION="$VERSION"

FROM alpine:3.21

# apk add chromium
RUN apk add --no-cache ca-certificates tzdata \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone

COPY --from=builder /src/bin /app
COPY configs/config.prod.yaml /data/conf/config.yaml
COPY --from=node-ca node-ca.pem /etc/probecube/tls/node-ca.pem

WORKDIR /app

ENV TZ=Asia/Shanghai

# Persist the node identity in writable storage for enrollment and renewal.
VOLUME ["/etc/probecube/tls"]

CMD ["./probecube-agent", "-conf", "/data/conf/config.yaml"]
