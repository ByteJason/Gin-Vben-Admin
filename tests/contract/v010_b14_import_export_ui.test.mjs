import test from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
const root = process.cwd();
const apps = ['web-antd','web-ele','web-naive'];
for (const app of apps) {
  test(`B1.4 ${app} exposes import/export vertical slice`, () => {
    const api = join(root, `admin/apps/${app}/src/api/core/import-export.ts`);
    const page = join(root, `admin/apps/${app}/src/views/system/import-export/index.vue`);
    const routes = join(root, `admin/apps/${app}/src/router/routes/modules/system.ts`);
    assert.ok(existsSync(api), `${app} API`); assert.ok(existsSync(page), `${app} page`);
    const apiText = readFileSync(api,'utf8'); const pageText=readFileSync(page,'utf8'); const routeText=readFileSync(routes,'utf8');
    for (const token of ['preview','commit','cancel','retry','downloadErrorRows','export']) assert.match(apiText, new RegExp(token,'i'), `${app} api ${token}`);
    for (const token of ['preview','commit','cancel','retry','download','export']) assert.match(pageText, new RegExp(token,'i'), `${app} page ${token}`);
    assert.match(routeText, /import-export/);
    for (const locale of ['zh-CN','en-US']) {
      const text=readFileSync(join(root,`admin/apps/${app}/src/locales/langs/${locale}/page.json`),'utf8');
      for (const key of ['importExport','importPreview','importCommit','importCancel','importRetry','importDownloadErrors','exportExpired']) assert.match(text,new RegExp(`"${key}"\\s*:`),`${app}/${locale}/${key}`);
    }
  });
}
