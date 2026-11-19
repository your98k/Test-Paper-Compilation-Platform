# Windows packaging

From the `windows` directory, generate the icon and native resources, then build two app payloads and package them:

```sh
python3 setup/resources.py  # optional if app.ico and both .syso are already present
for arch in amd64 arm64; do
  mkdir -p "/tmp/k12-release/$arch"
  CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -p 1 -trimpath -buildvcs=false \
    -ldflags '-s -w -H windowsgui' -o "/tmp/k12-release/$arch/K12QuestionBank.exe" . || exit
 done
MAKENSIS=/path/to/linux/makensis sh setup/package.sh /tmp/k12-release /tmp/k12-release
```

For the released x64-only package, build only `amd64` in that loop and add `--x64` to the `package.sh` command. Without that flag the script requires both architectures.

The resulting installer is `K12QuestionBank-Setup.exe`. MakeNSIS needs a matching NSIS data tree (`NSISDIR`, including Unicode x86 stubs, System plugin, headers and SimpChinese language). The executable and uninstall stub run as the current user; the setup selects the native payload using `IsWow64Process2`, falling back to `GetNativeSystemInfo` on earlier Windows 10. The application is installed to `%LOCALAPPDATA%\Programs\your98k\K12QuestionBank`; the Start Menu link and Installed Apps entry are current-user only. Uninstall removes only the two installed EXEs, a link, and the uninstall registration (plus the install directory if empty). No user data is removed. The installer is unsigned.

`resources.py` uses Python 3 with Pillow and librsvg (`rsvg-convert` if present; library fallback on Alpine), and reuses the repository's SVG icon. `RT_GROUP_ICON` ID 101 is the app icon; code loading its own window class icon can load ID 101 from its EXE, otherwise Explorer/shortcut can still display it. `RT_MANIFEST` ID 1 declares `asInvoker`, the Windows 10 compatibility GUID (also applies to Windows 11), `PerMonitorV2` with per-monitor fallback, and the OS-provided Common Controls v6 side-by-side assembly. No resource sidecars or redistributables are needed. Handle `WM_DPICHANGED` in the app; avoid overriding the declared process DPI awareness. Do not link another overlapping resource object.

Build and packaging don't replace physical Windows tests: on Windows x64 and ARM64, install under a standard user, inspect Start Menu and Settings → Installed apps, launch and check the selected native architecture, switch monitor scale, uninstall, and check that a test user-created file and settings survive.
