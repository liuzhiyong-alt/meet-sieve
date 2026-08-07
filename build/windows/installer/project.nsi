Unicode true

####
## Please note: Template replacements don't work in this file. They are provided with default defines like
## mentioned underneath.
## If the keyword is not defined, "wails_tools.nsh" will populate them with the values from ProjectInfo.
## If they are defined here, "wails_tools.nsh" will not touch them. This allows to use this project.nsi manually
## from outside of Wails for debugging and development of the installer.
##
## For development first make a wails nsis build to populate the "wails_tools.nsh":
## > wails build --target windows/amd64 --nsis
## Then you can call makensis on this file with specifying the path to your binary:
## For a AMD64 only installer:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app.exe
## For a ARM64 only installer:
## > makensis -DARG_WAILS_ARM64_BINARY=..\..\bin\app.exe
## For a installer with both architectures:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app-amd64.exe -DARG_WAILS_ARM64_BINARY=..\..\bin\app-arm64.exe
####
## The following information is taken from the ProjectInfo file, but they can be overwritten here.
####
## !define INFO_PROJECTNAME    "MyProject" # Default "{{.Name}}"
## !define INFO_COMPANYNAME    "MyCompany" # Default "{{.Info.CompanyName}}"
## !define INFO_PRODUCTNAME    "MyProduct" # Default "{{.Info.ProductName}}"
## !define INFO_PRODUCTVERSION "1.0.0"     # Default "{{.Info.ProductVersion}}"
## !define INFO_COPYRIGHT      "Copyright" # Default "{{.Info.Copyright}}"
###
## !define PRODUCT_EXECUTABLE  "Application.exe"      # Default "${INFO_PROJECTNAME}.exe"
## !define UNINST_KEY_NAME     "UninstKeyInRegistry"  # Default "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
####
## !define REQUEST_EXECUTION_LEVEL "admin"            # Default "admin"  see also https://nsis.sourceforge.io/Docs/Chapter4.html
####
## Include the wails tools
####
!include "..\..\bin\windows-resources\meetsieve_build.nsh"
!define INFO_PRODUCTVERSION "${MEETSIEVE_BUILD_VERSION}"
!include "wails_tools.nsh"
!include "LogicLib.nsh"
!include "StrFunc.nsh"
${Using:StrFunc} StrStr

!define MEETSIEVE_INSTANCE_MUTEX "Global\MeetSieve.App.Instance.v1"
!define MEETSIEVE_INSTANCE_RUNNING_MESSAGE "MeetSieve 正在运行，请先结束会议并退出应用后再继续。"
!define MEETSIEVE_FIREWALL_RULE "MeetSieve LAN Private"
!define MEETSIEVE_INSTALL_MARKER "meetsieve-install.json"
!define MEETSIEVE_FILE_MANIFEST "meetsieve-files.json"

!macro EnsureMeetSieveNotRunning
    System::Call 'kernel32::OpenMutexW(i 0x00100000, i 0, w "${MEETSIEVE_INSTANCE_MUTEX}") p .r0'
    IntCmp $0 0 +4
    System::Call 'kernel32::CloseHandle(p r0)'
    MessageBox MB_ICONEXCLAMATION|MB_OK "${MEETSIEVE_INSTANCE_RUNNING_MESSAGE}"
    Abort
!macroend

# The version information for this two must consist of 4 parts
VIProductVersion "${MEETSIEVE_FILE_VERSION}"
VIFileVersion    "${MEETSIEVE_FILE_VERSION}"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
# !define MUI_WELCOMEFINISHPAGE_BITMAP "resources\leftimage.bmp" #Include this to add a bitmap on the left side of the Welcome Page. Must be a size of 164x314
!define MUI_FINISHPAGE_NOAUTOCLOSE # Wait on the INSTFILES page so the user can take a look into the details of the installation steps
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.

!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
# !insertmacro MUI_PAGE_LICENSE "resources\eula.txt" # Adds a EULA page to the installer
!define MUI_PAGE_CUSTOMFUNCTION_LEAVE ValidateInstallDirectory
!insertmacro MUI_PAGE_DIRECTORY # In which folder install page.
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_INSTFILES # Installing page.
!insertmacro MUI_PAGE_FINISH # Finished installation page.

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_UNPAGE_FINISH

!insertmacro MUI_LANGUAGE "SimpChinese"

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\MeetSieve-${MEETSIEVE_BUILD_VERSION}-windows-amd64-installer.exe"
InstallDir "$PROGRAMFILES64\${INFO_PRODUCTNAME}"
InstallDirRegKey HKLM "${UNINST_KEY}" "InstallLocation"
ShowInstDetails show # This will always show the installation details.
ShowUninstDetails show

Function .onInit
    !insertmacro EnsureMeetSieveNotRunning
    !insertmacro wails.checkArchitecture
FunctionEnd

Function ValidateInstallDirectory
    GetFullPathName $INSTDIR "$INSTDIR"
    StrLen $0 "$INSTDIR"
    IntCmp $0 3 invalid invalid valid_path

    valid_path:
    StrCmp "$INSTDIR" "$WINDIR" invalid
    StrCmp "$INSTDIR" "$SYSDIR" invalid
    StrCmp "$INSTDIR" "$PROGRAMFILES" invalid
    StrCmp "$INSTDIR" "$PROGRAMFILES32" invalid
    StrCmp "$INSTDIR" "$PROGRAMFILES64" invalid

    IfFileExists "$INSTDIR\${MEETSIEVE_INSTALL_MARKER}" validate_marker check_empty

    validate_marker:
    ClearErrors
    FileOpen $0 "$INSTDIR\${MEETSIEVE_INSTALL_MARKER}" r
    IfErrors invalid_nonempty
    FileRead $0 $1
    FileClose $0
    ${StrStr} $2 $1 '"product_id":"meet-sieve"'
    StrCmp $2 "" invalid_nonempty
    ${StrStr} $2 $1 '"schema_version":1'
    StrCmp $2 "" invalid_nonempty valid

    check_empty:
    ClearErrors
    FindFirst $0 $1 "$INSTDIR\*.*"
    IfErrors valid

    inspect_entry:
    StrCmp $1 "" empty_directory
    StrCmp $1 "." next_entry
    StrCmp $1 ".." next_entry
    FindClose $0
    Goto invalid_nonempty

    next_entry:
    FindNext $0 $1
    Goto inspect_entry

    empty_directory:
    FindClose $0
    Goto valid

    invalid:
    MessageBox MB_ICONEXCLAMATION|MB_OK "请选择 MeetSieve 专属安装目录，不能使用磁盘根目录或公共系统目录。"
    Abort

    invalid_nonempty:
    MessageBox MB_ICONEXCLAMATION|MB_OK "所选目录包含未知文件，且不是可识别的 MeetSieve 安装目录。请选择新目录或空目录。"
    Abort

    valid:
FunctionEnd

Function EnsureWebView2Installed
    SetRegView 64
    ReadRegStr $0 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
    StrCmp $0 "" webview_missing webview_ok

    webview_missing:
    MessageBox MB_ICONSTOP|MB_OK "Microsoft Edge WebView2 Runtime 安装失败，MeetSieve 尚未完整安装。"
    SetErrorLevel 1
    Abort

    webview_ok:
FunctionEnd

Section "MeetSieve 核心程序" SEC_CORE
    SectionIn RO
    !insertmacro wails.setShellContext
    Call ValidateInstallDirectory

    !insertmacro wails.webview2runtime
    Call EnsureWebView2Installed

    SetOutPath $INSTDIR

    !insertmacro wails.files
    File "/oname=onnxruntime.dll" "..\..\bin\windows-resources\onnxruntime.dll"
    File "/oname=ONNXRUNTIME-LICENSE.txt" "..\..\bin\windows-resources\ONNXRUNTIME-LICENSE.txt"
    SetOutPath "$INSTDIR\models"
    File "/oname=voice-matching-profile.json" "..\..\bin\windows-resources\models\voice-matching-profile.json"
    SetOutPath $INSTDIR
    File "/oname=meetsieve-install.json" "..\..\bin\windows-resources\meetsieve-install.json"
    File "/oname=meetsieve-files.json" "..\..\bin\windows-resources\meetsieve-files.json"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller
    SetRegView 64
    WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
SectionEnd

Section "开始菜单快捷方式" SEC_START_MENU
    SectionIn RO
    !insertmacro wails.setShellContext
    CreateDirectory "$SMPROGRAMS\${INFO_PRODUCTNAME}"
    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
SectionEnd

Section "桌面快捷方式" SEC_DESKTOP
    !insertmacro wails.setShellContext
    CreateShortcut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
SectionEnd

Section "局域网访客防火墙规则" SEC_FIREWALL
    nsExec::ExecToLog 'netsh advfirewall firewall delete rule name="${MEETSIEVE_FIREWALL_RULE}"'
    Pop $0
    nsExec::ExecToLog 'netsh advfirewall firewall add rule name="${MEETSIEVE_FIREWALL_RULE}" dir=in action=allow profile=private protocol=TCP program="$INSTDIR\${PRODUCT_EXECUTABLE}" enable=yes'
    Pop $0
    StrCmp $0 "0" firewall_ok
    MessageBox MB_ICONSTOP|MB_OK "创建局域网访客专用网络防火墙规则失败，安装未完整完成。"
    SetErrorLevel 1
    Abort

    firewall_ok:
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    nsExec::ExecToLog 'netsh advfirewall firewall delete rule name="${MEETSIEVE_FIREWALL_RULE}" program="$INSTDIR\${PRODUCT_EXECUTABLE}"'
    Pop $0

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}\${INFO_PRODUCTNAME}.lnk"
    RMDir "$SMPROGRAMS\${INFO_PRODUCTNAME}"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    Delete "$INSTDIR\onnxruntime.dll"
    Delete "$INSTDIR\ONNXRUNTIME-LICENSE.txt"
    Delete "$INSTDIR\models\voice-matching-profile.json"
    Delete "$INSTDIR\meetsieve-install.json"
    Delete "$INSTDIR\meetsieve-files.json"
    RMDir "$INSTDIR\models"

    IfFileExists "$INSTDIR\${PRODUCT_EXECUTABLE}" uninstall_failed
    IfFileExists "$INSTDIR\onnxruntime.dll" uninstall_failed
    IfFileExists "$INSTDIR\ONNXRUNTIME-LICENSE.txt" uninstall_failed
    IfFileExists "$INSTDIR\models\voice-matching-profile.json" uninstall_failed
    Goto uninstall_files_removed

    uninstall_failed:
    MessageBox MB_ICONSTOP|MB_OK "部分 MeetSieve 程序文件无法删除。卸载入口已保留，请关闭占用文件的程序后重试。"
    SetErrorLevel 1
    Abort

    uninstall_files_removed:

    !insertmacro wails.deleteUninstaller
    RMDir "$INSTDIR"
SectionEnd

Function un.onInit
    !insertmacro EnsureMeetSieveNotRunning
FunctionEnd
