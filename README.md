# ride-hailing（子项目二：打车微服务）

radish-go 仓库下的**独立 go module**：`github.com/lijayin-0202/radish-ride-service`。
由 `kratos new` 标准布局裁剪而来，当前已完成：

- **模块1 用户账号与个人中心（成员A）**：登录注册（T0）、实名认证与档案维护（实名、常用地址、紧急联系人）、
  个人中心（券/积分/签到/消息）、登录设备与登录记录，共 23 个 rpc（22 个 HTTP + 1 个仅 gRPC）。
  姓名与身份证号按 UA-T0-09 另存 AES-256-GCM 密文（`pkg/securebox`），密钥由 `RIDE_DATA_KEY` 注入。
- **模块2 叫车下单与订单全行程（成员B）**：车型费用试算、下单、派车与车辆确认、行程推进、支付回调、评价与 SSE 事件流，
  并对外提供 GVA 后台调用的 internal 接口（订单/乘客/车辆/派单记录）。
- **模块3 运营区域、站点与车辆派单**：区域/站点管理、自动派单、派单状态回调、
  Outbox 投递、超时扫描与补派工作者；跨模块契约见 `docs/模块3跨模块派单契约.md`。

> 后台服务（gin-vue-admin）位于仓库根目录 `../radish-admin-server`，
> 本目录**不再内嵌副本**（历史内嵌目录已与根目录逐文件哈希比对一致后移除，
> 其独有的 AGENT 总纲已归档到根目录 `docs/AGENT-总纲-v1.0.0.md`）。

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

线上（`robotaxi` schema）已具备 v1.0.0 全量结构与 `schema_versions` 台账，`0001` 的字段已在库中存在；`0002` 已执行并记录为 `2.0.0-40-member-b`，只包含两项增量：`order_commands.command_type` 枚举扩展、`order_fee_items` 金额符号约束。状态枚举（0–7）、活动订单唯一索引、成功支付唯一索引、评价唯一索引、轨迹时间索引均由基础结构提供，迁移脚本不再重复创建。

> 已知数据问题：`service_areas.city_code=110100`，但 `vehicle_service_types.city_code=420100`，而演示站点名为宿迁。试算按 `city_code` 查车型，客户端传 `321300` 会返回 `no available service type`，需由数据维护方统一城市编码。

## 目录说明

| 路径 | 说明 | 当前状态 |
| --- | --- | --- |
| `cmd/ride-hailing/main.go` | 服务入口：解析配置 → 组装 kratos App → 优雅退出 | 保留 |
| `cmd/ride-hailing/wire.go` | wire 依赖注入声明（`wireinject` 构建标签） | 保留 |
| `cmd/ride-hailing/wire_gen.go` | wire 生成结果（`!wireinject`，可直接编译） | 保留 |
| `api/ridehailing/v1/ridehailing.proto` | 成员 B 的 HTTP/gRPC 接口契约 | 已实现 |
| `api/ridehailing/v1/ridehailing.pb.go` | 上述 proto 的生成产物 | 生成 |
| `api/ridehailing/v1/passenger_account.proto` | 模块1 契约：23 个 rpc（22 个 HTTP + 1 个仅 gRPC） | 已实现 |
| `api/ridehailing/v1/service_area.proto` / `station.proto` / `dispatch_contract.proto` | 模块3 区域、站点与派单契约 | 已实现 |
| `docs/模块1乘客账号与个人中心接口文档.md` | 模块1 接口/字段/错误码/枚举/表映射说明 | 已维护 |
| `migrations/` | 模块1与模块3 PostgreSQL 迁移脚本，**需评审后由 DBA 执行，服务不自动迁移** | 已交付 |
| `configs/config.yaml` | 运行配置：HTTP `:8000`、gRPC `:9000`、数据源 | 保留 |
| `internal/conf/conf.proto` | `Bootstrap / Server / Data` 配置结构 | 保留 |
| `internal/conf/conf.pb.go` | 配置结构生成代码 | 生成 |
| `internal/biz/` | 领域模型、状态机与用例；`biz.ProviderSet` 已登记 | 模块1（登录/实名/地址/联系人/积分/券/消息/设备）与模块2（订单、试算、支付、评价）已实现 |
| `internal/data/` | 数据层 `Data` + `NewData` + `cleanup`；PostgreSQL 事务、幂等与数据隔离 | 已接入 PostgreSQL + Redis（`pkg/cache` 统一构造） |
| `internal/service/` | Proto 服务实现与 DTO 转换；`service.ProviderSet` 已登记 | 已注册 `PassengerAccountService` 与订单相关服务 |
| `internal/server/http.go` | HTTP Server、中间件与 SSE 路由 | 已注册模块1 HTTP 路由（22 个）及模块2 订单路由 |
| `internal/server/grpc.go` | gRPC Server、中间件与业务服务 | 已注册模块1 gRPC 服务（含内部实名校验）及模块2 订单服务 |
| `migrations/0001_member_b_*` / `0002_member_b_*` | 成员 B 数据库 up/down 脚本（订单与试算、状态机与支付退款） | 待人工执行 |
| `docs/` | 状态机、时序、Kafka 事件契约、E2E 联调脚本 | 已交付 |
| `internal/server/server.go` | `ProviderSet` | 保留 |
| `pkg/cache` | Redis 客户端统一构造（单机/集群 `UniversalClient`，口径对齐 GVA） | 已实现 |
| `pkg/securebox` | AES-256-GCM 加解密（姓名/证件密文，`v1:` 版本前缀） | 已实现 |
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
# 启动前必须注入敏感项（缺任一项会 fail-closed 拒绝启动）
#   RIDE_DATABASE_SOURCE  PostgreSQL 连接串
#   RIDE_ID_CARD_SALT     证件哈希盐值
#   RIDE_DATA_KEY         敏感字段加密密钥（32 字节 base64）
# 本地可用 scripts/run.ps1：自动生成并持久化 RIDE_JWT_SECRET / RIDE_DATA_KEY 到 .env
go run ./cmd/ride-hailing -conf ./configs     # 或 make run
```

> 建表前置：`migrations/20261023_0001_*.up.sql`、`20261027_0002_*.up.sql`、`20261029_0003_*.up.sql`、
> `20261102_0004_*.up.sql`、`20261102_0005_*.up.sql`、`20261102_0006_*.up.sql`（按编号顺序执行，需评审）。

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
| ④ 数据层 | 实现 Repo（PostgreSQL 主库 / Redis 缓存），在 `NewData` 中初始化连接与 `cleanup` | `internal/data/` |
| ⑤ 服务层 | 新增 `XxxService` 实现生成的 `XxxServer`（内嵌 `UnimplementedXxxServer`），登记到 `service.ProviderSet` | `internal/service/` |
| ⑥ 传输层 | 在 `http.go` / `grpc.go` 注册 `RegisterXxxHTTPServer` / `RegisterXxxServer` | `internal/server/` |
| ⑦ 依赖注入 | 修改 `wire.go` 并在 `cmd/ride-hailing` 下执行 `wire`，重新生成 `wire_gen.go` | `cmd/ride-hailing/` |
| ⑧ 配置 | 新增配置项先改 `internal/conf/conf.proto`，再 `buf generate`，最后同步 `configs/config.yaml` | `internal/conf/`、`configs/` |

> 分层约定：`api` 只放 proto 契约，`service` 只做参数校验与编排，`biz` 只写领域逻辑，`data` 只做持久化，禁止跨层反向依赖。

## 当前接口清单

路径统一前缀 `/api/v1/passenger`。HTTP 默认监听 `:8000`，gRPC 默认监听 `:9000`，挂载 `recovery / tracing / 请求日志（不打印参数）` 中间件。

### 模块1 用户账号与个人中心（成员A）

契约见 `api/ridehailing/v1/passenger_account.proto`，字段与错误码说明见 `docs/模块1乘客账号与个人中心接口文档.md`。

| 分组 | 接口 | 优先级 |
| --- | --- | --- |
| 登录注册 | `POST /auth/sms-codes`、`POST /auth/mobile-login`、`POST /auth/password-login` | T0 |
| 个人中心 | `GET /profile`、`PATCH /profile`、`POST /mobile-change`、`PATCH /settings` | T2/T3 |
| 实名档案 | `POST /real-name`、`GET /real-name`（落掩码 + 加盐哈希 + AES-256-GCM 密文） | T2 |
| 常用地址 | `GET/POST /addresses`、`PATCH/DELETE /addresses/{id}` | T2 |
| 紧急联系人 | `GET/PUT /emergency-contact` | T2 |
| 账号安全 | `GET /login-devices`、`GET /login-records` | T2 |
| 券包积分 | `GET /coupons`、`GET /points`、`POST /sign-ins` | T2 |
| 消息中心 | `GET/PATCH /notifications` | T3 |
| 内部能力 | `gRPC CheckRealNameStatus`（无 HTTP 路由，供订单/行程前置校验） | T0 支撑 |

> 管理后台侧（乘客账号状态、登录记录、实名审核）由 `radish-admin-server` 提供，不在本服务暴露。

### 模块2 叫车下单与订单全行程（成员B）

接口路径已与任务书 4.5 冻结契约对齐：

| 优先级 | 接口 | 说明 |
| --- | --- | --- |
| T0 | `POST /api/v1/passenger/fare-options/estimate` | 车型与费用试算，返回可用车数与接驾 ETA |
| T0 | `POST /api/v1/passenger/orders` | 创建订单（`X-Idempotency-Key`），初始状态 0 待派车 |
| T0 | `GET /api/v1/passenger/orders/{id}/live-view` | 活动订单聚合：订单 + 车辆 + 位置 + 派单 + 核验 + 安全事件 |
| T0 | `POST /api/v1/passenger/orders/{id}/cancel` | 取消订单，返回违约金规则与可退金额 |
| T0 | `GET /api/v1/passenger/orders/{id}/settlement-view` | 结算聚合：金额 + 费用明细 + 支付 + 评价状态 |
| T0 | `POST /api/v1/passenger/orders/{id}/payments` | 创建/复用支付单 |
| T0 | `POST /api/v1/payments/callback` | 支付渠道回调（内部令牌），幂等收敛 PAID/FAILED |
| T1 | `GET /api/v1/passenger/orders` | 历史订单游标分页 |
| T2 | `POST /api/v1/passenger/orders/{id}/reviews` | 行程评价（一单一次，支持匿名） |
| — | `GET /api/v1/passenger/orders/{id}/events` | SSE 行程事件流 |
| — | `POST /api/v1/passenger/orders/{id}/commands` | 订单命令（乘客端限 `UNLOCK_AND_START`/`REQUEST_PULL_OVER`） |

聚合鉴权由网关注入 `X-Passenger-ID`；调度/车端推进状态与支付回调必须携带 `X-Internal-Token`（`data.security.internal_token`）。

## 交付文档

| 文档 | 内容 |
| --- | --- |
| `docs/order-state-machine.md` | 状态枚举、迁移表、命令权限、并发与幂等、状态流水字段 |
| `docs/order-sequence.md` | T0 主链路时序图与取消退款时序 |
| `docs/kafka-order-events.md` | 订单状态事件 topic/字段/重试/死信提案（待评审） |
| `docs/e2e-t0.md` | 端到端联调脚本 + 失败回放 |

数据库变更：`migrations/0001_member_b_orders.*`（试算与订单表）与 `migrations/0002_member_b_contract.*`（状态重映射、命令枚举、支付/评价/位置索引、取消与退款表）。两套脚本都含 up/down，执行前须在测试库评审。
