# 管理端信息架构与页面职责

> 适用版本：`1.0.0-dev`。本文是生产侧栏、路由、权限与页面内容的权威清单；历史 checkpoint 中的旧菜单只代表当时状态。

## 1. 导航原则

- 登录后的唯一首页是 `/dashboard/analytics`，页面名称为“运行概览”。`/`、`/dashboard`、旧 `/analytics` 与 `/dashboard/workspace` 最终都收敛到该地址。
- 生产侧栏只显示产品功能。上游 Vben 示例、组件演示和未接入真实 API 的模板入口只在开发模式提供，不进入安装种子。
- 菜单采用 `mixed` 过渡模式：优先消费服务端 `/api/admin/v1/menu/all`；新安装会写入确定性的生产菜单种子；旧库菜单为空时，前端使用同构静态菜单兜底。服务端仍是 API 授权的唯一裁决者。
- 页面容器占满 `<main>` 可用宽度，外层不设置固定 `max-width`；页面边距在 375/768/1024/1440 断点自适应。表单正文可在页面内部限制可读宽度，数据表和监控面板不限制。
- 导航可见性使用访问码控制；直接输入 URL 仍必须经过服务端权限校验。超级管理员由服务端通配策略覆盖，不在前端硬编码绕过。

## 2. 生产菜单

| 一级菜单 | 二级菜单 | 路由 | 访问码 | 页面内容 |
|---|---|---|---|---|
| 概览 | 运行概览 | `/dashboard/analytics` | `dashboard:overview:read` | 实例、版本与运行时间；数据库、Redis、任务等真实服务状态摘要；快捷进入资源监控；无数据时显示空态，不生成演示指标 |
| 身份与权限 | 用户管理 | `/iam/users` | `iam:users:read` | 用户查询、分页、创建/编辑、启停、软删除、重置密码、角色分配与登录记录 |
| 身份与权限 | 角色管理 | `/iam/roles` | `iam:roles:read` | 角色列表与创建；权限和数据范围关系维护；成员分配仍在用户管理页完成，未闭环的详情编辑/删除明确标记为待完成 |
| 身份与权限 | 菜单管理 | `/iam/menus` | `iam:menus:read` | 目录/菜单/按钮树、组件白名单、访问码、排序、启停及动态路由发布数据 |
| 身份与权限 | 权限列表 | `/iam/permissions` | `iam:permissions:read` | API/按钮权限元数据与状态；策略、数据范围改由角色详情承载 |
| 系统配置 | 系统设置 | `/system/settings` | `system:settings:read` | 基础、安全、语言、第三方等配置分类、校验和版本信息 |
| 系统配置 | 字典管理 | `/system/dictionary` | `system:dictionary:read` | 字典类型、条目、双语文本、排序与状态 |
| 系统配置 | 邮件服务 | `/system/mail` | `system:mail:read` | SMTP 账号池、连接测试和投递记录；普通管理页暂不展示邮件正文详情 |
| 系统配置 | 文件中心 | `/system/files` | `system:files:read` | 当前本地 provider 的对象元数据、上传下载、签名 URL、访问控制、删除和清理预检；远程对象存储尚未交付 |
| 系统配置 | 可观测设置 | `/system/observability` | `system:observability:read` | metrics、trace、日志导出开关与目标配置；不与实时资源监控混用 |
| 运维中心 | 资源监控 | `/system/monitor` | `ops:monitor:read` | 实例/build/runtime；CPU、内存、文件系统；数据库与 Redis 健康、延迟、连接池及非敏感计数；15 秒实时会话趋势和局部降级 |
| 运维中心 | 审计日志 | `/system/audit` | `ops:audit:read` | 登录、授权、配置和数据操作审计的筛选、详情与导出 |
| 运维中心 | 任务管理 | `/system/tasks` | `ops:tasks:read` | 调度、手动运行、取消、重试、日志、并发和失败状态 |
| 运维中心 | 数据作业 | `/system/import-export` | `ops:data-jobs:read` | 模板、导入预检、异步导入导出、进度、错误行与下载状态 |

一级分组的稳定标识与路径分别为：`menu-overview`/`/dashboard`、`menu-identity`/`/iam`、`menu-system-config`/`/configuration`、`menu-operations`/`/operations`。二级项保留已有业务 URL，避免破坏深链和书签。

### 2.1 读取、管理与页面依赖访问码

`*:read` 决定菜单和页面读取能力，`*:manage` 决定新增、编辑、删除、执行、重试等写操作。前端隐藏或禁用无权操作只是交互反馈，服务端仍逐请求裁决。页面发起辅助请求前还必须具有相应依赖访问码，不能因为能打开主页面就隐式扩权。

| 页面 | 读取访问码 | 写操作访问码 | 辅助依赖访问码 |
|---|---|---|---|
| 运行概览 | `dashboard:overview:read` | — | — |
| 用户管理 | `iam:users:read` | `iam:users:manage` | `iam:roles:read`（角色选项）、`iam:roles:manage`（成员关系写入） |
| 角色管理 | `iam:roles:read` | `iam:roles:manage` | `iam:permissions:read`、`iam:data-scopes:read` |
| 菜单管理 | `iam:menus:read` | `iam:menus:manage` | `iam:components:read`（组件白名单） |
| 权限列表 | `iam:permissions:read` | — | — |
| 系统设置 | `system:settings:read` | `system:settings:manage` | — |
| 字典管理 | `system:dictionary:read` | `system:dictionary:manage` | `system:settings:read`（可选读取 i18n 策略；无权时不请求并采用编译默认） |
| 邮件服务 | `system:mail:read` | `system:mail:manage` | — |
| 文件中心 | `system:files:read` | `system:files:manage` | — |
| 可观测设置 | `system:observability:read` | `system:observability:manage` | — |
| 资源监控 | `ops:monitor:read` | — | — |
| 审计日志 | `ops:audit:read` | —（导出与保留预检均为只读 GET） | — |
| 任务管理 | `ops:tasks:read` | `ops:tasks:manage` | — |
| 数据作业 | `ops:data-jobs:read` | `ops:data-jobs:manage` | — |

兼容深链 `/iam/policies` 与 `/iam/data-scopes` 不进入生产菜单；读取分别使用 `iam:policies:read`、`iam:data-scopes:read`，写入分别使用 `iam:policies:manage`、`iam:data-scopes:manage`。平台管理员默认由安装时的通配策略获得全部访问码；其他主体只要被明确授予 `ops:monitor:read` 也可以访问资源监控，页面不以角色名称硬编码放行。

## 3. 资源监控的数据边界

资源监控只展示服务端能够真实采集且不泄露凭据的数据：

- **实例**：版本、提交、Go 版本、OS/架构、启动时间、运行时间、live/ready 状态。
- **CPU/进程**：逻辑核心、进程 CPU 或明确命名的 load 1/5/15；不把 load 伪装成 CPU 使用率。
- **内存**：进程 RSS、Go heap/GC；能识别容器或主机限制时再显示对应总量和占比。
- **文件系统**：当前数据所在文件系统的总量、已用、可用与采集范围；不回传真实目录。
- **数据库**：驱动、部署模式、健康、延迟、open/in-use/idle/max、等待次数/时长。
- **Redis**：部署模式、健康、延迟、连接池和可获得的 keyspace 计数。

合法的零值必须显示为 `0`，采集不到显示“不可用”，失败的单个依赖显示 `degraded`，不能把三者混为一谈。地址、DSN、数据库/Redis 密码、token、原始命令和本机路径均不进入响应。没有接入持久化时序源前，图表仅表示当前浏览器会话采样，不宣称历史趋势。

## 4. 兼容路由与隐藏入口

| 旧入口 | 当前行为 | 说明 |
|---|---|---|
| `/analytics` | 重定向到 `/dashboard/analytics` | 修复旧 `homePath` |
| `/workspace`、`/dashboard/workspace` | 重定向到 `/dashboard/analytics` | 合并两套演示首页 |
| `/iam/policies` | 保留兼容访问，不进生产菜单 | 策略关系转入角色详情 |
| `/iam/data-scopes` | 保留兼容访问，不进生产菜单 | 数据范围关系转入角色详情 |
| `/profile` 及模板注册/找回/扫码入口 | `/profile` 仅开发模式注册；其余功能闭环前不进生产菜单 | 避免生产构建暴露模拟交互 |
| Vben demo/example 路由 | 仅开发模式 | 不写入安装菜单种子，也不计入业务验收 |

404 页“返回首页”必须使用当前用户的 `homePath`，没有用户资料时回退 `/dashboard/analytics`，不得制造 `/` → 旧路由 → 404 的跳转链。

## 5. 登录与访问码契约

`GET /api/admin/v1/iam/me` 返回用户资料、规范化首页和 `accessCodes`。兼容接口 `GET /api/admin/v1/auth/codes` 从同一个 IAM 权限源返回访问码，不维护第二份权限规则。权限决策缓存按租户、组织和平台管理员上下文隔离，并把一次读取绑定到当时的缓存代次；只缓存明确拒绝，allow 始终回到权威授权源复核，因此撤权提交后不会继续复用旧 allow。

可观测设置使用独立的 `GET/PUT /api/admin/v1/observability/settings/{key}` 入口，并只接受文档化的 `observability.*` 键。`system:observability:read/manage` 不匹配通用 `/settings/{key}`，因此不会隐式获得安全、文件或邮件配置的读写权限。

前端按原子事务处理登录：token、用户资料和访问码全部建立后才完成跳转；新版 `/iam/me` 已包含 `accessCodes` 时不再发起兼容请求。只在旧服务端完全缺少该字段时调用 `/auth/codes`，且仅该兼容接口的 404 可降级为空数组；其他错误回滚本次认证状态并展示真实错误。

## 6. 三套 UI 等价验收

Ant Design Vue、Element Plus 与 Naive UI 必须共享相同的路由、权限、API 适配器和页面壳契约，同时保留各自组件实现。验收至少覆盖：

1. 登录后只出现上述四个一级分组，顺序和二级项一致。
2. `/`、登录回跳、404 返回首页和旧地址都落到“运行概览”。
3. 375/768/1024/1440 宽度无页面级固定最大宽度、无整页横向滚动；宽表只在自己的滚动容器内滚动。
4. 运行概览与资源监控覆盖 loading、empty、degraded、error、stale 和真实零值。
5. 键盘焦点、语义标题、状态文本、深浅主题和 reduced-motion 行为等价。

## 7. 当前不冒充已完成的能力

以下入口有后端或局部页面，但尚未达到完整产品闭环，因此不扩展生产菜单：个人资料/会话管理、注册与找回密码、角色完整编辑/删除、远程对象存储、独立任务 worker 的生产部署、导出成功下载闭环、持久化监控时序与告警链路。它们继续由需求与验收文档记录红灯，完成对应 API、权限、状态和回归后再进入菜单。
