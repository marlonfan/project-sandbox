# 构建阶段：纯 Go 交叉编译，无 CGO
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sandbox-server .

# 运行阶段：alpine + CA 证书（SMTP TLS）+ 时区数据
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/sandbox-server ./sandbox-server
COPY project-sandbox.html ./
ENV PSB_ADDR=:8787 \
    PSB_DB=/data/sandbox.db \
    PSB_HTML=/app/project-sandbox.html \
    TZ=Asia/Shanghai
# 数据库与配置持久化目录：挂载卷到 /data，.env 放 /data/.env
VOLUME ["/data"]
EXPOSE 8787
ENTRYPOINT ["./sandbox-server"]
