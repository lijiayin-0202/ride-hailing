# 订单状态机（模块二）

> 代码事实来源：`internal/biz/state_machine.go`（迁移表）与 `internal/biz/ride_rules.go`（展示映射）。
> 数据库约束：`trip_orders.ck_trip_orders_status`（0–7）、`order_commands.ck_order_commands_type`。

## 1. 状态枚举

| 值 | 状态码 | 中文 | 说明 |
| --- | --- | --- | --- |
| 0 | `PENDING_DISPATCH` | 待派车 | 订单已创建，等待调度派单 |
| 1 | `DISPATCHING` | 派车中 | 调度已发起派单尝试 |
| 2 | `PICKING_UP` | 接驾中 | 车辆已接单，正在前往上车点 |
| 3 | `WAITING_BOARDING` | 待上车 | 车辆已到达，等待核验上车 |
| 4 | `IN_TRIP` | 行程中 | 已核验上车，行程进行中 |
| 5 | `COMPLETED` | 已完成 | 终态，可支付、可评价 |
| 6 | `CANCELLED` | 已取消 | 终态，可退优惠券/退款 |
| 7 | `EXCEPTION_CLOSED` | 异常关闭 | 终态，故障/安全事件导致的关闭 |

## 2. 迁移图

```text
                DISPATCH            DISPATCH_ACCEPT        MARK_ARRIVED
  [0 待派车] ─────────────▶ [1 派车中] ─────────────▶ [2 接驾中] ─────────▶ [3 待上车]
      │  ▲                     │                          │                   │
      │  └──── 派单失败回退 ────┘                          │        UNLOCK_AND_START / START_TRIP
      │                                                     │                   ▼
      │                                                     │              [4 行程中]
      │                                                     │                   │
      │                                                    CANCEL        COMPLETE_TRIP
      │                                                     │                   ▼
      │                                                     ▼              [5 已完成]
      └────────────────── CANCEL ──────────────────────▶ [6 已取消]
                                                            ▲
      任意非终态 ───────────── CLOSE_EXCEPTION ────────────┴──▶ [7 异常关闭]
```

## 3. 迁移表

| from | 允许的 to |
| --- | --- |
| 0 待派车 | 1 派车中、6 已取消、7 异常关闭 |
| 1 派车中 | 0 待派车（派单失败回退）、2 接驾中、6 已取消、7 异常关闭 |
| 2 接驾中 | 3 待上车、6 已取消、7 异常关闭 |
| 3 待上车 | 4 行程中、6 已取消、7 异常关闭 |
| 4 行程中 | 5 已完成、7 异常关闭 |
| 5 / 6 / 7 | 终态，拒绝任何迁移 |

非法迁移统一返回 `BIZ_CONFLICT`（HTTP 409），不会落库。

## 4. 命令与权限

| 命令 | 目标状态 | 允许调用方 |
| --- | --- | --- |
| `UNLOCK_AND_START` | 3 → 4 | 乘客端（需 4 位上车码） |
| `REQUEST_PULL_OVER` | 4 → 4 | 乘客端 |
| `CANCEL` | 0/1/2/3 → 6 | 仅 `POST /orders/{id}/cancel`，不走命令接口 |
| `DISPATCH` | 0 → 1 | 调度（模块三） |
| `DISPATCH_ACCEPT` | 1 → 2 | 调度/车端（模块三/四） |
| `MARK_ARRIVED` | 2 → 3 | 车端（模块四） |
| `START_TRIP` | 3 → 4 | 车端（模块四） |
| `COMPLETE_TRIP` | 4 → 5 | 车端（模块四） |
| `CLOSE_EXCEPTION` | 非终态 → 7 | 调度/客服（内部） |

内部命令必须携带 `X-Internal-Token`（配置项 `data.security.internal_token`；未配置时本地开发放行）。

## 5. 并发与幂等

1. 状态迁移统一走 `UPDATE ... WHERE id = ? AND status = 当前状态`，受影响行数不等于 1 即判定并发冲突（409）。
2. 创建订单靠 `uq_trip_orders_passenger_idempotency (passenger_id, idempotency_key)` 与活动订单部分唯一索引兜底。
3. 命令靠 `uq_order_commands_passenger_idempotency` 去重，重复请求返回首次结果。
4. 评价靠 `uq_trip_reviews_order` 保证一单一次。
5. 支付回调按 `payments.payment_no` + `payment_status` 幂等，重复 SUCCESS 回调返回 `duplicated=true` 且不重复记账。

## 6. 状态流水

`trip_status_logs` 每次迁移写一行：`previous_status`、`current_status`、`change_source`、`reason_code`、`operator_type`、`operator_id`、`description`。

`operator_type`：1 乘客 / 2 调度 / 3 车端 / 4 客服 / 5 系统。
