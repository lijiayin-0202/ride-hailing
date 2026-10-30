$ErrorActionPreference = 'Stop'

# 这是静态防护：确认迁移保留关键对象，同时拒绝危险的建表策略关键字。

$migrationRoot = $PSScriptRoot
$schemaPath = Join-Path $migrationRoot '0001_module3_operating_dispatch_schema.sql'
$foreignKeyPath = Join-Path $migrationRoot '0002_module3_dispatch_order_foreign_key.sql'
$testDatabaseStartupPath = Join-Path $migrationRoot 'start_module3_test_database.ps1'

if (-not (Test-Path -LiteralPath $schemaPath)) {
    throw "缺少基础迁移脚本: $schemaPath"
}

if (-not (Test-Path -LiteralPath $foreignKeyPath)) {
    throw "缺少订单外键迁移脚本: $foreignKeyPath"
}

if (-not (Test-Path -LiteralPath $testDatabaseStartupPath)) {
    throw "缺少隔离库启动脚本: $testDatabaseStartupPath"
}

$schema = Get-Content -Raw -LiteralPath $schemaPath -Encoding utf8
$foreignKey = Get-Content -Raw -LiteralPath $foreignKeyPath -Encoding utf8
$testDatabaseStartup = Get-Content -Raw -LiteralPath $testDatabaseStartupPath -Encoding utf8

$requiredSchemaFragments = @(
    'BEGIN;',
    'COMMIT;',
    'CREATE TABLE public.service_areas',
    'CREATE TABLE public.service_area_schedules',
    'CREATE TABLE public.stations',
    'CREATE TABLE public.vehicles',
    'CREATE TABLE public.vehicle_status_logs',
    'CREATE TABLE public.dispatch_records',
    'dispatch_records_order_attempt_key',
    'ux_dispatch_records_order_active',
    'idx_vehicles_dispatch_candidate',
    'idx_vehicle_status_logs_vehicle_reported_at',
    'idx_dispatch_records_order_created_at'
)

foreach ($fragment in $requiredSchemaFragments) {
    if ($schema -notmatch [regex]::Escape($fragment)) {
        throw "基础迁移缺少: $fragment"
    }
}

if ($schema -match '(?i)\\b(serial|identity|drop\\s+table)\\b') {
    throw '基础迁移包含禁止关键字。'
}

if ($foreignKey -notmatch 'fk_dispatch_records_order') {
    throw '订单外键迁移缺少 fk_dispatch_records_order。'
}

if ($foreignKey -notmatch '(?i)to_regclass') {
    throw '订单外键迁移缺少 trip_orders 存在性检查。'
}

# 冷启动必须覆盖补派相关迁移，否则运行期测试会掩盖缺表问题。
$requiredStartupFragments = @(
    '0005_order_dispatch_sync_and_reassignment.sql',
    'Get-Content -Raw -LiteralPath $orderDispatchPath',
    'order_dispatch_outbox',
    'dispatch_reassignment_jobs',
    'idx_order_dispatch_outbox_ready',
    'idx_dispatch_reassignment_jobs_ready'
)

foreach ($fragment in $requiredStartupFragments) {
    if ($testDatabaseStartup -notmatch [regex]::Escape($fragment)) {
        throw "隔离库启动脚本未覆盖补派迁移对象: $fragment"
    }
}

Write-Output '模块 3 迁移静态校验通过。'
