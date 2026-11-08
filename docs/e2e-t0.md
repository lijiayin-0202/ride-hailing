# T0 端到端联调脚本

试算 → 下单 → 派车 → 接单 → 到达 → 核验上车 → 行程 → 完成 → 支付 → 回调 → 结算 → 评价，附失败回放。

依赖：`curl`、`jq`。服务默认 `http://127.0.0.1:8000`。

> 说明：`city_code` 必须与 `vehicle_service_types.city_code` 一致（当前演示数据为 `420100`）。
> 服务连接线上库时需带 `search_path=robotaxi`，并通过 `RIDE_DATA_DATABASE_SOURCE`、`RIDE_DATA_SECURITY_CONTACT_PHONE_KEY`、`RIDE_DATA_SECURITY_INTERNAL_TOKEN` 注入配置。

```bash
export BASE=${BASE:-http://127.0.0.1:8000}
export PASSENGER=7                 # 乘客 ID（网关注入 X-Passenger-ID）
export INTERNAL=${INTERNAL:-}      # 服务端 data.security.internal_token；本地未配置可留空
export OTHER_PASSENGER=8           # 越权校验用

api()  { curl -sS "$@"; }
jwt()  { jq -r "$1"; }

# 0. 试算
QUOTE=$(api -X POST "$BASE/api/v1/passenger/fare-options/estimate" \
  -H 'Content-Type: application/json' -H "X-Passenger-ID: $PASSENGER" \
  -d '{"city_code":"420100","ride_mode":"NOW",
       "origin":{"place_id":"ST_DEMO_001","name":"软件园","longitude":118.296,"latitude":33.963},
       "destination":{"place_id":"ST_DEMO_002","name":"宿迁站","longitude":118.276,"latitude":33.92}}')
echo "$QUOTE" | jq '{quote_token, expires_at, quotes: [.quotes[] | {service_type_id, pickup_eta_seconds, available_vehicle_count, payable: .fare.payable_amount}]}'
TOKEN=$(echo "$QUOTE" | jwt '.quote_token')
SERVICE=$(echo "$QUOTE" | jwt '.quotes[0].service_type_id')

# 1. 下单（记录幂等键用于重复下单回放）
IDEMPOTENCY="e2e-$(date +%s)"
ORDER=$(api -X POST "$BASE/api/v1/passenger/orders" \
  -H 'Content-Type: application/json' -H "X-Passenger-ID: $PASSENGER" -H "X-Idempotency-Key: $IDEMPOTENCY" \
  -d "{\"quote_token\":\"$TOKEN\",\"service_type_id\":\"$SERVICE\",\"passenger_count\":1,
       \"contact_phone\":\"13812345678\",\"user_agreement_version\":\"2026-11\"}")
echo "$ORDER" | jq '{order_id, order_no, status}'
ORDER_ID=$(echo "$ORDER" | jwt '.order_id')
```

```bash
# 2. 调度/车端推进（内部命令，需 X-Internal-Token）
cmd() { api -X POST "$BASE/api/v1/passenger/orders/$ORDER_ID/commands" \
  -H 'Content-Type: application/json' -H "X-Passenger-ID: $PASSENGER" \
  -H "X-Internal-Token: $INTERNAL" -H "X-Idempotency-Key: cmd-$1-$(date +%s%N)" \
  -d "{\"command_type\":\"$1\"$2}"; }

cmd DISPATCH        | jq '{order_status, status_text}'   # 0 → 1 派车中
cmd DISPATCH_ACCEPT | jq '{order_status, status_text}'   # 1 → 2 接驾中
cmd MARK_ARRIVED    | jq '{order_status, status_text}'   # 2 → 3 待上车

# 3. 聚合详情（拿到上车码后缀）
LIVE=$(api "$BASE/api/v1/passenger/orders/$ORDER_ID/live-view" -H "X-Passenger-ID: $PASSENGER")
echo "$LIVE" | jq '{status, dispatch, boarding, location, trip_progress}'
CODE=$(echo "$LIVE" | jwt '.pickup.boarding_code_suffix')

# 4. 核验上车（3 → 4）
api -X POST "$BASE/api/v1/passenger/orders/$ORDER_ID/commands" \
  -H 'Content-Type: application/json' -H "X-Passenger-ID: $PASSENGER" \
  -H "X-Idempotency-Key: unlock-$(date +%s%N)" \
  -d "{\"command_type\":\"UNLOCK_AND_START\",\"boarding_code\":\"$CODE\"}" | jq '{order_status}'

# 5. 完成行程（4 → 5）
cmd COMPLETE_TRIP | jq '{order_status, status_text}'

# 6. 支付单 + 渠道回调
PAY=$(api -X POST "$BASE/api/v1/passenger/orders/$ORDER_ID/payments" \
  -H 'Content-Type: application/json' -H "X-Passenger-ID: $PASSENGER" -H "X-Idempotency-Key: pay-$(date +%s)" \
  -d '{"payment_method":"WECHAT"}')
echo "$PAY" | jq '{payment_no, amount, payment_status}'
PAY_NO=$(echo "$PAY" | jwt '.payment_no')
PAY_AMOUNT=$(echo "$PAY" | jwt '.amount')

api -X POST "$BASE/api/v1/payments/callback" \
  -H 'Content-Type: application/json' -H "X-Internal-Token: $INTERNAL" \
  -d "{\"payment_no\":\"$PAY_NO\",\"channel_trade_no\":\"wx-e2e-1\",\"result\":\"SUCCESS\",\"amount\":$PAY_AMOUNT}" \
  | jq '{payment_status, duplicated}'

# 7. 结算视图 + 评价
api "$BASE/api/v1/passenger/orders/$ORDER_ID/settlement-view" -H "X-Passenger-ID: $PASSENGER" \
  | jq '{payable_amount, paid_amount, refunded_amount, fee_items, payment, review}'

api -X POST "$BASE/api/v1/passenger/orders/$ORDER_ID/reviews" \
  -H 'Content-Type: application/json' -H "X-Passenger-ID: $PASSENGER" -H "X-Idempotency-Key: review-$(date +%s)" \
  -d '{"rating":5,"tag_codes":["SAFE","CLEAN"],"comment":"体验很好","anonymous":true}' | jq .
```

## 失败回放

```bash
# A. 重复下单：同一 X-Idempotency-Key 返回同一订单，不产生第二张单
api -X POST "$BASE/api/v1/passenger/orders" \
  -H 'Content-Type: application/json' -H "X-Passenger-ID: $PASSENGER" -H "X-Idempotency-Key: $IDEMPOTENCY" \
  -d "{\"quote_token\":\"$TOKEN\",\"service_type_id\":\"$SERVICE\",\"passenger_count\":1,
       \"contact_phone\":\"13812345678\",\"user_agreement_version\":\"2026-11\"}" | jq '.order_id'
# 期望：与第一次 order_id 相同（或 409 活动订单冲突）

# B. 非法状态迁移：已完成订单再派单 → 409 BIZ_CONFLICT
cmd DISPATCH | jq '{code, reason}'

# C. 重复支付回调 → duplicated=true，金额不重复记账
api -X POST "$BASE/api/v1/payments/callback" \
  -H 'Content-Type: application/json' -H "X-Internal-Token: $INTERNAL" \
  -d "{\"payment_no\":\"$PAY_NO\",\"channel_trade_no\":\"wx-e2e-1\",\"result\":\"SUCCESS\",\"amount\":$PAY_AMOUNT}" \
  | jq '{payment_status, duplicated}'

# D. 重复评价 → 409
api -X POST "$BASE/api/v1/passenger/orders/$ORDER_ID/reviews" \
  -H 'Content-Type: application/json' -H "X-Passenger-ID: $PASSENGER" -H "X-Idempotency-Key: review-dup-$(date +%s)" \
  -d '{"rating":4,"tag_codes":["CLEAN"]}' | jq '{code, reason}'

# E. 越权查单：他人乘客读同一订单 → 404
api "$BASE/api/v1/passenger/orders/$ORDER_ID/live-view" -H "X-Passenger-ID: $OTHER_PASSENGER" | jq '{code, reason}'

# F. 行程中取消 → 409（取消费用规则不允许）
#    先创建第二张单并推进到 IN_TRIP，再调用：
api -X POST "$BASE/api/v1/passenger/orders/$ORDER_ID/cancel" \
  -H 'Content-Type: application/json' -H "X-Passenger-ID: $PASSENGER" \
  -d '{"reason_code":"PASSENGER_CANCEL"}' | jq '{code, reason}'

# G. 取消计费：待上车(3)取消 → 违约金 10 元，未支付则 refund_amount=0
```

## SSE 行程事件

```bash
curl -N "$BASE/api/v1/passenger/orders/$ORDER_ID/events" \
  -H "X-Passenger-ID: $PASSENGER" -H "Last-Event-ID: 0"
```
