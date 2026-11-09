# 历史负数 ID 兼容修复记录

## 任务状态

测试通过，待用户在已重启的微服务中完成页面验收。

## 问题与根因

后台站点管理页请求历史运营区域 `-1793241578900257898` 时返回 `400 INVALID_STATION_REQUEST`。
测试库已有历史数据使用负数雪花 ID，但模块 3 的运营区域与站点维护用例将所有负数 ID 判为非法，造成页面无法查询该区域下的站点。

## 本次改动

- 运营区域与站点维护逻辑将 ID 校验从“必须大于零”改为“不得等于零”。
- 站点列表保留 `0` 表示全部站点，同时允许按历史负数运营区域 ID 过滤。
- 新增两组回归测试，覆盖负数运营区域/站点 ID 的查询、创建、更新、启停和时段替换。

## 未涉及范围

未修改 proto、HTTP 路由、数据库迁移、派单逻辑和前端请求封装。

## 测试记录

1. 先新增回归用例并执行：两个用例均因旧校验返回领域错误而失败，符合问题复现预期。
2. 完成最小修复后执行：

```powershell
go test ./internal/biz -run 'Test(StationUsecaseSupportsHistoricalNegativeIDs|ServiceAreaAdminUsecaseSupportsHistoricalNegativeIDs)' -count=1 -v
```

结果：通过。

3. 执行领域层全量测试：

```powershell
go test ./internal/biz -count=1
```

结果：通过。

## 验收步骤

重启微服务后，在“运营调度管理 → 站点管理”选择历史运营区域；页面应展示站点或“暂无数据”，且不再提示“模块3服务暂不可用”。
