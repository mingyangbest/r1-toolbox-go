# 多阶段构建：任何机器上 `docker build` 即可产出 r1-toolbox 二进制，无需本机安装 Go。
#
#   docker build --target binary .            # 只编译
#   docker create -v /out --name tmp r1-toolbox:build true
#   ... 或直接用 docker compose run --rm build（产物落到 ./build/）
#
# ⚠️ 本程序运行依赖海康 R1/R1X 的屏幕硬件（/dev/fb0、背光 sysfs、风扇 hwmon），
#    普通 x86 服务器/电脑上只能编译，不能真正点亮屏幕。

# ---------- 阶段 1：编译 ----------
FROM golang:1.21-bookworm AS builder

WORKDIR /src
ENV GOFLAGS=-mod=mod \
    CGO_ENABLED=1

# 先拷 go.mod 以利用 Docker 层缓存加速依赖下载
COPY go.mod ./
RUN go mod download || true

COPY . .
RUN go build -trimpath -ldflags "-s -w" -o /out/r1-toolbox ./cmd

# ---------- 阶段 2：binary 导出（docker compose run --rm build 使用）----------
FROM debian:bookworm-slim AS binary

COPY --from=builder /out/r1-toolbox /out/r1-toolbox
CMD ["sh", "-c", "echo '二进制在镜像 /out/r1-toolbox，请用 docker compose run --rm build 导出到 ./build/'"]

# ---------- 阶段 3：运行时镜像（实验性）----------
# 屏幕渲染需要特权设备访问，仅在 R1/R1X 宿主机上尝试：
#   docker compose up -d screen
FROM debian:bookworm-slim AS runtime

RUN apt-get update && apt-get install -y --no-install-recommends \
        libsdl2-2.0-0 \
        smartmontools \
        lm-sensors \
        i2c-tools \
        ca-certificates \
        fonts-wqy-zenhei \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/r1-toolbox /usr/local/bin/r1-toolbox
COPY config.example.yaml /etc/r1-toolbox/config.yaml

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/r1-toolbox"]
