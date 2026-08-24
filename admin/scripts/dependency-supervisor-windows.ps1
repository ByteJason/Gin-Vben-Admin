param(
  [Parameter(Mandatory = $true)][string]$NodePath,
  [Parameter(Mandatory = $true)][string]$SupervisorPath,
  [Parameter(Mandatory = $true)][string]$GatePath,
  [Parameter(Mandatory = $true)][string]$GateToken
)

$ErrorActionPreference = 'Stop'
if ($GateToken -notmatch '^[0-9a-fA-F]{8}-(?:[0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}$') {
  exit 1
}
$native = @'
using System;
using System.Runtime.InteropServices;

public static class DependencyInstallJob {
  [StructLayout(LayoutKind.Sequential)]
  private struct IO_COUNTERS {
    public UInt64 ReadOperationCount;
    public UInt64 WriteOperationCount;
    public UInt64 OtherOperationCount;
    public UInt64 ReadTransferCount;
    public UInt64 WriteTransferCount;
    public UInt64 OtherTransferCount;
  }

  [StructLayout(LayoutKind.Sequential)]
  private struct JOBOBJECT_BASIC_LIMIT_INFORMATION {
    public Int64 PerProcessUserTimeLimit;
    public Int64 PerJobUserTimeLimit;
    public UInt32 LimitFlags;
    public UIntPtr MinimumWorkingSetSize;
    public UIntPtr MaximumWorkingSetSize;
    public UInt32 ActiveProcessLimit;
    public UIntPtr Affinity;
    public UInt32 PriorityClass;
    public UInt32 SchedulingClass;
  }

  [StructLayout(LayoutKind.Sequential)]
  private struct JOBOBJECT_EXTENDED_LIMIT_INFORMATION {
    public JOBOBJECT_BASIC_LIMIT_INFORMATION BasicLimitInformation;
    public IO_COUNTERS IoInfo;
    public UIntPtr ProcessMemoryLimit;
    public UIntPtr JobMemoryLimit;
    public UIntPtr PeakProcessMemoryUsed;
    public UIntPtr PeakJobMemoryUsed;
  }

  [DllImport("kernel32.dll", CharSet = CharSet.Unicode)]
  private static extern IntPtr CreateJobObject(IntPtr securityAttributes, string name);

  [DllImport("kernel32.dll", SetLastError = true)]
  private static extern bool SetInformationJobObject(IntPtr job, int infoClass, IntPtr info, UInt32 length);

  [DllImport("kernel32.dll", SetLastError = true)]
  public static extern bool AssignProcessToJobObject(IntPtr job, IntPtr process);

  [DllImport("kernel32.dll")]
  public static extern bool CloseHandle(IntPtr handle);

  public static IntPtr CreateKillOnCloseJob() {
    const UInt32 JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE = 0x00002000;
    IntPtr job = CreateJobObject(IntPtr.Zero, null);
    if (job == IntPtr.Zero) throw new System.ComponentModel.Win32Exception();
    JOBOBJECT_EXTENDED_LIMIT_INFORMATION info = new JOBOBJECT_EXTENDED_LIMIT_INFORMATION();
    info.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE;
    int length = Marshal.SizeOf(typeof(JOBOBJECT_EXTENDED_LIMIT_INFORMATION));
    IntPtr pointer = Marshal.AllocHGlobal(length);
    try {
      Marshal.StructureToPtr(info, pointer, false);
      if (!SetInformationJobObject(job, 9, pointer, (UInt32)length)) {
        CloseHandle(job);
        throw new System.ComponentModel.Win32Exception();
      }
    } finally {
      Marshal.FreeHGlobal(pointer);
    }
    return job;
  }
}
'@

$job = [IntPtr]::Zero
$child = $null
try {
  Add-Type -TypeDefinition $native
  $job = [DependencyInstallJob]::CreateKillOnCloseJob()
  $start = [System.Diagnostics.ProcessStartInfo]::new()
  $start.FileName = $NodePath
  $start.Arguments = '"' + $SupervisorPath.Replace('"', '\"') + '"'
  $start.WorkingDirectory = (Get-Location).Path
  $start.UseShellExecute = $false
  $start.CreateNoWindow = $true
  $start.EnvironmentVariables['INIT_WINDOWS_JOB_GATE'] = $GatePath
  $start.EnvironmentVariables['INIT_WINDOWS_JOB_TOKEN'] = $GateToken
  $child = [System.Diagnostics.Process]::Start($start)
  if (-not [DependencyInstallJob]::AssignProcessToJobObject($job, $child.Handle)) {
    throw [System.ComponentModel.Win32Exception]::new()
  }
  $gatePayload = '{"schema":1,"owner":"admin-dependency-job","token":"' + $GateToken + '","guardianPid":' + $PID.ToString([System.Globalization.CultureInfo]::InvariantCulture) + "}`n"
  $gateBytes = [System.Text.UTF8Encoding]::new($false).GetBytes($gatePayload)
  $gateStream = [System.IO.FileStream]::new(
    $GatePath,
    [System.IO.FileMode]::CreateNew,
    [System.IO.FileAccess]::Write,
    [System.IO.FileShare]::None
  )
  try {
    $gateStream.Write($gateBytes, 0, $gateBytes.Length)
    $gateStream.Flush($true)
  } finally {
    $gateStream.Dispose()
  }
  $child.WaitForExit()
  $exitCode = $child.ExitCode
} catch {
  $exitCode = 1
} finally {
  if ($job -ne [IntPtr]::Zero) {
    [void][DependencyInstallJob]::CloseHandle($job)
  }
  Remove-Item -LiteralPath $GatePath -Force -ErrorAction SilentlyContinue
}
exit $exitCode
