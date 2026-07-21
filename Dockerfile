FROM docker.m.daocloud.io/library/golang:1.26 AS builder

COPY . /src
WORKDIR /src

RUN GOPROXY=https://goproxy.cn make build

FROM docker.m.daocloud.io/library/alpine:3.21

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
    && apk add --no-cache curl timezone \

COPY --from=builder /src/bin /app

WORKDIR /app

ENV TZ=Asia/Shanghai

EXPOSE 8000
EXPOSE 9000
VOLUME /data/conf

CMD ["./probecube-agent", "-conf", "/data/conf"]
