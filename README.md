# software-distribution-platform-hub
软件发布平台控制面。技术栈：go gin框架。

## 本地开发 / 管理命令

管理命令入口为根 `Makefile`（`make <target>`，`make help` 可列出全部）；服务启停、清理的底层实现见 `hack/svc.sh`。打包/部署形态说明：

- `package` 产物（交付包）默认版本 `v0.0.1`，与工作区根 `deploy-local.sh` 的默认部署版本一致；
- `start-deploy` / `stop-deploy` 复用工作区根 `deploy-local.sh` / `undeploy-local.sh`（hub 的 go.mod `replace` 指向同级 runner 模块，两者须保持同级目录）。

| make target | 作用 |
| --- | --- |
| `make build` | 编译二进制到 `bin/hub`（`go build ./cmd/hub`） |
| `make package` | 组件打包：交叉编译 → 运行时镜像 + docker save + charts → 交付包 `output/software-distribution-platform-hub-<version>.tar.gz`，并尝试推送本地 registry（`build/hub/build.sh`） |
| `make start-dev` | 启动本地开发服务（`hack/svc.sh start`：pid 文件 + 进程组管理，启动前自动清理旧实例；`go run ./cmd/hub`） |
| `make stop-dev` | 停止本地开发服务（`hack/svc.sh stop`，按 pid 文件 + 进程特征兜底清理） |
| `make clean` | 仅删生成物（`bin/ .run/ output/ coverage/`、散落单文件、swaggo 生成的 `docs/docs.go|swagger.{json,yaml}`），保留下载依赖与工具链 |
| `make clean-deep` | 本仓彻底清理（删生成物，同 `clean`）；不删下载依赖/工具链，绝不触碰工作区共享资源（`../.bin` / `../.kubeconfig` / `../.dockerconfig`，属部署形态由 `deploy-local.sh` 管理） |
| `make start-deploy` | 本地全量部署：调用工作区根 `deploy-local.sh`（kind + helm 交付形态） |
| `make stop-deploy` | 本地全量卸载：调用工作区根 `undeploy-local.sh`（保留 kind 集群） |

## 设计文档

本组件的设计文档（领域模型、控制面实现 Story、Backlog、下发队列 ADR、用户故事等）已统一收敛到独立的 [`software-distribution-platform-docs`](https://github.com/rouroumaibing/software-distribution-platform-docs) 仓库（单一真源），本仓库不再存放设计文档正文。

- 领域 / 数据模型：[`hub/DATA-MODEL.md`](https://github.com/rouroumaibing/software-distribution-platform-docs/blob/main/hub/DATA-MODEL.md)
- 控制面实现 Story：[`hub/STORY-hub-implementation.md`](https://github.com/rouroumaibing/software-distribution-platform-docs/blob/main/hub/STORY-hub-implementation.md)
- 实现 Backlog（G1–G7）：[`hub/STORY-BACKLOG.md`](https://github.com/rouroumaibing/software-distribution-platform-docs/blob/main/hub/STORY-BACKLOG.md)
- 下发持久队列 ADR：[`hub/ADR-dispatch-durable-queue.md`](https://github.com/rouroumaibing/software-distribution-platform-docs/blob/main/hub/ADR-dispatch-durable-queue.md)
- 跨组件对齐（整体目标 / 授权模型 G7 / 执行模型）：见 docs 仓库 [`README.md` §5](https://github.com/rouroumaibing/software-distribution-platform-docs/blob/main/README.md)

> 本仓库 `docs/design/README.md` 仅保留一个指针，指向上述统一文档库；设计文档的修改请在 docs 仓库进行。
>
> 注：本仓库 `docs/` 根下的 `docs.go` / `swagger.json` / `swagger.yaml` 是 swaggo 自动生成的 API 文档（非设计文档），仍留在本仓库。
