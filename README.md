# ride-hailing（子项目二：打车微服务）

radish-go 仓库下的**独立 go module**：`github.com/lijayin-0202/radish-ride-service`。
由 `kratos new` 标准布局裁剪而来，已实现成员 B 的叫车下单与订单全行程模块。

> 本目录**不包含 kratos 框架源码**，全部依赖由 `go.mod` 从远端拉取（`github.com/go-kratos/kratos/v2`）。

## 成员 B：订单全行程

服务提供车型询价、创建订单、订单详情、SSE 行程事件、订单命令、支付、历史订单和评价接口。HTTP 鉴权由网关完成，网关需注入 `X-Passenger-ID`；写接口的幂等键使用 `X-Idempotency-Key`。

启动前通过环境变量注入配置，禁止把真实凭据写进仓库：

```bash
export RIDE_DATA_DATABASE_SOURCE='host=... port=5432 user=... password=... dbname=... sslmode=require options=-c\ search_path=robotaxi,public'
export RIDE_DATA_SECURITY_CONTACT_PHONE_KEY='至少 32 字节的随机密钥'
make run
```

数据库变更脚本位于 `migrations/`，包含 up/down。执行前必须在测试库评审和验证，应用启动时不会自动迁移数据库。

## 目录说明

| 路径 | 说明 | 当前状态 |
| --- | --- | --- |
| `cmd/ride-hailing/main.go` | 服务入口：解析配置 → 组装 kratos App → 优雅退出 | 保留 |
| `cmd/ride-hailing/wire.go` | wire 依赖注入声明（`wireinject` 构建标签） | 保留 |
| `cmd/ride-hailing/wire_gen.go` | wire 生成结果（`!wireinject`，可直接编译） | 保留 |
| `api/ridehailing/v1/ridehailing.proto` | 成员 B 的 HTTP/gRPC 接口契约 | 已实现 |
| `api/ridehailing/v1/ridehailing.pb.go` | 上述 proto 的生成产物 | 生成 |
| `configs/config.yaml` | 运行配置：HTTP `:8000`、gRPC `:9000`、数据源 | 保留 |
| `internal/conf/conf.proto` | `Bootstrap / Server / Data` 配置结构 | 保留 |
| `internal/conf/conf.pb.go` | 配置结构生成代码 | 生成 |
| `internal/biz/` | 订单领域模型、状态机和用例 | 已实现 |
| `internal/data/` | PostgreSQL、事务、幂等和数据隔离 | 已实现 |
| `internal/service/` | Proto 服务实现和 DTO 转换 | 已实现 |
| `internal/server/http.go` | HTTP Server、中间件和 SSE 路由 | 已注册 |
| `internal/server/grpc.go` | gRPC Server 和业务服务 | 已注册 |
| `migrations/` | 成员 B 数据库 up/down 脚本 | 待人工执行 |
| `internal/server/server.go` | `ProviderSet` | 保留 |
| `buf.yaml` / `buf.gen.yaml` / `buf.lock` | protobuf 生成配置与依赖锁定 | 保留 |
| `Makefile` | `init / api / build / run / test / lint / generate / docker` | 保留 |
| `Dockerfile` | 多阶段构建，暴露 8000 / 9000 | 保留 |
| `third_party/README.md` | 第三方 proto 依赖说明（使用 buf 远程依赖） | 保留 |

## 环境要求

| 组件 | 版本 | 说明 |
| --- | --- | --- |
| Go | >= 1.24 | `go version` |
| buf | >= 1.32 | 改 proto 后需要：`go install github.com/bufbuild/buf/cmd/buf@latest` |
| protoc-gen-go | latest | `go install google.golang.org/protobuf/cmd/protoc-gen-go@latest` |
| protoc-gen-go-grpc | latest | `go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest` |
| protoc-gen-go-http | v2 | `go install github.com/go-kratos/kratos/cmd/protoc-gen-go-http/v2@latest` |

安装后请确保 `$(go env GOPATH)/bin`（Windows 常见为 `D:\GoWork\bin`）已加入 PATH，否则 buf 找不到插件。

## 初始化与启动

```bash
cd ride-hailing
go mod download
go run ./cmd/ride-hailing -conf ./configs     # 或 make run
```

启动成功日志：

```text
[gRPC] server listening on: [::]:9000
[HTTP] server listening on: [::]:8000
```

编译与测试：

```bash
go build -o bin/ride-hailing ./cmd/ride-hailing   # 或 make build
go test ./... -cover                              # 或 make test
```

## 常用命令

| 命令 | 说明 |
| --- | --- |
| `make init` | 下载依赖与代码生成工具 |
| `make api` | 按 `api/**/*.proto` 重新生成 `*.pb.go` |
| `make build` | 编译到 `bin/` |
| `make run` | 直接运行（读 `./configs`） |
| `make test` | 跑测试并输出覆盖率 |
| `make lint` | `go vet` + `gofmt` 检查 |
| `make generate` | 执行 wire 重新生成 `wire_gen.go` |
| `make docker` | 构建镜像 |

## 后续开发指引（按顺序补全，避免破坏分层）

| 步骤 | 操作 | 涉及文件 |
| --- | --- | --- |
| ① 定义接口 | 在 `api/ridehailing/v1/` 下新增模块 proto（如 `order.proto`），写 `message` 与 `service` | `api/**/*.proto` |
| ② 生成代码 | `buf dep update && buf generate`（或 `make api`） | 生成 `*.pb.go` |
| ③ 领域层 | 定义实体、Repo 接口、Usecase，并登记到 `biz.ProviderSet` | `internal/biz/` |
| ④ 数据层 | 实现 Repo（MySQL / Redis），在 `NewData` 中初始化连接与 `cleanup` | `internal/data/` |
| ⑤ 服务层 | 新增 `XxxService` 实现生成的 `XxxServer`（内嵌 `UnimplementedXxxServer`），登记到 `service.ProviderSet` | `internal/service/` |
| ⑥ 传输层 | 在 `http.go` / `grpc.go` 注册 `RegisterXxxHTTPServer` / `RegisterXxxServer` | `internal/server/` |
| ⑦ 依赖注入 | 修改 `wire.go` 并在 `cmd/ride-hailing` 下执行 `wire`，重新生成 `wire_gen.go` | `cmd/ride-hailing/` |
| ⑧ 配置 | 新增配置项先改 `internal/conf/conf.proto`，再 `buf generate`，最后同步 `configs/config.yaml` | `internal/conf/`、`configs/` |

> 分层约定：`api` 只放 proto 契约，`service` 只做参数校验与编排，`biz` 只写领域逻辑，`data` 只做持久化，禁止跨层反向依赖。

## 当前接口清单

当前已注册的 HTTP / gRPC 业务接口包括：车型与费用试算、创建订单、订单详情、订单历史、订单命令、支付记录和行程评价；行程事件通过 HTTP SSE 与 gRPC streaming 提供。
HTTP 默认监听 `:8000`，gRPC 默认监听 `:9000`，并挂载 `recovery / tracing / logging` 中间件。HTTP 写接口由网关注入 `X-Passenger-ID`，创建订单、订单命令和评价使用 `X-Idempotency-Key` 保证幂等。
