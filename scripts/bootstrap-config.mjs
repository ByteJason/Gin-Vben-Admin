const supportedDrivers = new Set(['mysql', 'postgres']);

export function renderServerConfig(template, database) {
  if (!supportedDrivers.has(database)) {
    throw new Error(`unsupported database driver: ${database}`);
  }

  const driverLine = /^(\s{2}driver:\s*)(mysql|postgres)\s*$/m;
  if (!driverLine.test(template)) {
    throw new Error('database.driver is missing from the server configuration template');
  }
  return template.replace(driverLine, (_line, prefix) => `${prefix}${database}`);
}
