# 组卷台 Windows 1.0.0

首个 Windows 原生桌面版本，适用 Windows 10 / 11 x64。

- Go + Win32 原生界面，不依赖 Electron、WebView、Node.js 或 .NET 运行时。
- 直连用户填写的 OpenAI 兼容 Chat Completions API，无需浏览器 CORS 或 Worker。
- 数学、语文、英语、物理、化学；内置原有生成器构建期抽样的 56,126 道离线题。
- 年级、学期、知识点、难度和题量筛选，单题换新、答案解析、独立错题本。
- Word、复制文本及 Windows 打印 / Microsoft Print to PDF。
- API Key 默认仅在内存中，可选 Windows DPAPI 加密保存。
- 当前用户安装、开始菜单入口、标准卸载；卸载保留设置、错题与用户文件。

安装文件：`K12QuestionBank-Setup.exe`；源码：`K12QuestionBank-Source.zip`；完整性校验：`SHA256SUMS.txt`。

## 验证和已知边界

发布流程运行 Linux / Windows 模块测试，并在 Windows 验证安装、载荷哈希、主窗口启动、关闭和卸载。不调用真实付费 API；不等于全部交互功能的人工验收。

安装包未做代码签名。离线题是有限抽样，不是全部生成器的运行时移植；部分知识点只支持 AI。数学公式采用文本表达，不完整复刻网页公式排版。网页错题本不会自动同步到桌面。

## 后续版本发布规则

源码与变更记录统一保存在本仓库。每次桌面版优化先提高 Windows 版本号，同步清单和资源，执行测试，再推送 `windows-vX.Y.Z` 标签。GitHub Actions 自动测试、打包并发布对应 Releases（安装包、完整源码、说明、SHA256）；保留历史版本，不覆盖旧版本附件。仅推 main 会执行构建检查，不创建正式发行版。
