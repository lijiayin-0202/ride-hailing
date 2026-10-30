# 03 · Git 工作流、提交落地与冲突解决

> 隶属 AGENT.MD v1.3.0 · 更新日期 2026-10-27

## 1. 任务流程概览

```
领任务(任务编号) → 切分支 → 命令生成(make api/generate) → 编码(逐行注释) → 验证(§3)
→ 测试流一轮跑通(AGENT.MD §9) → commit → push → PR → 合并 → 删分支 → 改台账
```

## 2. 分支与 commit 落地

- 分支命名：`feature/T-<需求号>-<短名>`（如 `feature/T-103-chat-stream`）；Bug 修复 `fix/T-<Bug号>-<短名>`。
- 提交信息格式、类型、必填项、示例：**见 `AGENT.MD §5`**，本文件不再复制。
- 完整 Git 命令序列（切分支 → 命令生成 → 编码 → 构建 → 测试 → diff 自查 → commit → push → PR → 合并 → 删分支）**见 `AGENT.MD §6`**，本文件只补充细则。

```bash
# ① 确认不在 master，从最新 master 切分支
git status && git fetch origin
git switch -c feature/T-103-chat-stream origin/master

# ② 命令生成（改 proto 后必须重跑，见 08 §1）
make api && make generate

# ③ 暂存指定文件（禁止 git add .）
git add api/agent/v1/agent.pb.go internal/biz/agent_domain.go internal/service/agent_service.go

# ④ 提交（生成物与业务实现分两次 commit）
git commit -m "[feat] [v2.1.0] [#103] 新增流式对话 gRPC 接口"
git commit -m "[feat] [v2.1.0] [#103] 实现流式对话 Blades 编排逻辑"

# ⑤ 推送
git push -u origin feature/T-103-chat-stream
```

- 每天开工先 `git fetch origin && git rebase origin/master`，冲突在自己分支解决（§5）。
- 禁止 `git add .`、禁止 `git commit -n` / `--no-verify`、禁止删除或跳过失败用例。

## 3. 提交前验证（硬阻断）

**任何 commit 前必须本地验证运行成功；验证不通过禁止提交。**

> 五道关（①真实库 ②本地能跑通 ③Apipost 测通 ④浏览器无报错 ⑤测试流一轮跑通）的权威定义见 `AGENT.MD §9`；本节 L1/L2 对应其中的 ①② 两关。

| 范围 | 必须全绿 |
|------|----------|
| L1 编译与单测 | `make lint`（go vet + gofmt）→ `make test`（含覆盖率） |
| L2 运行验证 | `make run` 起服成功（HTTP :8000 / gRPC :9000）；核心接口实测返回预期 |
| 数据库 | 连真实测试库实际读写删，测试数据 `test_` 前缀并清理 |

- 变更范围与必做项：接口/编排/数据层 → L1+L2+五道关全过；纯文档 → L1 即可。

## 4. PR 与合并

1. PR 描述必填：改动点、影响面（接口/proto/topic/配置）、测试证据（命令输出 + Apipost 截图 + 浏览器截图 + 测试流执行结果）。
2. 模板：
   - 改动点：新增 ChatStream 接口（SSE 流式）
   - 影响面：`api/agent/v1` 新增 rpc；消费方：网关（已群内通知）
   - 验证：五道关证据 + 测试流一轮跑通结果（多轮回归按需自测，非阻断）
3. 1 名 reviewer 批准后方可合并；reviewer 按 `agent/06` 复核。
4. 合并方式：`Rebase and merge`（保持线性历史）。
5. 合并后删除 feature 分支，同步任务台账为"已完成"。

## 5. Git 冲突解决方案（照做，不解就回退）

### 5.1 预防（开工时）

- 每天开工 `git fetch origin && git rebase origin/master`，让分支始终贴近 master。
- 共享文件（`agent/07 §1` 清单）改动前先在群里说一声。

### 5.2 解决步骤

```bash
# ① 拉取并 rebase
git fetch origin && git rebase origin/master

# ② 查看冲突文件
git status          # both modified: path/to/file

# ③ 逐文件打开，只处理冲突区块
#    <<<<<<< HEAD     —— master 上（或对方）的代码
#    =======
#    >>>>>>> feature  —— 你分支的代码

# ④ 按文件类型取舍（见 5.3 策略表），编辑完成后标记
git add path/to/file

# ⑤ 继续或终止
git rebase --continue      # 所有冲突解决后
git rebase --abort         # 解不动时：整体回到 rebase 前的状态（安全）
```

### 5.3 冲突取舍策略

| 文件类型 | 策略 |
|----------|------|
| 生成物：`*.pb.go`、`wire_gen.go`、`buf.lock` | **不手改**：先取 master 版本，冲突解完重新执行 `make api` / `make generate` |
| 共享契约：`api/**/*.proto`、`internal/conf/conf.proto`、`agent/02 §2` topic 表 | 双方新增**都保留**（proto 字段号只追加），解决后群里 @ 相关方确认 |
| 依赖：`go.mod` / `go.sum` | 双方依赖都保留，再跑 `go mod tidy` 重新生成 |
| `configs/config.yaml`、ProviderSet、`internal/server` 注册处 | 以 master 为基础，各自只追加自己的段/行，注释任务号 |
| 业务代码 | 双方逻辑合并；无法机械合并时以数据字典/接口文档为准，必要时 @ 对方一起看 |

### 5.4 冲突解决后的强制验证

- 解决完冲突**必须重跑** `make lint && make test`，全绿才能 `--continue` 并 push。
- 两人协作冲突解不动：`git rebase --abort` → 群里 @ 文件归属人屏幕共享一起解。

### 5.5 冲突处理禁止项

- 禁止 `git push -f` / `--force-with-lease` 到 master 或他人分支。
- 禁止用 `--ours` / `--theirs` 盲选业务代码。
- 禁止解决冲突时顺手改功能、夹带新提交。
- 禁止删掉对方的改动来"消除"冲突（那不叫解决，叫覆盖）。

## 6. 误提交处理

- **业务代码误提交到 master**：立即群内通报 → `git revert` 反向提交（不要 reset 已推送历史）→ 把改动挪到 feature 分支重新走 PR → 复盘。
- **框架白名单文件误走分支**：合并前发现 → 改 PR 标题与 commit 类型（`[opt]`）；已合并不回退，下次注意。

## 7. 提交前自检清单（逐条打勾）

- [ ] 当前分支不是 `master`（或改动命中 `AGENT.MD §4.1` 白名单）
- [ ] 生成物全部由命令重新生成，未手改
- [ ] 逐行注释齐全，改动处注释同步
- [ ] `make lint` / `make test` 全绿
- [ ] 真实库测试通过，测试数据已清理
- [ ] Apipost 用例全绿（含 SSE）；涉及前端时浏览器无报错
- [ ] 测试流已执行一轮、链路跑通无报错（多轮回归按需自测，非阻断）
- [ ] commit 格式符合 `AGENT.MD §5`，一次一个类型
- [ ] 无明文密码、无 `.env`、无 `bin/`、无测试残留
