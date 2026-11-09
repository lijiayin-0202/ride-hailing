# 微服务配置启动修复记录

- 状态：开发完成，测试通过，待数据库环境恢复后联调验收
- 日期：2026-11-08

## 现象

使用 `go run ./cmd/ride-hailing -conf .\\configs` 启动时，程序会把 `local.yaml.example` 也当作配置文件读取，随后因为文件扩展名为 `example` 而启动失败。

## 根因

Kratos 默认目录配置源会读取目录内所有非隐藏文件，不能自动忽略示例文件。

## 修改

- 新增 `configSources`，目录模式下仅按顺序加载 `config.yaml` 和可选的 `local.yaml`。
- 命令传入单个配置文件时保持原有单文件加载方式。
- 新增测试，覆盖“目录中有 `local.yaml.example`”和“单文件配置”两种场景。

## 验证

- `go test ./cmd/ride-hailing -run TestConfigSources -count=1`。
- 启动日志已验证不再出现 `unsupported key: local.yaml.example`。

## 当前联调阻塞

本机 PostgreSQL 测试端口 `55432` 未启动，微服务虽可监听 8000/9000 端口，但无法访问本地测试库；Docker Desktop 的本机引擎当前不可用。数据库恢复后可继续完成真实接口联调。
