# 订单状态事件契约（Kafka 提案）

> 依据《萝卜快跑开发任务》1.1（Kafka 承载订单状态异步解耦）与 4.2（成员 B 提出事件需求，公共封装由平台负责人评审）。
> 状态：**提案，待成员 F 审核后冻结**；当前代码尚未接入 producer，本文件是评审输入。

## 1. Topic

| Topic | 生产方 | 消费方（规划） | 用途 |
| --- | --- | --- | --- |
| `order.status.changed` | ride-service | 调度（模块三）、车端（模块四）、客服（模块五）、统计（模块六） | 订单状态迁移事实 |
| `order.payment.changed` | ride-service | 客服（模块五）、统计（模块六） | 支付/退款状态收敛 |
| `order.status.changed.dlq` | 平台封装 | 告警值班 | 死信 |

## 2. 消息结构（JSON，字段只增不改）

```json
{
  "event_id": "evt_20261107120000123_ab12",
  "event_type": "order.status.changed",
  "occurred_at": "2026-11-07T12:00:00Z",
  "biz_id": "ord_1024",
  "order_id": 1024,
  "order_no": "AG202611071200000001",
  "passenger_id": 7,
  "from_status": 1,
  "to_status": 2,
  "vehicle_id": 12,
  "service_area_id": 3,
  "reason_code": "DISPATCH_ACCEPT",
  "operator_type": 2,
  "trace_id": "9f1c..."
}
```

必填：`event_id`、`event_type`、`occurred_at`、`biz_id`、`order_id`、`from_status`、`to_status`。
可选：`vehicle_id`、`reason_code`、`operator_type`、`trace_id`。

## 3. 生产与消费约定

1. **key = `order_id`**：保证同一订单的状态事件分区有序。
2. **生产时机**：`trip_status_logs` 落库并提交事务后发送（事务内不发消息）；发送失败进入本地重试队列，不阻塞下单。
3. **消费幂等**：消费方按 `event_id` 去重（Redis SETNX + 24h TTL），并校验 `to_status` 是否大于本地状态，乱序事件直接丢弃。
4. **重试与死信**：最多重试 3 次，指数退避；超限写 `order.status.changed.dlq` 并告警。
5. **禁止**：消费方不得直接改 `trip_orders`，只能调用 ride-service 暴露的服务或事件反向确认。

## 4. 与模块的接口

| 模块 | 关注点 |
| --- | --- |
| 三 调度 | `to_status=1` 触发派单；派单结果回写走 `DISPATCH_ACCEPT` 命令 |
| 四 车端 | `to_status=2/3` 下发接驾/到达指令；`to_status=4` 结束待执行指令 |
| 五 客服 | `to_status=6/7` 生成通知与安全事件关联 |
| 六 统计 | 只读消费，写入聚合指标，不反写业务表 |

## 5. 待评审事项

- Topic 命名是否统一为 `<业务域>.<事件>` 前缀（当前提交 `order.*`）。
- 是否需要 `order.created` 独立事件（当前包含在 `order.status.changed` 的 `from_status=null`）。
- 死信告警渠道与值班归属。
