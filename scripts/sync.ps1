<#
.SYNOPSIS
    Ark 班轮自动化定时同步交付脚本 (Windows / PowerShell)

.DESCRIPTION
    适用于 Windows 操作系统下的定时任务计划 (Task Scheduler) 或手动一键快速同步交付。
    具备工作区自寻址、带时间戳日志输出、小磁盘极致干净模式与错误捕获。
    自动按配额保留航次个数 ($env:ARK_RETENTION_COUNT 或 --keep N，默认 5 个)，
    并在交付完成后顺带自动清除远端未打标孤立版本 (untagged)，杜绝远端镜像堆积。

.EXAMPLE
    # 手动运行测试:
    powershell -ExecutionPolicy Bypass -File .\scripts\sync.ps1

.EXAMPLE
    # Windows 任务计划程序一键注册命令 (每日凌晨 03:00 自动执行):
    $Action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument "-ExecutionPolicy Bypass -File F:\workspace\ark\scripts\sync.ps1"
    $Trigger = New-ScheduledTaskTrigger -Daily -At 3am
    Register-ScheduledTask -TaskName "ArkDailySync" -Action $Action -Trigger $Trigger -Description "Ark 每日自动航次交付与远端维护" -User "SYSTEM"
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
$LogFile = Join-Path $LogDir "sync.log"

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
            $key = $parts[0].Trim()
            $val = $parts[1].Trim().Trim('"').Trim("'")
            if (-not [Environment]::GetEnvironmentVariable($key)) {
                [Environment]::SetEnvironmentVariable($key, $val, "Process")
            }
        }
    }
}

$FinalArgs = @("board")
if ($CustomArgs -and $CustomArgs.Count -gt 0) {
    $FinalArgs += $CustomArgs
} else {
    # 默认采用生产推荐策略: 按天精度打标 + 全量重置本地缓存
    $FinalArgs += @("day", "--clean-all")
}

# 3. 日志记录函数
function Write-Log {
    param([string]$Message)
    $ts = (Get-Date).ToString("yyyy-MM-dd HH:mm:ss")
    $line = "[$ts] $Message"
    Write-Host $line
    Add-Content -Path $LogFile -Value $line -Encoding UTF8
}

Write-Log "================================================================="
Write-Log "⚓ 正在启动 Ark 自动化定时同步交付任务 (Windows Engine)..."
Write-Log "================================================================="
Write-Log "-> 执行指令: $ArkBin $($FinalArgs -join ' ')"

# 4. 执行任务
Push-Location $ArkDir
try {
    & $ArkBin @FinalArgs 2>&1 | Tee-Object -FilePath $LogFile -Append
    if ($LASTEXITCODE -eq 0) {
        Write-Log "✓ 自动化同步交付任务执行成功！"
    } else {
        Write-Log "[-] 任务异常终止，退出码: $LASTEXITCODE，详情请查阅日志。"
    }
} catch {
    Write-Log "[-] 执行发生系统未捕获异常: $_"
} finally {
    Pop-Location
}

# 5. 日志自动滚动截断保护 (保留最新 5000 行)
if (Test-Path $LogFile) {
    $lines = Get-Content $LogFile
    if ($lines.Count -gt 5000) {
        $lines | Select-Object -Last 5000 | Set-Content $LogFile -Encoding UTF8
        Write-Log "💡 日志已自动截断至最新 5000 行。"
    }
}

Write-Log "⚓ 本次任务结束。"
Write-Log "================================================================="
