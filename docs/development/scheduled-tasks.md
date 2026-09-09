# 定时任务接入与运维指南

> 本文面向从本仓库拉取代码后，需要新增定时任务、注册业务方法或接入 HTTP 回调的开发者。
> 任务定义、执行记录和执行日志均来自数据库与队列，页面不包含示例数据或前端计算的假数据。

## 1. 功能边界

定时任务由四层组成：

```text
contracts/openapi/admin-v1.yaml
        ↓
transport/http/tasks → application/tasks → domain/task
                                      ↑
                        platform/tasks (GORM/Redis)
```

- **任务定义**：保存名称、cron、时区、执行器类型、参数和启用状态。
- **调度器**：按任务时区计算到期时间，并用幂等键入队；服务端返回 `nextRunAt`，前端不自行推算。
- **执行器**：只允许显式注册的方法键或 HTTP 请求配置，不执行任务参数中的代码、命令或脚本。
- **运行记录**：保存触发来源、执行器、开始/结束时间、耗时、结果摘要、脱敏输出和稳定错误码。

## 2. 数据库与启动配置

任务相关表为：

- `gvba_task_definitions`：任务定义及执行器配置；
- `gvba_task_runs`：每次执行的状态、幂等键和结果摘要；
- `gvba_task_run_logs`：按 attempt 追加的诊断日志。

生产环境必须启用数据库持久化。未配置数据库时的内存 repository 仅用于单元测试和本地开发，进程重启后数据会丢失，不能作为生产任务存储。

启动顺序：先执行项目迁移，再启动 API、worker 和 scheduler。任务增量字段使用显式版本迁移：调用 migration runner 的 `ApplyTaskRunExecutionMetadata`（回滚调用 `RevertTaskRunExecutionMetadata`），对应版本为 `v005_task_run_execution_metadata`；不要用启动时 AutoMigrate 代替它。新增字段均有默认值或可为空，旧记录可直接读取。生产应使用数据库 repository；无数据库时的 memory repository 只适用于单进程开发/测试，重启会丢失定义与运行记录。

当配置 Redis 时，scheduler 使用 Redis lease 保证多实例只有一个节点执行同一时间片；未配置 Redis 时保持单进程调度语义。多实例部署应启用 Redis，并为 lease 设置大于单次 tick 的 TTL。

## 3. 注册业务方法执行器

业务代码在 bootstrap 组合根显式注册稳定的方法键。当前注册端口的真实签名是 `RegisteredMethod(context.Context, json.RawMessage) (ExecutionResult, error)`；方法键来自持久化的 `methodKey`，不是从 payload 动态解析函数名、SQL 或 shell。

```go
type MethodRegistry interface {
    Register(key string, method tasks.RegisteredMethod) error
}

func registerTaskMethods(reg MethodRegistry, orders OrderService) error {
    return reg.Register("orders.reconcile", func(ctx context.Context, payload json.RawMessage) (tasks.ExecutionResult, error) {
        var input struct { Days int `json:"days"` }
        if err := json.Unmarshal(payload, &input); err != nil {
            return tasks.ExecutionResult{}, fmt.Errorf("invalid payload: %w", err)
        }
        if input.Days <= 0 { input.Days = 1 }
        if err := orders.Reconcile(ctx, input.Days); err != nil { return tasks.ExecutionResult{}, err }
        return tasks.ExecutionResult{ResultSummary: "orders.reconcile completed"}, nil
    })
}
```

任务定义只保存 `executorType=registered` 和 `methodKey=orders.reconcile`。未知键会在创建或执行时返回稳定错误码并写入运行日志；不会静默成功。

## 4. HTTP 执行器

HTTP 任务字段包括 `url`、`method`、`headers`、`body`、`timeoutSeconds` 和 `allowInternal`。默认仅允许公网 HTTP(S) 目标；loopback、RFC1918、link-local、未解析地址和重定向到内网均被拒绝。只有具备管理权限的操作者显式开启 `allowInternal` 后才允许内网目标，且该变更写入审计日志。

响应状态码 `2xx` 视为成功，其余状态码为失败；超时或取消保留稳定错误码。PublicDefinition/运行日志的 HTTP `body` 非空时整体显示为 `[REDACTED]`；URL 中带敏感 query 参数时整体掩码，不能仅删除参数名。编辑已有定义时服务端保留原始配置用于执行，但响应仍返回脱敏值；普通 JSON 输入字段不因脱敏策略被截断，只有运行输出有长度上限。`allowInternal` 必须由任务配置和执行器策略同时显式开启；默认拒绝内网解析地址。管理 API 仍执行现有管理员鉴权、租户隔离和审计约束，不应把该字段当作绕过权限的开关。

```json
{
  "name": "汇率同步",
  "type": "http",
  "cron": "0 */30 * * * *",
  "timezone": "Asia/Shanghai",
  "executorType": "http",
  "http": {
    "url": "https://HOST/api/rates",
    "method": "GET",
    "headers": { "Authorization": "Bearer TOKEN" },
    "allowInternal": false,
    "timeoutSeconds": 15
  },
  "enabled": true
}
```

`timeoutSeconds` 顶层任务字段是任务超时的唯一权威配置；HTTP 配置对象中的同名展示值不能覆盖它。HTTP 请求体和响应输出按上文规则处理。仓库 bootstrap 默认提供空的注册方法目录（没有 seed 任务）；业务模块必须在组合根显式注册自己的 `methodKey`。

## 5. 迁移、启动与回滚

新装数据库使用 fresh-install `up` 创建完整模型；已有数据库先完成 fresh schema，再执行增量任务字段迁移。CLI 当前接受的动作名称为 `tasks-upgrade` 和 `tasks-rollback`：

```bash
go -C server run ./cmd/migrate --config /path/to/config.yaml tasks-upgrade
go -C server run ./cmd/migrate --config /path/to/config.yaml tasks-rollback
```

`tasks-rollback` 会删除 v005 新增字段，执行前必须停 API/worker、备份数据库并确认没有依赖这些字段的运行实例；不要把它当作无损回滚。没有可用 MySQL/Redis 时，本地只能完成静态检查或 memory 测试，本文不将其记为真实基础设施联调通过。

## 6. 调度、状态与并发语义

- cron 支持 5、6、7 段表达式及 `@hourly`、`@daily` 别名；时区由任务定义指定。
- 服务端负责 `nextRunAt` 和五年预览边界；不对错过的时间片补跑。
- `payloadSchema` 只要求 JSON object，不等同于完整 JSON Schema 校验器。
- `maxAttempts` 表示一次运行允许的总尝试次数（默认 3），不是失败次数之外再加 3 次。
- 状态持久化枚举为 `pending`、`running`、`succeeded`、`failed`、`dead_letter`、`cancelled`；`timeout`、`skipped` 只作为查询/UI 衍生标签，不写入原始状态枚举。
- 调度器按任务幂等键入队；`forbid`、`allow`、`replace` 并发策略在服务端生效。worker 上下文取消会停止协作中的执行，超时/取消写入稳定错误码。
- `taskName`、`taskDescription`、`configSnapshot` 用于运行历史审计；`lastRun*` 和下次执行时间由服务端计算，前端不自行推导。
- 多租户调度必须使用真实 tenant/org scope；当前未配置 Redis 时仅保证单进程调度语义，多实例生产部署需要数据库和 Redis。

## 7. 管理 API

API 前缀为 `/api/admin/v1/tasks`，遵循现有管理员鉴权、租户上下文、错误结构和分页结构：

管理页面 canonical 路径为 `/system/tasks`；历史 `/ops/tasks` 深链只做重定向。访问需要 `ops:tasks:read`，创建、编辑、删除、运行、取消和重试需要 `ops:tasks:manage`；租户和组织 scope 在服务端强制校验。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/tasks` | 按名称、执行器、状态分页查询任务；返回 `nextRunAt` |
| POST | `/tasks` | 创建任务定义 |
| PATCH | `/tasks/{id}` | 更新任务定义 |
| DELETE | `/tasks/{id}` | 软删除任务 |
| POST | `/tasks/{id}/run` | 确认后手动执行，支持幂等键 |
| GET | `/tasks/runs` | 跨任务分页查询执行日志 |
| GET | `/tasks/methods` | 查询 bootstrap 已注册的方法目录 |
| POST | `/tasks/preview` | 计算 cron 未来执行时间（服务端时区与五年边界） |
| GET | `/tasks/{id}/runs` | 查询单任务运行记录 |
| GET | `/tasks/{id}/runs/{runId}/logs` | 查询运行详情和脱敏日志 |
| POST | `/tasks/{id}/runs/{runId}/cancel` | 取消待执行/执行中的记录 |
| POST | `/tasks/{id}/runs/{runId}/retry` | 为失败记录生成新的幂等键并重试 |

分页参数统一使用 `page`、`pageSize`；响应包含 `items`、`total`、`page`、`pageSize`。UI 的任务列表和执行日志 Tab 均直接消费这些接口，加载、空态、错误和重复提交状态由页面处理。

## 8. 前端接入

三套管理端（`web-antd`、`web-ele`、`web-naive`）共享同一 API 语义。新增业务页面时：

1. 从 `admin/packages/api-client` 的生成客户端调用任务 API，不在组件中拼接服务端内部路径；
2. 任务创建表单根据 `executorType` 展示 registered/http 字段；
3. 执行日志详情展示 `triggerSource`、`executorType`、状态、时间、耗时、摘要和脱敏输出；
4. 通过 `nextRunAt` 展示下次执行时间，不在浏览器重新解析 cron；
5. 所有 mutation 使用请求进行中状态防止重复提交，并在 401/403/404/409/5xx 时显示服务端错误。

## 9. 最小接入清单

```text
[ ] 在 contracts/openapi/admin-v1.yaml 更新请求/响应 schema
[ ] 添加 domain 校验与 application 用例测试（正常、校验失败、重复、超时）
[ ] 在 bootstrap 显式注册 method key
[ ] 为持久化字段添加 migration Up/Down 和 schema 测试
[ ] 为 HTTP 目标配置 allowInternal 与超时，检查敏感字段脱敏
[ ] 运行 worker/scheduler 集成测试并检查运行日志
[ ] 更新三套 UI、中文/英文文案和本文件
[ ] 运行 OpenAPI 生成检查、Go 测试、vue-tsc、构建和 git diff --check
```

## 10. 故障排查

- `task executor is not registered`：bootstrap 未注册对应 method key，检查注册顺序和任务定义。
- `task http target is blocked`：目标解析到 loopback、私网、链路本地或 localhost；确认 URL、DNS 和 `allowInternal` 双重显式配置。
- `task.run.conflict`：幂等键已存在；读取原运行记录，不要重复创建。
- 长时间 `pending`：检查 worker、队列连接和 Redis lease；查看对应 run 的日志，而不是凭页面状态猜测。
