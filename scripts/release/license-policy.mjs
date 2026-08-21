#!/usr/bin/env node
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const args = process.argv.slice(2);
const value = (flag) => { const i = args.indexOf(flag); return i >= 0 ? args[i + 1] : undefined; };
const sbomPath = value('--sbom');
const noticePath = value('--notice');
if (!sbomPath || !noticePath) {
  console.error('usage: license-policy.mjs --sbom <path> --notice <path> [--check]');
  process.exit(2);
}
const allowed = new Set(['MIT', 'Apache-2.0', 'BSD-2-Clause', 'BSD-3-Clause', 'ISC', 'MPL-2.0', '0BSD', 'CC0-1.0', 'LGPL-2.1-only', 'LGPL-3.0-only', 'Unlicense']);
const doc = JSON.parse(readFileSync(resolve(sbomPath), 'utf8'));
const notice = readFileSync(resolve(noticePath), 'utf8');
const exceptionPath = resolve(fileURLToPath(new URL('./license-exceptions.json', import.meta.url)));
const exceptions = JSON.parse(readFileSync(exceptionPath, 'utf8')).exceptions ?? [];
const exceptionFor = (component, id) => {
  const source = component.properties?.find((property) => property.name === 'source')?.value;
  return exceptions.find((item) => item.source === source && item.license === id && item.expires >= new Date().toISOString().slice(0, 10));
};
const counts = {};
const unknown = [];
const registeredExceptions = [];
for (const component of doc.components ?? []) {
  const ids = (component.licenses ?? []).flatMap((entry) => entry.license?.id ?? entry.expression ?? []);
  const list = Array.isArray(ids) ? ids : [ids];
  const unresolved = list.length ? list.filter((id) => !allowed.has(id)) : ['UNKNOWN'];
  const unregistered = unresolved.filter((id) => !exceptionFor(component, id));
  for (const id of unresolved.filter((id) => exceptionFor(component, id))) registeredExceptions.push({ name: component.name, license: id, source: exceptionFor(component, id).source });
  if (unregistered.length) unknown.push({ name: component.name, licenses: unregistered });
  for (const id of list.length ? list : ['UNKNOWN']) counts[id] = (counts[id] ?? 0) + 1;
}
if (!notice.includes('Gin-Vben-Admin') || !notice.includes('Vue Vben Admin')) unknown.push({ name: 'NOTICE', licenses: ['missing project attribution'] });
const summary = { status: unknown.length ? 'failed' : 'passed', componentCount: doc.components?.length ?? 0, licenseCounts: Object.fromEntries(Object.entries(counts).sort()), unknown, registeredExceptions: registeredExceptions.length, exceptionInventory: exceptionPath };
console.log(JSON.stringify(summary));
if (unknown.length) process.exit(1);
