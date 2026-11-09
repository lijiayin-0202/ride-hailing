# 订单主链路时序（模块二 T0）

```text
乘客端            网关/ride-hailing        ride-service(biz/data)      调度(模块三)      车端(模块四)        支付渠道
  │                      │                        │                       │                │                 │
  ├─ POST /fare-options/estimate ─────────────────▶│ 校验站点/区域            │                │                 │
  │                      │                        ├─ ResolveServiceArea ──▶ stations/areas │                │
  │                      │                        ├─ ListServiceTypes ────▶ pricing_rules  │                │
  │                      │                        ├─ CountAvailableVehicles▶ vehicles       │                │
  │                      │                        ├─ 优惠券校验 + 计费                     │                │
  │                      │                        └─ 写 fare_quote_*（2 分钟有效）        │                │
  │◀── quote_token / 车型方案 / 可用车数 ──────────┤                       │                │                 │
  │                      │                        │                       │                │                 │
  ├─ POST /orders (X-Idempotency-Key) ───────────▶│                       │                │                 │
  │                      │                        ├─ 校验 quote 归属/过期   │                │                 │
  │                      │                        ├─ 活动订单排他检查       │                │                 │
  │                      │                        ├─ 锁定优惠券 0→1        │                │                 │
  │                      │                        ├─ 订单快照 + 费用明细 + 状态流水(0)      │                 │
  │◀── order_id / status=PENDING_DISPATCH ─────────┤                       │                │                 │
  │                      │                        │                       │                │                 │
  │                      │◀── DISPATCH（内部令牌）──────────────────────────┤                │                 │
  │                      │                        ├─ 0→1 派车中 + 派单流水  │                │                 │
  │                      │                        │                       ├─ 候选车筛选 ───▶│                 │
  │                      │◀── DISPATCH_ACCEPT ────────────────────────────┼────────────────┤                 │
  │                      │                        ├─ 1→2 接驾中 + 车辆绑定 │                │                 │
  │                      │                        │◀── telemetry 位置上报 ──────────────────┤                 │
  │                      │◀── MARK_ARRIVED ───────────────────────────────┼────────────────┤                 │
  │                      │                        ├─ 2→3 待上车            │                │                 │
  │                      │                        │◀── boarding_verification───────────────┤                 │
  │                      │                        ├─ 3→4 行程中（校验上车码哈希）           │                 │
  ├─ GET /orders/{id}/live-view ─────────────────▶│                       │                │                 │
  │◀── 订单 + 车辆 + 最新位置 + 派单 + 核验 + 安全事件 ─┤                      │                │                 │
  │                      │                        │◀── COMPLETE_TRIP ─────────────────────┼─────────────────┤
  │                      │                        ├─ 4→5 已完成            │                │                 │
  │                      │                        │                       │                │                 │
  ├─ GET /orders/{id}/settlement-view ───────────▶│ 订单 + 费用明细 + 支付 + 评价状态       │                 │
  ├─ POST /orders/{id}/payments ─────────────────▶│ 创建支付单(PENDING)    │                │                 │
  ├─ POST /payments/callback（渠道）──────────────▶│ 回调收敛 → PAID / FAILED（幂等）       │                 │
  ├─ POST /orders/{id}/reviews ──────────────────▶│ 4→5 且未评价时落库      │                │                 │
  │                      │                        │                       │                │                 │
  ├─ POST /orders/{id}/cancel ───────────────────▶│ 取消费用规则 + 释放优惠券 + 退款流水     │                 │
```

## 取消与退款

```text
乘客 POST /orders/{id}/cancel
  → biz.CancelFee(当前状态, 应付金额)         // 0/1 免费；2 收 5 元；3 收 10 元；4 及以上 409
  → data.PaidAmount(订单)                    // payments 中 PAID/部分退款/已退款求和
  → refund = max(0, paid - fee)
  → 单事务：trip_orders(status=6) + passenger_coupons(1→0) + trip_status_logs
            + order_cancellations + refunds(如有退款) + payments.payment_status(5/6)
```

重复取消：`order_cancellations.order_id` 唯一 + 状态已是 6 时返回首次结果（幂等）。
