FROM --platform=$BUILDPLATFORM docker.m.daocloud.io/library/golang:1.26 AS builder

COPY go.mod go.sum ./

RUN GOPROXY=https://goproxy.cn go mod download

COPY . /src

WORKDIR /src

# No build-time secrets: the agent reads its configuration from disk at runtime
# and authenticates to NATS with the mTLS client certificate referenced by
# leafnode.tls, so nothing sensitive is baked into the binary.
RUN make build

FROM docker.m.daocloud.io/library/alpine:3.21

# apk add chromium
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
    && apk add --no-cache ca-certificates tzdata \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone

COPY --from=builder /src/bin /app
COPY --from=builder /src/configs/config.prod.yaml /data/conf/config.yaml
COPY --from=builder /src/configs/tls /data/tls

WORKDIR /app

ENV TZ=Asia/Shanghai

# /data/conf holds agent.yaml; /data/tls holds the client certificate, its key
# and the root CA. Mount both from a secret store — never bake them into an
# image layer.
VOLUME ["/data/conf", "/data/tls"]

CMD ["./probecube-agent", "-conf", "/data/conf/config.yaml"]
