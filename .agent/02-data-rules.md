# 02 · pgvector(RAG) 规范与 Kafka 契约

> 隶属 AGENT.MD v1.2.1 · 更新日期 2026-10-26
> 本文档只描述 **Kratos Agent 服务**的向量检索与消息契约，独立成套。

## 1. pgvector 使用规范（RAG 检索）

1. 启用：`CREATE EXTENSION IF NOT EXISTS vector;`（迁移脚本内置，幂等）。
2. 向量维度与 embedding 模型对齐（如 `vector(1536)`）；换模型 = 全量重建，须提案。
3. 相似度算子：余弦 `<=>`（默认）、L2 `<->`、内积 `<#>`；同一表混用需注释理由。
4. 数据量 > 1 万行必须建 HNSW 索引
   `CREATE INDEX ON chunks USING hnsw (embedding vector_cosine_ops) WITH (m=16, ef_construction=64);`
5. 检索 topK ≤ 50；先过滤（租户/知识库 ID）后排序，**禁止把全表向量拉回内存算相似度**。
6. GORM 用 `github.com/pgvector/pgvector-go`；批量写入事务 + 批量 INSERT。
7. 向量数据与业务数据分库存放；业务库只存引用 ID（chunk_id / doc_id）。

## 2. Kafka 契约（异步事件）

1. Topic：`<业务域>.<事件>`，全小写点分。初始清单（新增先更新本表并群内确认，再写代码）：

| Topic | 生产方 | 消费方 | 用途 |
|-------|--------|--------|------|
| `agent.chat.completed` | agent 服务 | 后台统计/审计消费组 | 会话完成事件（含 token 用量） |
| `agent.tool.invoked` | agent 服务 | 审计消费组 | MCP 工具调用留痕 |
| `agent.rag.indexed` | 索引任务 | agent 服务 | 知识库向量化完成，可检索 |

2. 消息体：JSON，必含 `event_id`（UUID）、`event_type`、`occurred_at`、`biz_id`；**字段只增不改**，新增字段必须可选。
3. 生产/消费组命名：`<svc>-producer-group` / `<svc>-consumer-group`，禁止共用组名。
4. 消费必须幂等：按 `event_id` 去重（Redis SETNX + TTL 或业务唯一表）。
5. 失败重试有上限，超限进死信 topic（`<topic>.dlq`）并告警，禁止无限重试。
6. 顺序性要求高的场景用同一 `key`（如 `session_id`）保证分区有序。
7. 客户端统一 `segmentio/kafka-go`，封装在 `internal/data/kafka/`，禁止散装使用。
8. **会话上下文走 Redis Memory，不走 Kafka**；Kafka 只承载事件与审计。
