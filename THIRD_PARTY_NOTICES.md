# 第三方归属与许可证

本项目使用的上游管理端代码源自 [Vue Vben Admin](https://github.com/vbenjs/vue-vben-admin)，快照 `e3369bd63523831abb24a604da7721ba4f8c8db6` 与许可证见 [`LICENSES/Vue-Vben-Admin-MIT.txt`](LICENSES/Vue-Vben-Admin-MIT.txt)。上游快照记录也见 [`UPSTREAM_SNAPSHOT.md`](UPSTREAM_SNAPSHOT.md) 和 [`NOTICE`](NOTICE)。

依赖来源由以下锁文件确定：

- `admin/pnpm-lock.yaml`
- `server/go.sum`

发布候选的离线清单命令：

```text
node scripts/release/sbom.mjs --output .runtime/release/sbom.cdx.json
node scripts/release/license-policy.mjs --sbom .runtime/release/sbom.cdx.json --notice NOTICE
```

依赖锁文件缺少 SPDX 元数据时，清单会标记 `NOASSERTION` 并要求有责任人与到期日的例外记录；不会把未知许可证伪称为 MIT。项目自身许可证见 [`LICENSE`](LICENSE)。
