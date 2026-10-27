# 04 · 配置与部署

> 隶属 AGENT.MD v1.2.0 · 更新日期 2026-10-26

## 1. 配置文件结构

- `internal/conf/conf.proto` 定义 `Bootstrap`（Server/Data/Kafka/Registry/Trace/Model 结构）→ `make api` 生成 `conf.pb.go`；**配置结构变更必须先改 conf.proto 再生成**。
- `configs/config.yaml`：HTTP :8000、gRPC :9000、数据源 DSN 占位、redis、kafka brokers、注册中心、治理参数（超时 30s / 重试 1 次）。
- 敏感值（模型密钥、数据库密码、Redis/Kafka 密码）一律 `${ENV_VAR}` 占位，**禁止明文入库**。
- 本地运行读 `./configs`：`go run ./cmd/agent -conf ./configs`（或 `make run`）。

## 2. 配置分层与环境变量口径（**不再维护变量清单表**）

> 本节只写规则，**不罗列环境变量清单**：文档中不建、不追加、不同步任何环境变量表格（含 `.env.example` 与 md 的双向对齐）。

### 2.1 读取入口唯一：`internal/conf`

1. 配置读取**统一收敛在 `internal/conf` 包**：yaml 加载、环境变量读取、默认值填充全部在 conf 内部完成，对外只暴露 `Bootstrap` 结构。
2. **业务代码绝对禁止散落裸调用 `os.Getenv()` / `os.LookupEnv()`**：`api/`、`service/`、`biz/`、`data/`、`internal/blades/`、`internal/server/`、`cmd/` 一律不得直接读环境变量。
3. 业务代码只使用 `conf.XXX`（配置结构体字段或 conf 暴露的取值方法），不感知配置来自 yaml 还是环境变量。

### 2.2 敏感配置 vs 普通配置

| 类别 | 范围 | 存放方式 |
|------|------|----------|
| **敏感项** | 数据库 DSN、各类密码、`API_KEY`、SASL 凭证等 | **只允许从环境变量注入**；禁止写入本地 yaml / 配置文件；禁止把 `.env` 提交到仓库；`.example` 模板里只留占位注释，不填真实密钥 |
| **普通项** | 地址、端口、日志级别、功能开关 | 可写在 yaml 配置；也支持环境变量覆盖（同名优先取环境变量） |

### 2.3 `.env.example` 规则

1. 新增配置项时**按需同步 `.env.example`**，只放占位（如 `MODEL_API_KEY=`），**不填真实值**。
2. `.env.example` **仅作为本地开发参考模板**，不再是"主清单"；**不需要与任何 markdown 表格双向同步**（也不存在这类表格）。
3. 真实 `.env` 不入库（已 gitignore）；线上通过部署环境 Secret 注入。

### 2.4 校验与日志脱敏

1. **代码扫描拦截**：提交/评审前扫描业务目录，命中 `os.Getenv` / `os.LookupEnv` 直接拦截；**仅 `internal/conf` 包内允许使用环境读取函数**。
2. **生成代码同样遵守**：生成 repo / service / api 代码时，配置读取一律 `conf.XXX`，禁止生成 `os.Getenv`。
3. **敏感值脱敏**：DSN、密码、`API_KEY`、SASL 凭证等**禁止在日志中打印完整明文**，必须做掩码（如仅保留首尾 2~4 位或用 `***` 遮蔽）。

## 3. 密钥管理

- 仓库内**禁止**出现数据库/Redis/Kafka 密码、模型密钥、第三方 API Key。
- 本地：`.env`（已 gitignore）；线上：部署环境 Secret 注入。
- `.env.example` 只写占位与说明，不填真实值；泄露密钥立即轮换并在群里通报。

## 4. 构建与部署

| 目标 | 命令 | 产出 |
|------|------|------|
| 编译 | `make build` | `bin/ride-hailing` |
| 镜像 | `make docker` | 多阶段构建镜像（暴露 8000/9000） |
| 本地起服 | `make run` | 本地验证用 |
| 迁移 | `migrations/` 脚本（幂等） | 库结构 |

- 部署顺序：迁移脚本 → 服务实例 → 冒烟（健康检查 + 一次真实 SSE 会话 + 一次 RAG 检索）。
- 无状态服务，多实例水平扩容；Agent 相关服务与 MCP / RAG 数据服务**分离部署**。
- 配置热更新（注册中心配置中心）：Agent 规约 / prompt 变更无需重启；实例扩容后确认 Kafka 消费组重平衡正常。
- 库对象变更（表/索引/pgvector 扩展）只走 `migrations/` 迁移脚本（幂等），禁止手工 DDL。
