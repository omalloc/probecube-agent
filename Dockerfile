FROM --platform=$BUILDPLATFORM docker.m.daocloud.io/library/golang:1.26 AS builder

COPY go.mod go.sum ./
RUN GOPROXY=https://goproxy.cn go mod download

COPY . /src

WORKDIR /src

RUN make build

FROM docker.m.daocloud.io/library/alpine:3.21

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
    && apk add --no-cache ca-certificates tzdata \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone

COPY --from=builder /src/bin /app

WORKDIR /app

ENV TZ=Asia/Shanghai

VOLUME /data/conf

CMD ["./probecube-agent", "-embed"]
