# software-distribution-platform-hub 运行时镜像（old images/Dockerfile 风格）。
# 由 build/hub/build.sh 在 output/hub/images/ 内使用，构建上下文只含
# hub.tar.gz 与本文件（ubuntu 基座 + 二进制，与 old 保持一致）。
FROM ubuntu:22.04
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
ADD software-distribution-platform-hub.tar.gz /app/
WORKDIR /app/
EXPOSE 8080
ENTRYPOINT ["/app/software-distribution-platform-hub"]
