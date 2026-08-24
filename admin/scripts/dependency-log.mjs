import {
  closeSync,
  constants as fsConstants,
  fstatSync,
  lstatSync,
  openSync,
} from 'node:fs';

const OPEN_ATTEMPTS = 3;

function inspectPath(path, inspect) {
  try {
    return { kind: 'present', stat: inspect(path) };
  } catch (error) {
    if (error?.code === 'ENOENT') return { kind: 'missing' };
    throw error;
  }
}

function privateRegularFile(stat) {
  return Boolean(
    stat
    && stat.isFile()
    && !stat.isSymbolicLink()
    && Number(stat.nlink) === 1
  );
}

function sameFile(left, right) {
  return left.dev === right.dev && left.ino === right.ino;
}

export function openDependencyLog(path, options = {}) {
  const inspect = options.lstat ?? lstatSync;
  const openFile = options.open ?? openSync;
  const inspectDescriptor = options.fstat ?? fstatSync;
  const close = options.close ?? closeSync;

  for (let attempt = 0; attempt < OPEN_ATTEMPTS; attempt += 1) {
    let initial;
    try {
      initial = inspectPath(path, inspect);
      if (initial.kind === 'present' && !privateRegularFile(initial.stat)) {
        throw new Error('DEPENDENCY_INSTALL_FAILED');
      }
    } catch {
      throw new Error('DEPENDENCY_INSTALL_FAILED');
    }

    const flags = initial.kind === 'missing'
      ? fsConstants.O_APPEND | fsConstants.O_CREAT | fsConstants.O_EXCL | fsConstants.O_WRONLY
      : fsConstants.O_APPEND | fsConstants.O_WRONLY | (fsConstants.O_NOFOLLOW ?? 0);
    let descriptor;
    try {
      descriptor = openFile(path, flags, 0o600);
    } catch (error) {
      if (initial.kind === 'missing' && error?.code === 'EEXIST' && attempt + 1 < OPEN_ATTEMPTS) continue;
      throw new Error('DEPENDENCY_INSTALL_FAILED');
    }

    try {
      const opened = inspectDescriptor(descriptor);
      const published = inspectPath(path, inspect);
      if (
        !privateRegularFile(opened)
        || published.kind !== 'present'
        || !privateRegularFile(published.stat)
        || !sameFile(opened, published.stat)
        || (initial.kind === 'present' && !sameFile(initial.stat, opened))
      ) throw new Error('DEPENDENCY_INSTALL_FAILED');
      return descriptor;
    } catch {
      try { close(descriptor); } catch {}
      throw new Error('DEPENDENCY_INSTALL_FAILED');
    }
  }
  throw new Error('DEPENDENCY_INSTALL_FAILED');
}
