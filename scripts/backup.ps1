<#
.SYNOPSIS
    Ark 班轮自动化定时备份脚本 (Windows / PowerShell)

.DESCRIPTION
    适用于 Windows 操作系统下的定时任务计划 (Task Scheduler) 或手动一键快速备份。
    具备工作区自寻址、带时间戳日志输出、小磁盘极致干净模式与错误捕获。
    自动按配额保留备份个数 ($env:ARK_RETENTION_COUNT 或 --keep N，默认 5 个)，
    并在备份完成后顺带自动清除远端未打标孤立版本 (untagged)，杜绝远端镜像堆积。

.EXAMPLE
    # 手动运行测试:
    powershell -ExecutionPolicy Bypass -File .\scripts\backup.ps1

.EXAMPLE
    # Windows 任务计划程序一键注册命令 (每日凌晨 03:00 自动执行):
    $Action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument "-ExecutionPolicy Bypass -File F:\workspace\ark\scripts\backup.ps1"
    $Trigger = New-ScheduledTaskTrigger -Daily -At 3am
    Register-ScheduledTask -TaskName "ArkDailyBackup" -Action $Action -Trigger $Trigger -Description "Ark 容器镜像每日自动备份" -User "SYSTEM"
#>

[CmdletBinding()]
param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$CustomArgs
)

$ErrorActionPreference = "Continue"

# 1. 自动定位 Ark 安装目录
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if (Test-Path (Join-Path $ScriptDir "..\ark.exe")) {
    $ArkDir = (Resolve-Path (Join-Path $ScriptDir "..")).Path
} elseif (Test-Path (Join-Path $ScriptDir "ark.exe")) {
    $ArkDir = $ScriptDir
} else {
    $ArkDir = (Get-Location).Path
}

$ArkBin = Join-Path $ArkDir "ark.exe"
$LogDir = Join-Path $ArkDir "logs"
$LogFile = Join-Path $LogDir "backup.log"

if (-not (Test-Path $ArkBin)) {
    Write-Error "[ERROR] 未在 $ArkDir 找到可执行文件 ark.exe！"
    exit 1
}

if (-not (Test-Path $LogDir)) {
    New-Item -ItemType Directory -Path $LogDir -Force | Out-Null
}

# 2. 准备执行参数
# 尝试从 .env 预载入环境变量
$EnvFile = Join-Path $ArkDir ".env"
if (Test-Path $EnvFile) {
    Get-Content $EnvFile | ForEach-Object {
        $line = $_.Trim()
        if ($line -and -not $line.StartsWith("#") -and $line.Contains("=")) {
            $parts = $line.Split("=", 2)
            $k = $parts[0].Trim()
            $v = $parts[1].Trim().Trim('"').Trim("'")
            if (-not [System.Environment]::GetEnvironmentVariable($k)) {
                [System.Environment]::SetEnvironmentVariable($k, $v)
            }
        }
    }
}

$RetentionCount = if ($env:ARK_RETENTION_COUNT) { $env:ARK_RETENTION_COUNT } else { "5" }
$BackupArgs = @("day", "--keep", $RetentionCount, "--clean-all")
if ($CustomArgs -and $CustomArgs.Count -gt 0) {
    $BackupArgs = $CustomArgs
}

$Timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
$Divider = "=" * 80

Add-Content -Path $LogFile -Value $Divider
Add-Content -Path $LogFile -Value "[START]  [$Timestamp] Windows 班轮自动定时航运开始..."
Add-Content -Path $LogFile -Value "[INFO]   工作区路径: $ArkDir"
Add-Content -Path $LogFile -Value "[CONFIG] 备份保留配额: 最近 $RetentionCount 个版本"
Add-Content -Path $LogFile -Value "[EXEC]   执行程序: $ArkBin $($BackupArgs -join ' ')"

$StartTime = Get-Date

# 3. 切换至工作区执行
Set-Location -Path $ArkDir

$ProcessInfo = New-Object System.Diagnostics.ProcessStartInfo
$ProcessInfo.FileName = $ArkBin
$ProcessInfo.Arguments = ($BackupArgs -join " ")
$ProcessInfo.RedirectStandardOutput = $true
$ProcessInfo.RedirectStandardError = $true
$ProcessInfo.UseShellExecute = $false
$ProcessInfo.CreateNoWindow = $true

$Process = New-Object System.Diagnostics.Process
$Process.StartInfo = $ProcessInfo

$Process.Start() | Out-Null
$Stdout = $Process.StandardOutput.ReadToEnd()
$Stderr = $Process.StandardError.ReadToEnd()
$Process.WaitForExit()

$ExitCode = $Process.ExitCode
$EndTime = Get-Date
$Duration = [math]::Round(($EndTime - $StartTime).TotalSeconds, 2)

if ($Stdout) {
    Add-Content -Path $LogFile -Value $Stdout.Trim()
}
if ($Stderr) {
    Add-Content -Path $LogFile -Value "[STDERR] $Stderr".Trim()
}

$FinishTimestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
if ($ExitCode -eq 0) {
    Add-Content -Path $LogFile -Value "[FINISH] [$FinishTimestamp] ✓ 定时备份圆满成功！(耗时: $Duration 秒)"
    Write-Host "[✓] Ark 定时备份成功完成！(耗时: $Duration 秒)" -ForegroundColor Green

    # 核心：顺带彻底清除远端未打标孤立版本 (untagged)，杜绝远端无标签悬空镜像堆积
    $CleanTimestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    Add-Content -Path $LogFile -Value "[CLEAN]  [$CleanTimestamp] 正在顺带扫描并清理远端孤立未打标版本 (Untagged Versions)..."

    $CleanProcessInfo = New-Object System.Diagnostics.ProcessStartInfo
    $CleanProcessInfo.FileName = $ArkBin
    $CleanProcessInfo.Arguments = "clean --untagged"
    $CleanProcessInfo.RedirectStandardOutput = $true
    $CleanProcessInfo.RedirectStandardError = $true
    $CleanProcessInfo.UseShellExecute = $false
    $CleanProcessInfo.CreateNoWindow = $true

    $CleanProcess = New-Object System.Diagnostics.Process
    $CleanProcess.StartInfo = $CleanProcessInfo
    $CleanProcess.Start() | Out-Null
    $CleanStdout = $CleanProcess.StandardOutput.ReadToEnd()
    $CleanStderr = $CleanProcess.StandardError.ReadToEnd()
    $CleanProcess.WaitForExit()

    if ($CleanStdout) {
        Add-Content -Path $LogFile -Value $CleanStdout.Trim()
    }
    if ($CleanStderr) {
        Add-Content -Path $LogFile -Value "[STDERR] $CleanStderr".Trim()
    }
    Add-Content -Path $LogFile -Value "[CLEAN]  [$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')] ✓ 远端未打标版本 (untagged) 清理巡检完毕！"
} else {
    Add-Content -Path $LogFile -Value "[ERROR]  [$FinishTimestamp] ✗ 定时备份执行失败 (退出码: $ExitCode, 耗时: $Duration 秒)"
    Write-Error "[✗] Ark 定时备份失败，退出码: $ExitCode，详见日志: $LogFile"
}

Add-Content -Path $LogFile -Value $Divider
exit $ExitCode
