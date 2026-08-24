import { open, lstat } from 'node:fs/promises';
import { parentPort, workerData } from 'node:worker_threads';

const { file, heartbeatRoot, id, intervalMs, pid, pidStartToken, rootDev, rootIno } = workerData ?? {};
if (
  !parentPort
  || typeof file !== 'string'
  || typeof heartbeatRoot !== 'string'
  || typeof id !== 'string'
  || !Number.isInteger(pid)
  || pid <= 0
  || !/^[0-9a-f]{64}$/.test(pidStartToken ?? '')
  || !Number.isInteger(intervalMs)
  || intervalMs < 10
  || typeof rootDev !== 'number'
  || typeof rootIno !== 'number'
) {
  process.exit(2);
}

async function assertHeartbeatRoot() {
  const stat = await lstat(heartbeatRoot);
  if (
    !stat.isDirectory()
    || stat.isSymbolicLink()
    || stat.dev !== rootDev
    || stat.ino !== rootIno
  ) throw new Error('HEARTBEAT_ROOT_REPLACED');
}

let handle;
let timer;
let stopped = false;
let writing = Promise.resolve();

function contents() {
  return `${JSON.stringify({
    schema: 2,
    owner: 'admin-init',
    id,
    pid,
    pidStartToken,
    updatedAt: new Date().toISOString(),
  })}\n`;
}

async function writeHeartbeat() {
  await assertHeartbeatRoot();
  const pathStat = await lstat(file);
  const handleStat = await handle.stat();
  if (
    !pathStat.isFile()
    || pathStat.isSymbolicLink()
    || pathStat.dev !== handleStat.dev
    || pathStat.ino !== handleStat.ino
  ) throw new Error('HEARTBEAT_REPLACED');
  const buffer = Buffer.from(contents());
  const { bytesWritten } = await handle.write(buffer, 0, buffer.length, 0);
  if (bytesWritten !== buffer.length) throw new Error('HEARTBEAT_SHORT_WRITE');
  await handle.truncate(buffer.length);
  await handle.sync();
}

async function stop() {
  if (stopped) return;
  stopped = true;
  clearInterval(timer);
  await writing.catch(() => {});
  await handle?.close().catch(() => {});
  parentPort.postMessage({ type: 'stopped' });
  parentPort.close();
}

async function stopFailedChannel(error) {
  if (stopped) return;
  stopped = true;
  clearInterval(timer);
  await handle?.close().catch(() => {});
  parentPort.postMessage({ type: 'degraded', code: error instanceof Error ? error.message : 'HEARTBEAT_FAILED' });
  parentPort.close();
}

process.on('uncaughtException', (error) => void stopFailedChannel(error));
process.on('unhandledRejection', (error) => void stopFailedChannel(error));

try {
  await assertHeartbeatRoot();
  handle = await open(file, 'wx', 0o600);
  await assertHeartbeatRoot();
  const initial = Buffer.from(contents());
  const { bytesWritten } = await handle.write(initial, 0, initial.length, 0);
  if (bytesWritten !== initial.length) throw new Error('HEARTBEAT_SHORT_WRITE');
  await handle.truncate(initial.length);
  await handle.sync();
  parentPort.postMessage({ type: 'ready' });
  timer = setInterval(() => {
    writing = writing.then(writeHeartbeat).catch(stopFailedChannel);
  }, intervalMs);
  parentPort.on('message', (message) => {
    if (message?.type === 'stop') void stop();
  });
  parentPort.on('close', () => void stop());
} catch (error) {
  parentPort.postMessage({ type: 'error', code: error instanceof Error ? error.message : 'HEARTBEAT_FAILED' });
  await stop();
}
