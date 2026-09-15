# software-distribution-platform-hub
软件发布平台控制面。技术栈：go gin框架。

## 本地开发 / 管理命令

管理命令入口为本仓 `Makefile`（`make <target>`，`make help` 可列出全部）；服务启停、清理的底层实现见 `hack/svc.sh`。**本仓自包含：所有命令只依赖仓内脚本（`hack/svc.sh`、`build/hub/build.sh`），不依赖仓库外的任何脚本。** 打包/部署形态说明：

- **构建产物统一落在 `output/` 下**（`make build` 的 `output/bin/hub`、`make package` 的 `output/{charts,images}/` 与交付包），清理即一条 `rm -rf output`；
- `package` 产物（交付包）默认版本 `v0.0.1`；
- **起服务**：`make start-dev`（后台运行，pid 文件 + 进程组管理）→ `make stop-dev`；产出镜像 / 交付包用 `make package`；
- hub 的 `go.mod` 含 `replace github.com/rouroumaibing/software-distribution-platform-runner => ../software-distribution-platform-runner`，因此**构建时 runner 仓库须与 hub 处于同级目录**，否则报 `replacement directory ... does not exist`。

| make target | 作用 |
| --- | --- |
| `make build` | 编译二进制到 `output/bin/hub`（`go build ./cmd/hub`） |
| `make package` | 组件打包：交叉编译 → 运行时镜像 + docker save + charts → 交付包 `output/software-distribution-platform-hub-<version>.tar.gz`，并尝试推送本地 registry（`build/hub/build.sh`） |
| `make start-dev` | 启动本地开发服务（`hack/svc.sh start`：pid 文件 + 进程组管理，启动前自动清理旧实例；`go run ./cmd/hub`） |
| `make stop-dev` | 停止本地开发服务（`hack/svc.sh stop`，按 pid 文件 + 进程特征兜底清理） |
| `make clean` | **先停本地服务**，再删生成物（`output/ .run/ coverage/`、历史位置 `bin/`、仓根裸编译二进制、散落单文件），保留下载依赖与工具链；**`docs/` 下的 swaggo 生成物已入库，默认保留** |
| `make clean NO_STOP=1` | 同上，但跳过停服务（CI / 无服务场景） |
| `make clean PURGE_DOCS=1` | 同上，但**连 `docs/` 的 swaggo 生成物一起删**（删后 `make build` 必挂，需 `git checkout -- docs/` 恢复） |
| `make clean-deep` | 本仓彻底清理（删生成物，同 `clean`）；不删下载依赖/工具链，删除范围严格限定在本仓目录内（不触碰仓库外的共享资源） |

## swaggo API 文档（`docs/`）

`docs/{docs.go,swagger.json,swagger.yaml}` 由 `swag init` 生成；但 **`docs.go` 是编译必需输入**（`cmd/hub/main.go` 空白导入该包注册 swagger spec，`/swagger/*any` 依赖它）。按「**编译/打包必需 → 入库**」的生成物规则（与 runner 的 controller-gen 产物 `zz_generated.deepcopy.go` 一致），这三个文件：

- **已入库**，`.gitignore` **不再忽略**，`make clean` **默认不删**；
- 缺失时 `go build ./cmd/hub`、`go vet ./...`、`go test ./...` 会直接失败（新克隆 / CI / release 全挂）——为此 `make build` 加了前置检查，会给出可操作的提示而不是抛 `no required module provides package .../docs`。

**再生成**（改了 handler 的 swagger 注解后）：

```bash
# 前置：需要 swag CLI。本机当前未装 swag，且 go.sum 缺 github.com/urfave/cli/v2，
# 因此 `go run github.com/swaggo/swag/cmd/swag` 暂不可用 —— 先 `brew install swag`（或补 go.sum 条目）
swag init --generalInfo cmd/hub/main.go --parseInternal --output docs
```

生成后连同 `docs/` 一起提交 —— 它是**入库的构建输入**，不是可丢弃产物。裁决依据见 `software-distribution-platform-docs/BUILD-ARTIFACTS.md` 附 C（F-1 双向钢人论证）。

## 设计文档

本组件的设计文档（领域模型、控制面实现 Story、Backlog、下发队列 ADR、用户故事等）已统一收敛到独立的 [`software-distribution-platform-docs`](https://github.com/rouroumaibing/software-distribution-platform-docs) 仓库（单一真源），本仓库不再存放设计文档正文。

- 领域 / 数据模型：[`hub/DATA-MODEL.md`](https://github.com/rouroumaibing/software-distribution-platform-docs/blob/main/hub/DATA-MODEL.md)
- 控制面实现 Story：[`hub/STORY-hub-implementation.md`](https://github.com/rouroumaibing/software-distribution-platform-docs/blob/main/hub/STORY-hub-implementation.md)
- 实现 Backlog（G1–G7）：[`hub/STORY-BACKLOG.md`](https://github.com/rouroumaibing/software-distribution-platform-docs/blob/main/hub/STORY-BACKLOG.md)
- 下发持久队列 ADR：[`hub/ADR-dispatch-durable-queue.md`](https://github.com/rouroumaibing/software-distribution-platform-docs/blob/main/hub/ADR-dispatch-durable-queue.md)
- 跨组件对齐（整体目标 / 授权模型 G7 / 执行模型）：见 docs 仓库 [`README.md` §5](https://github.com/rouroumaibing/software-distribution-platform-docs/blob/main/README.md)

> 本仓库 `docs/design/README.md` 仅保留一个指针，指向上述统一文档库；设计文档的修改请在 docs 仓库进行。
>
> 分工：**手写文档 → docs 仓**（单一真源，本仓只留指针）；**编译必需的生成物 → 留在本仓并入库**
> （`docs/` 根下的 `docs.go` / `swagger.json` / `swagger.yaml` 是 swaggo 生成的 API 文档，非设计文档，
> 被 `cmd/hub/main.go` 按 Go import 路径引用，**不能**挪到 `output/`。详见上方「swaggo API 文档」一节）。
