Unicode true
!include "x64.nsh"
!include "WinVer.nsh"

!define PRODUCT "组卷台"
!define PUBLISHER "your98k"
!define VERSION "1.0.0"
!define APP "K12QuestionBank.exe"
!define ARP "Software\Microsoft\Windows\CurrentVersion\Uninstall\your98k.K12QuestionBank"
!define DIR "$LOCALAPPDATA\Programs\your98k\K12QuestionBank"

Name "${PRODUCT}"
BrandingText "${PRODUCT} ${VERSION} · ${PUBLISHER}"
OutFile "${OUTPUT}"
InstallDir "${DIR}"
RequestExecutionLevel user
SetCompressor /SOLID lzma
Icon "${APP_ICON}"
UninstallIcon "${APP_ICON}"
VIProductVersion "1.0.0.0"
VIAddVersionKey /LANG=2052 "ProductName" "${PRODUCT}"
VIAddVersionKey /LANG=2052 "CompanyName" "${PUBLISHER}"
VIAddVersionKey /LANG=2052 "LegalCopyright" "${PUBLISHER}"
VIAddVersionKey /LANG=2052 "FileDescription" "${PRODUCT}安装程序"
VIAddVersionKey /LANG=2052 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=2052 "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=2052 "OriginalFilename" "K12QuestionBank-Setup.exe"

Page instfiles
UninstPage uninstConfirm
UninstPage instfiles
LoadLanguageFile "${NSISDIR}\Contrib\Language files\SimpChinese.nlf"

Var NativeMachine
Function .onInit
  SetShellVarContext current
  ; Fixed, per-user location: also prevents /D from redirecting our writes.
  StrCpy $INSTDIR "${DIR}"
  ${IfNot} ${AtLeastWin10}
    MessageBox MB_OK|MB_ICONSTOP "组卷台需要 Windows 10 或更高版本。" /SD IDOK
    SetErrorLevel 1633
    Abort
  ${EndIf}
  StrCpy $NativeMachine 0
  ; IsWow64Process2 returns the native host, not the x86 NSIS process architecture.
  ; On early x64 Windows 10 the API is not exported; GetNativeSystemInfo fallback.
  System::Call 'kernel32::GetCurrentProcess() p.r0'
  System::Call 'kernel32::IsWow64Process2(p r0, *i .r1, *i .r2) i.r3 ?e'
  Pop $4
  ${If} $3 == 1
    IntOp $NativeMachine $2 & 0xffff
  ${Else}
    System::Alloc 64
    Pop $0
    System::Call 'kernel32::GetNativeSystemInfo(p r0)'
    System::Call '*$0(&i2 .r1)'
    System::Free $0
    ${If} $1 == 9
      StrCpy $NativeMachine 34404 ; IMAGE_FILE_MACHINE_AMD64
    ${ElseIf} $1 == 12
      StrCpy $NativeMachine 43620 ; IMAGE_FILE_MACHINE_ARM64
    ${EndIf}
  ${EndIf}
  ${If} $NativeMachine != 34404
  !ifdef ARM64_PAYLOAD
  ${AndIf} $NativeMachine != 43620
    MessageBox MB_OK|MB_ICONSTOP "组卷台只支持 x64 或 ARM64 版 Windows。" /SD IDOK
  !else
    MessageBox MB_OK|MB_ICONSTOP "这个安装包需要 x64 版 Windows。" /SD IDOK
  !endif
    SetErrorLevel 1633
    Abort
  ${EndIf}
FunctionEnd

Section "${PRODUCT}"
  SetOutPath "$INSTDIR"
  SetOverwrite on
  ClearErrors
  !ifdef ARM64_PAYLOAD
  ${If} $NativeMachine == 43620
    File /oname=${APP} "${ARM64_PAYLOAD}"
  ${Else}
    File /oname=${APP} "${AMD64_PAYLOAD}"
  ${EndIf}
  !else
  File /oname=${APP} "${AMD64_PAYLOAD}"
  !endif
  IfErrors install_failed
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  IfErrors install_failed
  CreateShortcut "$SMPROGRAMS\${PRODUCT}.lnk" "$INSTDIR\${APP}" "" "$INSTDIR\${APP}" 0
  IfErrors install_failed
  WriteRegStr HKCU "${ARP}" "DisplayName" "${PRODUCT}"
  WriteRegStr HKCU "${ARP}" "Publisher" "${PUBLISHER}"
  WriteRegStr HKCU "${ARP}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${ARP}" "DisplayIcon" '"$INSTDIR\${APP}",0'
  WriteRegStr HKCU "${ARP}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${ARP}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKCU "${ARP}" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  WriteRegDWORD HKCU "${ARP}" "NoModify" 1
  WriteRegDWORD HKCU "${ARP}" "NoRepair" 1
  IfErrors install_failed
  Goto install_done
install_failed:
  SetErrorLevel 1
  Abort "安装未完成。请关闭组卷台后重试。"
install_done:
SectionEnd

Function un.onInit
  SetShellVarContext current
  StrCpy $INSTDIR "${DIR}"
FunctionEnd

Section "Uninstall"
  ; No recursive delete and no application data or settings registry deletion.
  IfFileExists "$INSTDIR\${APP}" 0 remove_registration
  ClearErrors
  Delete "$INSTDIR\${APP}"
  IfErrors uninstall_failed
remove_registration:
  Delete "$SMPROGRAMS\${PRODUCT}.lnk"
  DeleteRegKey HKCU "${ARP}"
  Delete "$INSTDIR\Uninstall.exe"
  SetOutPath "$TEMP"
  RMDir "$INSTDIR"
  Goto uninstall_done
uninstall_failed:
  SetErrorLevel 1
  Abort "请关闭组卷台后重试卸载。"
uninstall_done:
SectionEnd
