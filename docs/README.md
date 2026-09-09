# 公开文档

公共能力入口覆盖 SMTP、通知/验证码、媒体库、Logo 资源引用、定时任务及三套管理端的开发者引导 UI；各模块的 OpenAPI、持久化、迁移和人工验收边界以对应开发文档为准。定时任务接入指南特别说明了 `v005_task_run_execution_metadata` 的升级/回滚入口、显式 RegisteredMethod 注册、HTTP SSRF 约束及 memory/数据库+Redis 的部署差异。

- [项目安装与启动](../README.md#快速开始)
- [1.0.0-dev 全功能手工验收手册](manual-acceptance/1.0.0-dev-end-to-end.md)
- [管理端信息架构、菜单与页面职责](admin-information-architecture.md)
- [管理端开发命令](../admin/README.md)
- [公共能力需求：SMTP、验证码、通知与媒体库](requirements/common-capabilities.md)
- [公共能力 API 对接参考（文件/方法/参数/返回值/Demo）](integration/common-capabilities-api.md)
- [公共能力开发指南：SMTP、媒体库与 Logo](development/common-capabilities.md)
- [定时任务接入与运维指南：注册方法、HTTP 调用、执行日志与部署](development/scheduled-tasks.md)
- [数据库迁移与配置示例](../README.md#使用说明)
- [开发验证](../README.md#验证)
