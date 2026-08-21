# 安全报告

请不要在公开 issue 中披露可利用的漏洞、密钥或真实环境信息。请通过项目维护者提供的私密渠道提交安全报告，并包含：

- 受影响版本和运行模式
- 可复现步骤或最小请求样例
- 影响范围和建议修复方向
- 脱敏后的日志、响应和环境信息

本地安全门禁可运行：

```text
node scripts/security-gates.mjs --output .runtime/security/security-report.json
node scripts/security-gates.mjs --output .runtime/security/security-report.json --check
```

发布候选只对隔离环境执行 DAST；不要把扫描目标指向生产或第三方系统。
