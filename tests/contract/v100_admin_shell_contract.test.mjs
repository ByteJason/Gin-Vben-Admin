import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const root = new URL('../../', import.meta.url);
const read = (path) => readFileSync(new URL(path, root), 'utf8');

test('admin shell exposes one IAM-owned access-code contract', () => {
  const openapi = read('contracts/openapi/admin-v1.yaml');
  const client = read('admin/packages/api-client/src/generated/admin-v1.ts');

  assert.match(openapi, /^  \/api\/admin\/v1\/auth\/codes:$/m);
  assert.match(openapi, /operationId: getAdminAuthAccessCodes/);
  assert.match(
    openapi,
    /CurrentUser:[\s\S]*?accessCodes:[\s\S]*?type: array[\s\S]*?items: \{ type: string \}/,
  );
  assert.match(client, /getAdminAuthAccessCodes: '\/admin\/v1\/auth\/codes'/);
  assert.match(client, /codes: ADMIN_ENDPOINTS\.getAdminAuthAccessCodes/);
});

test('public information architecture freezes the five production groups', () => {
  const architecture = read('docs/admin-information-architecture.md');
  const normalized = architecture.replaceAll('`', '');
  const expected = [
    ['仪表盘', '（直接进入）', '/dashboard', 'dashboard:overview:read'],
    ['后台权限', '用户管理', '/iam/users', 'iam:users:read'],
    ['后台权限', '角色管理', '/iam/roles', 'iam:roles:read'],
    ['后台权限', '菜单管理', '/iam/menus', 'iam:menus:read'],
    ['后台权限', '权限管理', '/iam/permissions', 'iam:permissions:read'],
    ['系统管理', '系统设置', '/system/settings', 'system:settings:read'],
    ['系统管理', '参数管理', '/system/parameters', 'system:parameters:read'],
    ['系统管理', '字典管理', '/system/dictionary', 'system:dictionary:read'],
    ['系统管理', '邮件服务', '/system/mail', 'system:mail:read'],
    [
      '系统管理',
      '可观测设置',
      '/system/observability',
      'system:observability:read',
    ],
    ['运维监控', '服务器状态', '/ops/server-status', 'ops:server-status:read'],
    [
      '运维监控',
      '操作历史',
      '/ops/operation-history',
      'ops:operation-history:read',
    ],
    ['运维监控', '登录日志', '/ops/login-logs', 'ops:login-logs:read'],
    ['运维监控', '定时任务', '/ops/tasks', 'ops:tasks:read'],
    ['运维监控', '数据作业', '/ops/data-jobs', 'ops:data-jobs:read'],
    ['媒体管理', '媒体库', '/media/library', 'media:library:read'],
  ];

  for (const row of expected) {
    assert.ok(
      normalized.includes(`| ${row.join(' | ')} |`),
      `missing documented menu row: ${row.join(' / ')}`,
    );
  }
  assert.match(architecture, /`mixed` 过渡模式/);
  assert.match(architecture, /`\/dashboard\/analytics`[\s\S]*?`\/dashboard`/);
  assert.match(architecture, /数据库[\s\S]*?Redis[\s\S]*?不回传/);
});

test('public information architecture distinguishes read, manage and support capabilities', () => {
  const architecture = read('docs/admin-information-architecture.md');
  const adminReadme = read('admin/README.md');
  const requiredCodes = [
    'iam:users:manage',
    'iam:roles:manage',
    'iam:menus:manage',
    'iam:components:read',
    'iam:policies:read',
    'iam:data-scopes:read',
    'system:settings:manage',
    'system:dictionary:manage',
    'system:mail:manage',
    'media:library:manage',
    'system:observability:manage',
    'ops:tasks:manage',
    'ops:data-jobs:manage',
  ];

  for (const code of requiredCodes) {
    assert.ok(architecture.includes(`\`${code}\``), `missing ${code}`);
  }
  assert.match(architecture, /平台管理员默认[\s\S]*?明确授予 `ops:monitor:read`/);
  assert.match(architecture, /进程 RSS[\s\S]*?数据库[\s\S]*?Redis/);
  assert.doesNotMatch(architecture, /本地\/远程对象元数据/);
  assert.doesNotMatch(architecture, /投递记录和脱敏详情/);
  assert.match(adminReadme, /当前接口版本：`1\.0\.0-dev`/);
});
