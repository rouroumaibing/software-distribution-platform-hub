# Makefile — software-distribution-platform-hub 本地管理
# 原生工具链（Go 项目）：make <target>
# 本仓自包含：只依赖仓内脚本（hack/svc.sh、build/$(APP)/build.sh），不依赖仓库外的任何脚本。
#
# clean       = 先停本地服务，再删【本仓生成物】，保留下载的依赖/工具链
# clean-deep  = 本仓彻底清理（生成物 + 仓内下载依赖）
# 注意：删除一律限定在本仓目录（$(CURDIR)）内，绝不触碰仓库外的共享资源（工具链目录 /
#       kubeconfig / 容器运行时配置等可能被别的仓共用），仓级清理越界会破坏本地环境。
# 一律用显式路径删除，绝不触碰源码（cmd/ internal/ pkg/ go.mod ...）
# clean 也不删 docs/ 下的 swaggo 生成物（编译必需输入且已入库，见下方 DOCS_GEN 注释）

APP := hub

# 构建产物统一落在 output/ 下（与 package 的交付产物同根，clean 一条 rm -rf 即可清空）。
#   output/bin/$(APP)  <- make build
#   output/staging/**  <- build.sh 交叉编译中间物
#   output/{charts,images}/**、output/*.tar.gz <- build.sh 交付产物
# 刻意不写回仓根 bin/：产物集中在 output/ 才能"一条命令清干净"（对齐参考工程 old/go-devops 的 output/ 收敛做法）。
BIN := output/bin/$(APP)

.PHONY: help clean clean-deep build package start-dev stop-dev

help:
	@echo "targets:"
	@echo "  clean        停本地服务后删本仓生成物(output/ .run/ coverage/)"
	@echo "  clean-deep   同 clean（本仓无仓内下载依赖）"
	@echo "  build        编译二进制到 $(BIN)"
	@echo "  package      组件打包：构建镜像并推送本地 registry (build/$(APP)/build.sh)"
	@echo "  start-dev    启动本地开发服务(后台, pid 文件 + 进程组管理)"
	@echo "  stop-dev     停止本地开发服务(按 pid 文件 + 进程清理)"
	@echo ""
	@echo "  注: clean 会先停本地开发服务；跳过请用 NO_STOP=1 make clean"
	@echo "  注: docs/ 下的 swaggo 生成物已入库、clean 默认保留；确要删用 PURGE_DOCS=1 make clean"

# 生成物（clean 与 clean-deep 都会删；绝不删源码 / 下载依赖）
#   output/  = 全部构建产物（bin/ staging/ charts/ images/ 交付包）
#   bin/     = 历史位置（2026-09-15 前 make build 落在仓根 bin/），保留清理以防旧残留
GEN_DIRS := output .run coverage bin

# 清理前先停服务：服务在运行时产物/pid 仍被持有，先停再删才不会留下孤儿进程或半删状态。
# 跳过：NO_STOP=1 make clean
ifeq ($(NO_STOP),)
STOP_BEFORE_CLEAN = @bash hack/svc.sh stop
else
STOP_BEFORE_CLEAN = @echo "[clean:$(APP)] NO_STOP=1，跳过停服务"
endif

# swaggo docs（F-1 已裁决，2026-09-15）：
#   docs/{docs.go,swagger.json,swagger.yaml} 由 `swag init` 生成，但 docs.go 是
#   **编译必需输入**（cmd/hub/main.go:18 空白导入注册 swagger spec）。
#   生成物分岔规则：**编译/打包必需 → 入库**（同 runner 的 controller-gen 产物）；只有纯运行/
#   本地便利产物才忽略 + 可 clean（见 docs 仓 BUILD-ARTIFACTS.md 附 C 的双向钢人论证）。
#   因此：这三个文件**已入库**、`.gitignore` 不再忽略、**clean 默认不删**。
#   仅当确认要放弃本地构建能力时才用 PURGE_DOCS=1（删后 `make build` 必挂，需 git checkout 恢复）。
DOCS_GEN := docs/docs.go docs/swagger.json docs/swagger.yaml
ifeq ($(PURGE_DOCS),)
CLEAN_DOCS = @echo "[clean:$(APP)] 保留 $(DOCS_GEN)（编译必需输入，已入库）"
else
CLEAN_DOCS = rm -f $(addprefix $(CURDIR)/,$(DOCS_GEN))
endif

clean:
	$(STOP_BEFORE_CLEAN)
	@echo "[clean:$(APP)] 删除生成物 ($(GEN_DIRS))，保留依赖/工具链"
	rm -rf $(addprefix $(CURDIR)/,$(GEN_DIRS))
	# 散落单文件生成物
	find $(CURDIR) -maxdepth 2 \( -name '*.test' -o -name '*.out' -o -name '*.err.txt' -o -name '*_err.txt' -o -name '*.prof' -o -name '*.coverprofile' -o -name '.DS_Store' -o -name '*.swp' \) -delete 2>/dev/null
	# 裸跑 `go build ./cmd/$(APP)` 会把二进制丢在仓根（.gitignore 已锚定忽略 /$(APP)），显式清理（只此一个文件，不碰 cmd/$(APP)/ 源码目录）
	rm -f $(CURDIR)/$(APP)
	$(CLEAN_DOCS)
	@echo "[clean:$(APP)] done."

clean-deep: clean
	@echo "[clean:$(APP)] done (deep)."

build:
	# docs/docs.go 是编译必需输入（main.go 空白导入）；缺失时给出可操作的提示，
	# 而不是抛一句难懂的 `no required module provides package .../docs`。
	@test -f $(CURDIR)/docs/docs.go || { \
		echo "[build:$(APP)] FATAL: docs/docs.go 缺失（编译必需，且已入库）。"; \
		echo "              恢复: git checkout -- docs/   |   再生成: 见 README「swaggo API 文档」"; \
		exit 1; }
	@mkdir -p $(dir $(BIN))
	go build -o $(BIN) ./cmd/$(APP)

package:
	bash build/$(APP)/build.sh

start-dev:
	@bash hack/svc.sh start

stop-dev:
	@bash hack/svc.sh stop
