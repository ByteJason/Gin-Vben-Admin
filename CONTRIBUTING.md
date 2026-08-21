# 贡献指南

感谢参与 Gin-Vben-Admin。提交改动前请先阅读 `README.md`、相关 OpenAPI 契约和本地运行说明。

## 开发流程

1. 从最新 `main` 创建聚焦单一主题的分支。
2. 先补失败测试，再实现最小改动，最后整理代码。
3. 使用 Conventional Commits，例如 `feat(设置): 增加配置校验`、`fix(接口): 修正错误响应`。
4. 提交前运行受影响的单元、集成、契约、UI 和构建检查，并在合并请求中附命令与结果。
5. 合并请求应说明变更范围、兼容性、迁移与回滚方式；涉及数据结构时必须同时提供 up/down 或明确不可逆原因。

## 代码边界

- `admin/` 只放管理端工作区；`server/` 只放 Gin 服务端。
- `contracts/` 是跨端接口契约的权威来源，生成文件标明生成方向。
- 不提交密钥、真实连接串、运行时数据、构建产物或本地缓存。
- 保留第三方版权与许可证声明，新增依赖同步更新锁文件和归属说明。

## 本地检查

```text
node --test tests/contract/contract.test.mjs
pnpm --dir admin run test:smoke
pnpm --dir admin run check:type
go -C server test ./...
node ./scripts/verify.mjs --scope basic
```

提交前执行 `git diff --check`，并确认工作树没有未跟踪的密钥或数据库文件。
