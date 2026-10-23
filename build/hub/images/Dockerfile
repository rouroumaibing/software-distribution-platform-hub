# software-distribution-platform-hub 运行时镜像（old images/Dockerfile 风格）。
# 由 build/hub/build.sh 在 output/hub/images/ 内使用，构建上下文只含
# hub.tar.gz 与本文件（ubuntu 基座 + 二进制，与 old 保持一致）。
FROM ubuntu:22.04
# apt 源可替换后端：默认华为云镜像（境外官方源在云上节点不可达/极慢）；
# APT_MIRROR 取不含 scheme 的地址（默认 mirrors.huaweicloud.com/ubuntu）；
# 需要官方源时 --build-arg APT_MIRROR=archive.ubuntu.com/ubuntu 覆盖。
ARG APT_MIRROR=mirrors.huaweicloud.com/ubuntu
# 匹配串含 scheme 与路径、替换值保留 http://，避免生成无协议头或路径重复的行
RUN sed -i -e "s|http://archive.ubuntu.com/ubuntu|http://${APT_MIRROR}|g" -e "s|http://security.ubuntu.com/ubuntu|http://${APT_MIRROR}|g" /etc/apt/sources.list \
    && apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
ADD software-distribution-platform-hub.tar.gz /app/
WORKDIR /app/
EXPOSE 8080
ENTRYPOINT ["/app/software-distribution-platform-hub"]
