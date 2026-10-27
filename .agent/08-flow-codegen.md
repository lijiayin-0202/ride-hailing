# 08 · 命令生成清单 + 开发全流程

> 隶属 AGENT.MD v1.3.0 · 更新日期 2026-10-27
> §1 命令生成（能生成的不手写）；§2 开发全流程；Git 细节见 `agent/03`，数据规则见 `agent/02`。

## 1. 命令生成清单

| 产物 | 命令 | 说明 | 禁止 |
|------|------|------|------|
| 依赖与工具初始化 | `make init` | 下载依赖与 protoc 插件（GOPATH/bin 须在 PATH） | 手动乱放插件 |
| proto → `*.pb.go` | `make api`（= `buf dep update && buf generate`） | 改 `api/**/*.proto` 或 `internal/conf/conf.proto` 后必须重跑 | 手写/手改 `*.pb.go` |
| wire → `wire_gen.go` | `make generate` | 改 `cmd/*/wire.go` 或 ProviderSet 后必须重跑 | 手改 `wire_gen.go` |
| 编译 | `make build` | 产出 `bin/ride-hailing` | 手工拷贝二进制 |
| 本地运行 | `make run`（`go run ./cmd/agent -conf ./configs`） | HTTP :8000 / gRPC :9000 | 自写启动脚本 |
| 测试 / 检查 | `make test` / `make lint` | test 含覆盖率；lint = go vet + gofmt | 跳过失败用例 |
| 镜像 | `make docker` | 多阶段构建 8000/9000 | 宿主机裸跑上生产 |
| 新服务骨架 | `kratos new <name>`（须负责人批准） | 标准布局 | 自建目录 |

- 环境要求：Go ≥ 1.24；buf ≥ 1.32；插件 `protoc-gen-go` / `protoc-gen-go-grpc` / `protoc-gen-go-http`(v2)；确认 `$(go env GOPATH)/bin` 在 PATH。
- 禁改清单（只能重新生成）：`*.pb.go`、`wire_gen.go`、`buf.lock`。生成失败先修命令/环境，禁止手写顶替。
- 执行前 `git status` 确认分支正确；执行后核对生成文件与预期一致；生成物与 proto 同一次 PR 保持一致。

## 2. 开发全流程

| 步骤 | 阶段 | 动作与命令 | 结束条件 |
|------|------|-----------|----------|
| 1 | 开工 | `git switch -c feature/T-xxx-短名 origin/master` | 分支正确 |
| 2 | 环境 | `make init`；确认 PostgreSQL(+pgvector)/Redis/Kafka/模型底座 可用 | 插件在 PATH、中间件可连 |
| 3 | 契约 | `api/agent/v1/agent.proto` 定义 service/message/http 注解（SSE 用 server stream，参考模版 §4，字段号只追加）；先群内同步影响面 | buf lint 通过、调用方（网关）知悉 |
| 4 | 生成 | `make api` | pb.go 与 proto 一致，未手改 |
| 5 | 建模 | 库对象变更：`migrations/` 迁移脚本（含 `CREATE EXTENSION vector`、HNSW 索引）→ 测试库执行 | 脚本与库一致 |
| 6 | 领域层 | `internal/biz/`：agent_domain / tool_domain 实体、Repo 接口、租户校验规则；登记 ProviderSet；逐行注释 | 规则可单测 |
| 7 | 数据层 | `internal/data/`：agent_repo / chat_repo（GORM + pgvector + redis + kafka-go）；`NewData` 初始化与 cleanup | 编译过、测试库可读写 |
| 8 | Blades 封装 | `internal/blades/`：factory（WithInstruction/WithModel/WithTools/WithMemory）、MCP 工具适配器注册、Middleware（日志/限流/敏感词）；按 agent_id 懒加载缓存 | 不散装 new Agent |
| 9 | 服务层 | `internal/service/agent_service.go` 实现生成的 `AgentServiceServer`（校验 + 驱动 Runner 流式输出） | 无业务逻辑混入 |
| 10 | 传输层 | `internal/server/http.go` / `grpc.go` TODO 处注册服务与中间件 | SSE/gRPC 可达 |
| 11 | 注入 | 改 `cmd/agent/wire.go` → `make generate` | 编译通过 |
| 12 | 配置 | 新配置：`conf.proto` 追加字段 → `make api` → 同步 `configs/config.yaml`（敏感值走环境变量注入，读取一律 `conf.XXX`，禁止 `os.Getenv`，见 `agent/04 §2`） | 无明文密钥、无裸 `os.Getenv` |
| 13 | 验证 | `make lint && make test` 全绿 → `make run` 起服 | 全绿 + 起服成功 |
| 14 | 测试 | Apipost 用例（正常 / 参数非法 / 未登录，流式接口验证 SSE 事件序列）或 gRPC 客户端实测 → 浏览器验证前端会话页 → **测试流执行一轮、链路跑通无报错即可**（`AGENT.MD §9`） | 五道关全过 |
| 15 | 提交 | 生成物与业务实现分开 commit（`AGENT.MD §5`）→ push → PR（附证据）→ 合并 → 删分支 → 改台账 | PR 合并、分支已删 |

> 分层依据（模版约定）：api 只放 proto 契约，service 只做校验编排，biz 只写领域逻辑，data 只做持久化，blades 只做编排封装，禁止跨层反向依赖。

## 3. 结束标志

- [ ] 全程在 feature 分支，未在 master 写业务代码
- [ ] 生成物全部由命令生成，与 proto 同 PR 一致
- [ ] 逐行注释齐全，改动处注释同步（`AGENT.MD §7.2`）
- [ ] 本地验证全绿（make lint / test / run）
- [ ] 真实测试库（含 pgvector 检索用例）验证通过，测试数据已清理
- [ ] Apipost 用例全绿（含 SSE 流式事件序列）；前端会话页浏览器无报错
- [ ] 测试流已执行一轮、链路跑通无报错（多轮回归按需自测，不作阻断）
- [ ] commit 符合 `AGENT.MD §5`；PR 批准合并；分支已删；台账已更新

## 4. 反例（出现即回退）

在 master 写业务代码、`git add .`、`--no-verify`、手改 `*.pb.go`/`wire_gen.go`/`buf.lock`、散装调用模型 API（绕过 blades 封装）、把密钥或模型原始报错透传调用方、测试流未跑通即提交、mock 冒充真实库、冲突强推、私加依赖/新开目录、换框架。
