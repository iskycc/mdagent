# 构建阶段
FROM golang:1.26.3-alpine AS builder

WORKDIR /app

# 安装 git（go mod 可能需要）
RUN apk add --no-cache git

# 配置 Go 模块代理，避免默认代理网络超时
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}

# 先复制依赖文件，利用缓存
COPY go.mod go.sum ./
RUN go mod download

# 复制源码
COPY . .

# 编译二进制
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o miaodi-agent .

# 运行阶段
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 1000 appuser

ENV TZ=Asia/Shanghai

WORKDIR /app

COPY --from=builder /app/miaodi-agent /app/miaodi-agent

USER appuser

EXPOSE 8080

ENTRYPOINT ["/app/miaodi-agent"]
