# 组卷台 · Windows 原生版 1.0.0

## 安装和直连接口

1. 把 `K12QuestionBank-Setup.exe` 传到 Windows 电脑并运行。默认安装到当前用户的 `%LOCALAPPDATA%\Programs\your98k\K12QuestionBank`，无需管理员权限。从开始菜单搜索「组卷台」。
2. 本地离线组卷不需要 API。用 AI 时打开「API 设置」，填 OpenAI 兼容 HTTPS 地址（例如 `https://api.openai.com/v1`）、模型 ID、API Key，点「测试连接」，成功后点「保存」。
3. 把「出题方式」切换到「AI 原创新题」，选年级和知识点再生成。**测试连接和 AI 出题都会向你填写的服务商发请求，服务商可按量计费。**

API 请求由应用直接发送，不走网页、浏览器跨域或 Worker；支持 `/v1` 基址或完整 `/chat/completions` 地址，不跟随重定向。即使不受 CORS 限制，Windows 仍须能访问该地址。只支持 OpenAI 兼容 Chat Completions；不支持 Responses-only 接口。HTTPS 以外只允许本机回环地址。

API Key 默认只在本次运行中有效。勾选「在此 Windows 帐户加密记住 Key」才由 Windows DPAPI 加密保存。设置和错题文件位于 `%LOCALAPPDATA%\K12QuestionBank\Data`。卸载保留它们；网页错题本不会自动同步到桌面版。

## 功能和边界

- 五门学科：数学、语文、英语、物理、化学。支持按年级、学期、知识点和难度选题，每卷 1–40 题。
- 离线题来自原版题目生成器在构建期抽样得到的题库，不依赖网络或 Node.js；部分知识点只支持 AI，选题量超过细分题池时可扩大范围。
- 单题换新、答案与解析、独立错题本、复制、Word `.docx`、Windows 打印。导出 PDF 时在打印机里选「Microsoft Print to PDF」。
- 本版用原生文本显示常见数学表达式，不引入网页公式渲染器；不完整复刻网页版。AI 生成的是原创练习，使用前核对题目与答案。

## 下载与后续更新

安装包、源码与校验和统一在本仓库的 [Releases](https://github.com/your98k/Test-Paper-Compilation-Platform/releases) 按版本发布，版本标签使用 `windows-vX.Y.Z`。后续每次桌面版更新都发布新版本并保留历史包；软件本身不在后台自动安装更新。发布流程见 [CHANGELOG.md](CHANGELOG.md)。

## 构建

- Windows 客户端使用 Go 1.23+ 和 Win32 原生控件，没有 Electron、WebView 或 .NET 运行时依赖。
- 源码以 `your98k/k12-ai-question-bank` 的 `aef4583` 为基线，网页源码保留。复建见 `windows/setup/README.md`，`cd windows` 后运行 `go test ./...`。
