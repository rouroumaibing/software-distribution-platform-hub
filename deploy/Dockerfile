# Hub image — builder + ubuntu runtime (old build.sh 风格的容器化版).
# 构建上下文必须是 workspace 根（hub 的 go.mod replace 指向 ../runner 模块）：
#   docker build -f software-distribution-platform-hub/deploy/Dockerfile \
#     -t localhost:5000/hub:dev <workspace-root>
FROM golang:1.26-alpine AS build
ENV GOPROXY=https://goproxy.cn,direct
# replace 目标按 go.mod 的相对路径落到 /software-distribution-platform-runner
COPY software-distribution-platform-runner/ /software-distribution-platform-runner/
WORKDIR /src
COPY software-distribution-platform-hub/go.mod software-distribution-platform-hub/go.sum ./
RUN go mod download
COPY software-distribution-platform-hub/ ./
RUN CGO_ENABLED=0 go build -o /out/hub ./cmd/hub

FROM ubuntu:22.04
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/hub /usr/local/bin/hub
EXPOSE 8080
ENTRYPOINT ["hub"]
