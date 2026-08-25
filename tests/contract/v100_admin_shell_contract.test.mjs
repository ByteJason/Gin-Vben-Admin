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

test('public information architecture freezes the four production groups', () => {
  const architecture = read('docs/admin-information-architecture.md');
  const normalized = architecture.replaceAll('`', '');
  const expected = [
    ['概览', '运行概览', '/dashboard/analytics', 'dashboard:overview:read'],
    ['身份与权限', '用户管理', '/iam/users', 'iam:users:read'],
    ['身份与权限', '角色管理', '/iam/roles', 'iam:roles:read'],
    ['身份与权限', '菜单管理', '/iam/menus', 'iam:menus:read'],
    ['身份与权限', '权限列表', '/iam/permissions', 'iam:permissions:read'],
    ['系统配置', '系统设置', '/system/settings', 'system:settings:read'],
    ['系统配置', '字典管理', '/system/dictionary', 'system:dictionary:read'],
    ['系统配置', '邮件服务', '/system/mail', 'system:mail:read'],
    ['系统配置', '文件中心', '/system/files', 'system:files:read'],
    [
      '系统配置',
      '可观测设置',
      '/system/observability',
      'system:observability:read',
    ],
    ['运维中心', '资源监控', '/system/monitor', 'ops:monitor:read'],
    ['运维中心', '审计日志', '/system/audit', 'ops:audit:read'],
    ['运维中心', '任务管理', '/system/tasks', 'ops:tasks:read'],
    ['运维中心', '数据作业', '/system/import-export', 'ops:data-jobs:read'],
  ];

  for (const row of expected) {
    assert.ok(
      normalized.includes(`| ${row.join(' | ')} |`),
      `missing documented menu row: ${row.join(' / ')}`,
    );
  }
  assert.match(architecture, /`mixed` 过渡模式/);
  assert.match(architecture, /`\/analytics`[\s\S]*?`\/dashboard\/analytics`/);
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
    'system:files:manage',
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
