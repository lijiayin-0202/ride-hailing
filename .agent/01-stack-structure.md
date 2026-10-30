# 01 · Kratos 框架：技术栈、目录结构与模块设计

> 隶属 AGENT.MD v1.2.0 · 更新日期 2026-10-26
> 本文档只描述 **Kratos 微服务框架（含 Blades 智能体模块）**。

## 1. 技术版本

| 项 | 锁定 | 版本依据 | 明确禁止 |
|----|------|----------|----------|
| 微服务框架 | **Kratos v2**（gRPC + HTTP 双协议） | `go.mod`（Go ≥ 1.24） | go-zero、Hertz、裸 grpc-go |
| Agent 编排 | **Blades**（Kratos 官方 AI Agent 组件，统一管理 Model / Tool / Memory / Middleware） | `go.mod` | 自研编排器、LangChainGo |
| IDL 与生成 | **Protobuf + buf**（buf.yaml / buf.gen.yaml / buf.lock） | buf ≥ 1.32 | 手写 `*.pb.go` |
| 依赖注入 | **wire**（`cmd/*/wire.go` → `wire_gen.go`） | `make generate` | 手改 `wire_gen.go` |
| ORM | **GORM + `gorm.io/driver/postgres`** | `go.mod` | sqlx、ent、手写连接池 |
| 数据库 | **PostgreSQL 15+** | 部署环境 | MySQL、MongoDB |
| 向量库 | **pgvector**（同 PG 实例，独立库，用于 RAG 检索） | `CREATE EXTENSION vector` | Milvus、Qdrant、ES 向量 |
| 消息队列 | **Kafka**（`segmentio/kafka-go`） | `go.mod` | RocketMQ、RabbitMQ |
| 注册/配置中心 | Nacos / Consul / Etcd **三选一**，选定登记：`{{待负责人确认}}` | 部署环境 | 混用多种 |
| 会话记忆 | **Redis**（多轮上下文 Memory） | 部署环境 | 内存 Map 存生产会话 |
| 观测 | OpenTelemetry + Jaeger + Prometheus | `internal/server` 中间件 | 私自换中间件栈 |
| 后台管理 | gin-vue-admin（独立仓库，管理 Agent 规约 / prompt / 工具绑定配置） | 需求 #1 | go-admin、裸 Gin 权限 |

## 2. 模块职责

基于 Kratos + Blades 的 **Agent 核心微服务**：只对网关/BFF 提供 gRPC 调用，同时暴露 HTTP SSE 流式接口给前端。职责清单：

- 加载 Agent 规约、system prompt、工具绑定配置
- Blades Agent 编排：模型调用、工具调用、多轮上下文记忆、思考链管理
- MCP 工具调度、RAG 检索调用（pgvector）、FunctionCall 处理
- 流式对话输出（SSE / gRPC Stream）
- 服务治理：限流、熔断、重试、超时、链路追踪、日志埋点
- 租户/数据隔离、Agent 执行审计日志

## 3. 目录结构（Kratos 标准 DDD + Blades 模块）

```
ride-hailing/
├── api/
│   └── agent/v1/agent.proto    # proto IDL：gRPC + http 注解（SSE stream），唯一契约源
├── cmd/agent/
│   ├── main.go                 # 启动入口：解析 -conf → wire 组装 kratos App → 优雅退出
│   ├── wire.go / wire_gen.go   # wire 声明 / 生成结果（禁手改）
├── configs/config.yaml         # 模型密钥(占位)、注册中心、redis、kafka、治理配置
├── internal/
│   ├── conf/                   # conf.proto → conf.pb.go（Bootstrap/Server/Data 结构，生成）
│   ├── biz/                    # 领域层：agent_domain.go、tool_domain.go（实体/Repo 接口/规则）
│   ├── data/                   # 数据层：agent_repo.go、chat_repo.go（PG/pgvector/redis/kafka 客户端）
│   ├── service/                # agent_service.go：实现生成的 AgentServiceServer，对接 Blades Runner
│   ├── blades/                 # 内部 Blades 封装：factory.go（Agent 工厂）、tools/（MCP 适配器）、memory/（Redis 会话）
│   └── server/                 # grpc.go / http.go：服务注册与中间件（recovery/tracing/logging）
├── pkg/                        # 公共包：错误码、脱敏、格式校验
├── third_party/                # buf 远程 proto 依赖
├── Makefile                    # init / api / build / run / test / lint / generate / docker
├── buf.yaml / buf.gen.yaml / buf.lock
├── Dockerfile                  # 多阶段构建，暴露 8000(HTTP)/9000(gRPC)
└── go.mod
```

## 4. 分层职责（禁止跨层反向依赖）

| 层 | 只允许做 | 禁止 |
|----|----------|------|
| `api/` | proto 契约：service、message、http 注解（SSE 用 server stream） | 写业务逻辑 |
| `internal/service/` | 参数校验、调 biz、组装响应、驱动 Blades Runner 流式输出 | 直连 DB、写领域规则 |
| `internal/biz/` | Agent 领域实体、Repo 接口、租户校验、事件定义 | import service / data 实现 |
| `internal/data/` | 实现 Repo：GORM(PG)、pgvector 检索、redis Memory、kafka 收发 | 写业务规则；上层透传 gorm 错误 |
| `internal/blades/` | Agent 工厂（WithInstruction/WithModel/WithTools/WithMemory）、工具注册、中间件 | 业务规则、散装 new Agent |
| `internal/server/` | 注册 gRPC/HTTP 服务与中间件 | 业务逻辑 |
| `internal/conf/` | 配置结构定义 | 业务常量 |

## 5. Blades 组件使用规范

1. **Memory**：Redis 存会话上下文，多轮对话复用；key 含 `session_id`，设 TTL。
2. **Tool**：MCP 工具统一封装适配器后注册进 Blades，**禁止散装调用模型 API**。
3. **Middleware**：日志、限流、输入过滤、敏感词拦截，统一在 blades 封装层挂载。
4. **Agent 实例**：按 `agent_id` 懒加载 + 缓存，避免重复编译 prompt；配置变更（经 gva 后台下发）后失效重建。

## 6. 治理与异常规范

| 项 | 规则 |
|----|------|
| 超时 | 单次调用 30s（配置项，禁止硬编码） |
| 重试 | 非业务错误最多 1 次；业务错误不重试 |
| 熔断 | 连续失败阈值触发，熔断期直接返回友好错误 |
| 限流 | 按 租户 / agent_id 维度令牌桶 |
| 流控 | SSE 连接超时自动断开，心跳保活 |
| 领域校验错误 | 自定义 kratos error，带业务 code |
| 底层异常 | 内部记完整堆栈，对外返回简化 message；**禁止把密钥、模型原始报错栈透传调用方** |
| 流式中断 | 主动推送 ERROR 事件并关闭流，禁止 panic |

## 7. 与网关 / BFF 交互规则

- 调用方只有网关/BFF，**前端不直连本服务**；鉴权统一在网关层完成，本服务只做租户数据隔离校验。
- 网关两种模式二选一：① 作为 gRPC client 消费流再转发 SSE；② 直接透传本服务 HTTP SSE 接口。

## 8. 依赖与新技术引入判定（先提案后动手）

满足任一条必须先群内提案：新增 go.mod 依赖；新增顶层目录/中间件/注册中心；升级框架大版本；引入新存储组件。判定标准：现有栈做不到 + 有替代方案 + 负责人书面确认。
