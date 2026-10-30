[CmdletBinding()]
param(
    [string]$ContainerName = 'module3-postgres-test',
    [int]$HostPort = 55432,
    [switch]$ReuseExistingContainer
)

$ErrorActionPreference = 'Stop'

function Invoke-DockerPsql {
    param([string]$Sql)

    $Sql | docker exec -i $ContainerName psql -v ON_ERROR_STOP=1 -U module3_test -d module3_test
    if ($LASTEXITCODE -ne 0) {
        throw 'PostgreSQL command failed.'
    }
}

# 测试容器只绑定回环地址，避免本机隔离数据库暴露给局域网。
$repositoryRoot = Split-Path -Parent $PSScriptRoot
$schemaPath = Join-Path $PSScriptRoot '0001_module3_operating_dispatch_schema.sql'
$orderForeignKeyPath = Join-Path $PSScriptRoot '0002_module3_dispatch_order_foreign_key.sql'
$stationPath = Join-Path $PSScriptRoot '0003_station_status_and_direction.sql'
$outboxPath = Join-Path $PSScriptRoot '0004_dispatch_outbox.sql'
# 订单状态同步与补派任务依赖前四个迁移创建的派单记录表，必须最后应用。
$orderDispatchPath = Join-Path $PSScriptRoot '0005_order_dispatch_sync_and_reassignment.sql'
$localDirectory = Join-Path $repositoryRoot '.local'
$environmentPath = Join-Path $localDirectory 'module3-test-db.env'

if (-not (Test-Path -LiteralPath $schemaPath) -or -not (Test-Path -LiteralPath $orderForeignKeyPath) -or -not (Test-Path -LiteralPath $stationPath) -or -not (Test-Path -LiteralPath $outboxPath) -or -not (Test-Path -LiteralPath $orderDispatchPath)) {
    throw 'Module 3 migration files are missing; test database was not created.'
}

docker version --format '{{.Server.Version}}' | Out-Null
if ($LASTEXITCODE -ne 0) {
    throw 'Docker Engine is unavailable. Confirm that docker ps succeeds in this PowerShell session.'
}

$existingContainer = docker ps -a --filter "name=^/$ContainerName$" --format '{{.Names}}'
# 默认拒绝复用已有容器，以免误在仍含数据的隔离库上重复执行非幂等迁移。
if ($existingContainer -eq $ContainerName -and -not $ReuseExistingContainer) {
    throw "The test container $ContainerName already exists. The script will not overwrite it; rerun with -ReuseExistingContainer only when it is the empty test container created by this script."
}

if ($existingContainer -ne $ContainerName) {
    New-Item -ItemType Directory -Path $localDirectory -Force | Out-Null
    # 随机凭据仅写入 Git 忽略的本地环境文件，脚本不在输出中暴露它。
    $randomBytes = New-Object byte[] 24
    [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($randomBytes)
    $databasePassword = [Convert]::ToBase64String($randomBytes).Replace('+', 'A').Replace('/', 'B').Replace('=', 'C')
    @"
POSTGRES_DB=module3_test
POSTGRES_USER=module3_test
POSTGRES_PASSWORD=$databasePassword
"@ | Set-Content -LiteralPath $environmentPath -Encoding ascii

    docker run --name $ContainerName --detach --restart no --env-file $environmentPath --publish "127.0.0.1:${HostPort}:5432" --health-cmd 'pg_isready -U module3_test -d module3_test' --health-interval 5s --health-timeout 3s --health-retries 12 postgres:16-alpine | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw 'Unable to create the isolated PostgreSQL container.'
    }
}
else {
    docker start $ContainerName | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw 'Unable to start the existing isolated PostgreSQL container.'
    }
}

for ($attempt = 1; $attempt -le 12; $attempt++) {
    $health = docker inspect --format '{{.State.Health.Status}}' $ContainerName
    if ($health -eq 'healthy') { break }
    Start-Sleep -Seconds 5
}
if ($health -ne 'healthy') {
    throw 'PostgreSQL container did not pass its health check within 60 seconds.'
}

Invoke-DockerPsql -Sql (Get-Content -Raw -LiteralPath $schemaPath)
Invoke-DockerPsql -Sql 'CREATE TABLE IF NOT EXISTS public.trip_orders (id BIGINT PRIMARY KEY);'
Invoke-DockerPsql -Sql (Get-Content -Raw -LiteralPath $orderForeignKeyPath)
Invoke-DockerPsql -Sql (Get-Content -Raw -LiteralPath $stationPath)
Invoke-DockerPsql -Sql (Get-Content -Raw -LiteralPath $outboxPath)
Invoke-DockerPsql -Sql (Get-Content -Raw -LiteralPath $orderDispatchPath)

$verificationSql = @'
DO $$
DECLARE
    module3_table_count INTEGER;
    order_foreign_key_count INTEGER;
    required_index_count INTEGER;
BEGIN
    SELECT count(*) INTO module3_table_count
    FROM pg_tables
    WHERE schemaname = 'public'
      AND tablename IN ('service_areas', 'service_area_schedules', 'stations', 'vehicles', 'vehicle_status_logs', 'dispatch_records', 'order_dispatch_outbox', 'dispatch_reassignment_jobs');

    SELECT count(*) INTO order_foreign_key_count
    FROM pg_constraint
    WHERE conname = 'fk_dispatch_records_order';

    SELECT count(*) INTO required_index_count
    FROM pg_indexes
    WHERE schemaname = 'public'
      AND indexname IN ('idx_vehicles_dispatch_candidate', 'idx_vehicle_status_logs_vehicle_reported_at', 'idx_dispatch_records_order_created_at', 'idx_order_dispatch_outbox_ready', 'idx_order_dispatch_outbox_lease', 'idx_dispatch_reassignment_jobs_ready', 'idx_dispatch_reassignment_jobs_lease');

    IF module3_table_count <> 8 OR order_foreign_key_count <> 1 OR required_index_count <> 7 THEN
        RAISE EXCEPTION 'Migration verification failed: tables=%, order_fk=%, required_indexes=%', module3_table_count, order_foreign_key_count, required_index_count;
    END IF;
END
$$;

SELECT
    (SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tablename IN ('service_areas', 'service_area_schedules', 'stations', 'vehicles', 'vehicle_status_logs', 'dispatch_records', 'order_dispatch_outbox', 'dispatch_reassignment_jobs')) AS module3_table_count,
    (SELECT count(*) FROM pg_constraint WHERE conname = 'fk_dispatch_records_order') AS order_foreign_key_count,
    (SELECT count(*) FROM pg_indexes WHERE schemaname = 'public' AND indexname IN ('idx_vehicles_dispatch_candidate', 'idx_vehicle_status_logs_vehicle_reported_at', 'idx_dispatch_records_order_created_at', 'idx_order_dispatch_outbox_ready', 'idx_order_dispatch_outbox_lease', 'idx_dispatch_reassignment_jobs_ready', 'idx_dispatch_reassignment_jobs_lease')) AS required_index_count;
'@
Invoke-DockerPsql $verificationSql
Write-Output "Isolated database created and migration verified: container=$ContainerName, address=127.0.0.1:$HostPort, database=module3_test."
Write-Output 'The random local password is stored only in the ignored .local/module3-test-db.env and is not printed.'
