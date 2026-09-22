#!/bin/bash
# 构建 software-distribution-platform-hub（复刻 old/go-devops build/go-devops/build.sh 打包结构）：
#   宿主机交叉编译 -> images/ 二进制 tar.gz -> 运行时镜像 + docker save
#   charts/ chart 源目录 -> 打 tgz；最终 pack 成 <component>-<version>.tar.gz（charts + images 一起交付）
# 用法: ./build.sh [version]    默认 v0.0.1
#       ./build.sh clean        清理 output/
# 注意: hub 的 go.mod replace 指向 ../software-distribution-platform-runner，
#       因此必须在 workspace 根保持两个模块同级（脚本已按此假设取相对路径）。
set -e

PROJECT_ROOT=$(cd "$(dirname "$0")"/../..; pwd)
OUTPUTDIR="${PROJECT_ROOT}/output"
component=software-distribution-platform-hub
version=${1:-"v0.0.1"}

buildDate=$(TZ=Asia/Shanghai date +%FT%T%z)

function clean(){
    rm -rf "${OUTPUTDIR}"
    echo "${component} output cleaned."
}

if [ "${1:-}" = "clean" ]; then
    clean
    exit 0
fi

function prepare_go_mod(){
    export GO111MODULE=on
    export GOPROXY=https://goproxy.cn,direct
    export GONOSUMDB='*'
    export GOSUMDB=off
    export CGO_ENABLED=0
}

function prepare_build_file(){
    mkdir -p "${OUTPUTDIR}"
    cp -rf "${PROJECT_ROOT}/build/hub/charts" "${OUTPUTDIR}/"
    cp -rf "${PROJECT_ROOT}/build/hub/images" "${OUTPUTDIR}/"
}

# ----- 版本渲染（2026-09-21 新增）-----
# 背景：此前 build.sh 只把 version 用在 ldflags / 镜像 tag / 文件名三处，
#       chart 内 values.yaml 的 imageAddr 与 Chart.yaml 的 version 是硬编码 v0.0.1，
#       导致发 v0.0.2 的包、chart 里仍指向 v0.0.1 镜像（交付包与镜像 tag 脱节）。
# 现在：打包前把 version 渲染进 output/charts 的副本（不动仓内源文件）。

function sed_inplace(){
    # 不用 sed -i：BSD 与 GNU 的 -i 语义不同、本机可能混装
    # （实测：GNU sed 会把 -i 后的空串当脚本、把表达式当文件名而报错），
    # 统一走「临时文件 + 覆盖」，两种平台行为一致
    local expr="$1" file="$2"
    sed "${expr}" "${file}" > "${file}.tmp" && mv "${file}.tmp" "${file}"
}

# 读 build/hub/versions.yaml（三组件配套版本的事实源）并校验；导出 CONSOLE_VER / RUNNER_VER
function is_semver(){
    # 严格 SemVer 2.0.0（https://semver.org/lang/zh-CN/）：MAJOR.MINOR.PATCH[-PRERELEASE][+BUILD]
    # 允许 v 前缀（git tag 约定，不属于 SemVer 本体）。
    # 字面量一律用 [.] [+] 而非 \. \+，避免 BSD / GNU 的 ERE 转义差异。
    local out
    out=$(printf '%s\n' "$1" | sed -nE '/^v?(0|[1-9][0-9]*)[.](0|[1-9][0-9]*)[.](0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*)([.](0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*))*))?([+]([0-9a-zA-Z-]+([.][0-9a-zA-Z-]+)*))?$/p')
    [ -n "${out}" ]
}

function has_semver_suffix(){
    # 是否带预发布（-x）或构建（+x）后缀
    case "$1" in
        *-*|*+*) return 0 ;;
        *)       return 1 ;;
    esac
}

function resolve_versions(){
    local vf="${PROJECT_ROOT}/build/hub/versions.yaml"
    if [ ! -f "${vf}" ]; then
        echo "ERROR: 缺少版本清单 ${vf}（package-versions 的事实源）" >&2
        exit 1
    fi
    CONSOLE_VER=$(sed -nE 's/^console:[[:space:]]*"?([^"[:space:]]+)"?[[:space:]]*$/\1/p' "${vf}" | head -1)
    RUNNER_VER=$(sed  -nE 's/^runner:[[:space:]]*"?([^"[:space:]]+)"?[[:space:]]*$/\1/p' "${vf}" | head -1)
    local hub_ver
    hub_ver=$(sed   -nE 's/^hub:[[:space:]]*"?([^"[:space:]]+)"?[[:space:]]*$/\1/p'      "${vf}" | head -1)
    if [ -z "${CONSOLE_VER}" ] || [ -z "${RUNNER_VER}" ]; then
        echo "ERROR: ${vf} 缺少 console / runner 版本（三个键都必须有值）" >&2
        exit 1
    fi

    # 版本号规范：三个清单值 + 本次 tag 都必须是合法 SemVer。
    # 非法值会被 render_chart_version 写进 Chart.yaml 的 version，helm lint / helm install 直接失败，
    # 故在此提前失败并给人话提示。带后缀（-r1）不致命，但语义与直觉相反，必须提示。
    local bad="" pair kn vv
    for pair in "console=${CONSOLE_VER}" "hub=${hub_ver}" "runner=${RUNNER_VER}" "tag=${version}"; do
        kn=${pair%%=*}
        vv=${pair#*=}
        if ! is_semver "${vv}"; then
            echo "ERROR: ${kn}='${vv}' 不是合法 SemVer（形如 v1.2.3 / v1.2.3-r1；参考 https://semver.org/lang/zh-CN/）" >&2
            bad=1
        elif has_semver_suffix "${vv}"; then
            echo "NOTE: ${kn}='${vv}' 带后缀 —— SemVer 规定 pre-release（-r1）优先级**低于**同号正式版（0.1.2-r1 < 0.1.2）；build metadata（+x）不参与优先级比较，且 Docker/OCI 镜像 tag **不允许 +**"
        fi
    done
    if [ -n "${bad}" ]; then
        exit 1
    fi

    if [ "${hub_ver}" != "${version}" ]; then
        echo "WARN: versions.yaml 里 hub=${hub_ver:-<空>}，本次构建 tag=${version}（以 tag 为准）"
    fi
    echo "== versions.yaml: console=${CONSOLE_VER} hub=${version} runner=${RUNNER_VER} =="
}

# 渲染 chart 副本：imageAddr 的 tag + Chart.yaml 的 version
function render_chart_version(){
    local chartdir="${OUTPUTDIR}/charts/${component}"
    local vf="${chartdir}/values.yaml"
    local cf="${chartdir}/Chart.yaml"
    local repo
    repo=$(sed -nE 's/^[[:space:]]*imageAddr:[[:space:]]*(.*):[^:]*[[:space:]]*$/\1/p' "${vf}" | head -1)
    if [ -z "${repo}" ]; then
        echo "ERROR: 未能在 ${vf} 解析 imageAddr 的仓库地址" >&2
        exit 1
    fi
    sed_inplace "s#^\([[:space:]]*imageAddr:[[:space:]]*\).*#\1${repo}:${version}#" "${vf}"
    sed_inplace "s#^version: .*#version: ${version#v}#" "${cf}"
    echo "chart rendered: imageAddr=${repo}:${version} chartVersion=${version#v}"
}

# hub 专有：把三组件配套版本写进 chart 的 packageVersions 段（供 CM package-versions 使用）
function render_package_versions(){
    local vf="${OUTPUTDIR}/charts/${component}/values.yaml"
    local tmp="${vf}.tmp"
    awk -v c="${CONSOLE_VER}" -v h="${version}" -v r="${RUNNER_VER}" '
        /^packageVersions:[[:space:]]*$/ { inblk=1; print; next }
        inblk && /^[[:space:]]+console:[[:space:]]/ { print "  console: " c; next }
        inblk && /^[[:space:]]+hub:[[:space:]]/     { print "  hub: " h;     next }
        inblk && /^[[:space:]]+runner:[[:space:]]/  { print "  runner: " r;  next }
        inblk && /^[^[:space:]]/ { inblk=0 }
        { print }
    ' "${vf}" > "${tmp}" && mv "${tmp}" "${vf}"
    echo "packageVersions rendered: console=${CONSOLE_VER} hub=${version} runner=${RUNNER_VER}"
}

function build_hub(){
    mkdir -p "${OUTPUTDIR}/staging"
    pushd "${PROJECT_ROOT}" > /dev/null
    echo "== go build (linux/amd64) ${component} =="
    GOOS=linux GOARCH=amd64 go build -trimpath \
        -ldflags "-s -w -X main.buildDate=${buildDate} -X main.version=${version}" \
        -o "${OUTPUTDIR}/staging/${component}" ./cmd/hub
    popd > /dev/null
    if [ -f "${OUTPUTDIR}/staging/${component}" ]; then
        tar -zcvf "${OUTPUTDIR}/images/${component}.tar.gz" -C "${OUTPUTDIR}/staging" "${component}"
        echo "${component} build successfully."
    else
        echo "${component} build failed."
        exit 1
    fi
}

function build_hub_docker_image(){
    pushd "${OUTPUTDIR}/images" > /dev/null
    echo "== docker build ${component}:${version} =="
    docker build --network host . -t "${component}:${version}"
    docker save -o "${OUTPUTDIR}/images/${component}-${version}.tar" "${component}:${version}"
    if [ -f "${OUTPUTDIR}/images/${component}-${version}.tar" ]; then
        echo "${component} build image successfully."
    else
        echo "${component} build image failed."
        exit 1
    fi
    popd > /dev/null
}

# 可选: 有本地 registry（kind 环境 localhost:5000）时打 tag 推送，失败不阻断
function push_to_local_registry(){
    if docker tag "${component}:${version}" "localhost:5000/${component}:${version}" 2>/dev/null; then
        if docker push "localhost:5000/${component}:${version}" 2>/dev/null; then
            echo "${component} pushed to localhost:5000."
        else
            echo "WARN: push to localhost:5000 failed (no local registry?), skip."
        fi
    fi
}

function charts_pack(){
    pushd "${OUTPUTDIR}/charts" > /dev/null
    tar -zcvf "${component}-${version}.tgz" "${component}"
    if [ -f "${component}-${version}.tgz" ]; then
        echo "${component} charts pack successfully."
    else
        echo "${component} charts pack failed."
        exit 1
    fi
    popd > /dev/null
}

function pack(){
    pushd "${OUTPUTDIR}" > /dev/null
    tar -zcvf "${component}-${version}.tar.gz" charts images
    if [ -f "${component}-${version}.tar.gz" ]; then
        echo "${component} pack successfully."
    else
        echo "${component} pack failed."
        exit 1
    fi
    popd > /dev/null
}

prepare_go_mod
prepare_build_file
resolve_versions
render_chart_version
render_package_versions
build_hub
build_hub_docker_image
push_to_local_registry
charts_pack
pack
