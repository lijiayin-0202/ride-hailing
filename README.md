# ride-hailing（子项目二：打车微服务 · Kratos 空脚手架）

radish-go 仓库下的**独立 go module**：`github.com/lijayin-0202/radish-ride-service`。
由 `kratos new` 标准布局裁剪而来，当前是**不含任何业务逻辑的纯净脚手架**，用于团队后续按模块填充打车业务。

> 本目录**不包含 kratos 框架源码**，全部依赖由 `go.mod` 从远端拉取（`github.com/go-kratos/kratos/v2`）。

## 目录说明

| 路径 | 说明 | 当前状态 |
| --- | --- | --- |
| `cmd/ride-hailing/main.go` | 服务入口：解析配置 → 组装 kratos App → 优雅退出 | 保留 |
| `cmd/ride-hailing/wire.go` | wire 依赖注入声明（`wireinject` 构建标签） | 保留 |
| `cmd/ride-hailing/wire_gen.go` | wire 生成结果（`!wireinject`，可直接编译） | 保留 |
| `api/ridehailing/v1/ridehailing.proto` | proto 模板（仅 syntax / package / go_package + 使用说明） | **空白模板** |
| `api/ridehailing/v1/ridehailing.pb.go` | 上述 proto 的生成产物 | 生成 |
| `configs/config.yaml` | 运行配置：HTTP `:8000`、gRPC `:9000`、数据源 | 保留 |
| `internal/conf/conf.proto` | `Bootstrap / Server / Data` 配置结构 | 保留 |
| `internal/conf/conf.pb.go` | 配置结构生成代码 | 生成 |
| `internal/biz/biz.go` | 用例层 `ProviderSet` | **空模板** |
| `internal/data/data.go` | 数据层 `Data` 结构 + `NewData` + `cleanup` | **空模板** |
| `internal/service/service.go` | 服务层 `ProviderSet` | **空模板** |
| `internal/server/http.go` | HTTP Server + 中间件，路由注册处留 TODO | 空注册 |
| `internal/server/grpc.go` | gRPC Server + 中间件，服务注册处留 TODO | 空注册 |
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
| ⑥ 传输层 | 在 `http.go` / `grpc.go` 的 TODO 处注册 `RegisterXxxHTTPServer` / `RegisterXxxServer` | `internal/server/` |
| ⑦ 依赖注入 | 修改 `wire.go` 并在 `cmd/ride-hailing` 下执行 `wire`，重新生成 `wire_gen.go` | `cmd/ride-hailing/` |
| ⑧ 配置 | 新增配置项先改 `internal/conf/conf.proto`，再 `buf generate`，最后同步 `configs/config.yaml` | `internal/conf/`、`configs/` |

> 分层约定：`api` 只放 proto 契约，`service` 只做参数校验与编排，`biz` 只写领域逻辑，`data` 只做持久化，禁止跨层反向依赖。

## 当前接口清单

**空脚手架，尚未注册任何 HTTP / gRPC 业务接口。**
HTTP 监听 `:8000`、gRPC 监听 `:9000`，均只挂载了 `recovery / tracing / logging` 中间件，等待第一步「定义接口」后填充。
