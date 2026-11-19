param([Parameter(Mandatory = $true)][string]$DistDir)
$ErrorActionPreference = 'Stop'
$installer = (Resolve-Path (Join-Path $DistDir 'K12QuestionBank-Setup.exe')).Path
$payload = (Resolve-Path (Join-Path $DistDir 'payload/amd64/K12QuestionBank.exe')).Path
$installDir = Join-Path $env:LOCALAPPDATA 'Programs/your98k/K12QuestionBank'
$appPath = Join-Path $installDir 'K12QuestionBank.exe'
$arp = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\your98k.K12QuestionBank'
$shortcut = Join-Path ([Environment]::GetFolderPath('Programs')) '组卷台.lnk'

$p = Start-Process -FilePath $installer -ArgumentList '/S' -PassThru -Wait
if ($p.ExitCode -ne 0 -or !(Test-Path $appPath)) { throw 'Silent installation failed.' }
if ((Get-FileHash $payload).Hash -ne (Get-FileHash $appPath).Hash) { throw 'Installed executable does not match the built payload.' }
if (!(Test-Path $arp) -or !(Test-Path $shortcut)) { throw 'Uninstall registration or Start Menu shortcut is missing.' }
$sentinel = Join-Path $installDir 'smoke-user-file.txt'
Set-Content -Path $sentinel -Value 'uninstall must preserve user files'
$app = Start-Process -FilePath $appPath -PassThru
try {
    $deadline = [DateTime]::UtcNow.AddSeconds(25)
    do {
        Start-Sleep -Milliseconds 250
        $app.Refresh()
        if ($app.HasExited) { throw "Application exited unexpectedly: $($app.ExitCode)" }
    } while ($app.MainWindowHandle -eq 0 -and [DateTime]::UtcNow -lt $deadline)
    if ($app.MainWindowHandle -eq 0) { throw 'Native main window was not created.' }
    if (!$app.Responding) { throw 'Native main window is not responding.' }
    Write-Host "Native window created: $($app.MainWindowTitle)"
    if (!$app.CloseMainWindow()) { throw 'Native window rejected the close request.' }
    if (!$app.WaitForExit(10000)) { throw 'Application did not close.' }
    if ($app.ExitCode -ne 0) { throw "Application close failed: $($app.ExitCode)" }
} finally {
    if (!$app.HasExited) { Stop-Process -Id $app.Id -Force }
}
$uninstallPath = Join-Path $installDir 'Uninstall.exe'
$u = Start-Process -FilePath $uninstallPath -ArgumentList '/S' -PassThru -Wait
$deadline = [DateTime]::UtcNow.AddSeconds(20)
while ((Test-Path $appPath) -and [DateTime]::UtcNow -lt $deadline) { Start-Sleep -Milliseconds 250 }
if ((Test-Path $appPath) -or (Test-Path $arp) -or (Test-Path $shortcut)) { throw 'Uninstall did not remove owned files and registration.' }
if (!(Test-Path $sentinel)) { throw 'Uninstall deleted a user-created file.' }
Write-Host 'Install, payload hash, native launch, clean close and uninstall checks passed.'
