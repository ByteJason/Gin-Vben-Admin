import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { performance } from 'node:perf_hooks';

const TOKEN_PATTERN = /^[0-9a-f]{64}$/;

function digest(material) {
  return createHash('sha256').update(material).digest('hex');
}

function commandOutput(command, args, options, runProcess) {
  const result = runProcess(command, args, {
    encoding: 'utf8',
    shell: false,
    timeout: 2_000,
    windowsHide: true,
    ...options,
  });
  if (result?.error || result?.status !== 0 || typeof result?.stdout !== 'string') return null;
  const output = result.stdout.trim();
  return output && output.length <= 32_768 ? output : null;
}

export function validProcessStartToken(value) {
  return typeof value === 'string' && TOKEN_PATTERN.test(value);
}

export function processStartToken(pid, options = {}) {
  if (!Number.isInteger(pid) || pid <= 0) return null;
  const platform = options.platform ?? process.platform;
  const read = options.readFileSync ?? readFileSync;
  const runProcess = options.spawnSync ?? spawnSync;
  try {
    if (platform === 'linux') {
      const stat = String(read(`/proc/${pid}/stat`, 'utf8'));
      const closing = stat.lastIndexOf(')');
      if (closing < 0) return null;
      const fields = stat.slice(closing + 1).trim().split(/\s+/);
      const startTime = fields[19];
      if (!/^[0-9]+$/.test(startTime ?? '')) return null;
      const bootId = String(read('/proc/sys/kernel/random/boot_id', 'utf8')).trim();
      if (!/^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/i.test(bootId)) return null;
      return digest(`linux\0${bootId.toLowerCase()}\0${startTime}`);
    }
    if (platform === 'darwin') {
      const output = commandOutput('/bin/ps', [
        '-ww',
        '-p',
        String(pid),
        '-o',
        'lstart=',
        '-o',
        'command=',
      ], {
        env: { ...process.env, LANG: 'C', LC_ALL: 'C' },
      }, runProcess);
      if (output) return digest(`darwin\0${output}`);
      // Sandboxed launchers may deny ps even for the current process. This
      // fallback is only used by the owner when publishing its own identity;
      // another process treats an unavailable lookup as bounded fail-closed.
      return pid === process.pid ? digest(`darwin-self\0${Math.floor(performance.timeOrigin)}`) : null;
    }
    if (platform === 'win32') {
      const script = [
        `$p = Get-Process -Id ${pid} -ErrorAction Stop`,
        "$ticks = $p.StartTime.ToUniversalTime().Ticks.ToString([System.Globalization.CultureInfo]::InvariantCulture)",
        '[Console]::Out.Write($ticks)',
      ].join('; ');
      const output = commandOutput('powershell.exe', [
        '-NoLogo',
        '-NoProfile',
        '-NonInteractive',
        '-Command',
        script,
      ], {}, runProcess);
      return /^[0-9]+$/.test(output ?? '') ? digest(`windows\0${output}`) : null;
    }
    return null;
  } catch {
    return null;
  }
}
