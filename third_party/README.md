# third_party

本目录用于放置「从外部仓库引入、但不参与本项目编译」的 proto 依赖（如 `google/api/annotations.proto`）。

当前 ride-hailing 项目改用 **buf 远程依赖** 管理第三方 proto，已声明在 `../buf.yaml`：

```yaml
deps:
  - buf.build/googleapis/googleapis
```

执行 `buf dep update` 后会在 `../buf.lock` 中锁定版本，生成时由 buf 自动从本地缓存解析，
因此本目录无需再手工拷贝 proto 文件。

如果需要离线（无网络）生成代码，再把对应 proto 拷贝进来，并把 `buf.yaml` 的 `deps` 去掉即可。
