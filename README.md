# tcd-beyond-compare-code-skik

一个给 Codex / Claude Code / OpenCode 配套使用的本地代码审阅台与 skill。

它解决的问题很直接：**不要把大段 diff 挤在聊天窗口里，而是打开一个更像 Beyond Compare 的本地审阅界面，让人类先看重点、再点回具体代码行。**

## 它是什么

- 一个很轻的本地审阅服务：Go 单二进制 + 内嵌 HTML
- 一个面向 AI 助手的 skill：把“生成 diff 解释”收口成“打开本地人工审阅台”
- 适合代码、文案、配置等 before/after 对照审阅

## 它不是什么

- 不是完整 Git GUI
- 不是多人协作审阅平台
- 不是云端托管服务

## 核心特点

- **本地优先**：直接读本地 before / after 路径
- **并排对照**：旧版 / 新版同屏展示
- **重点提示**：右侧只突出更值得先看的高风险改动
- **低风险不报警**：普通机械改动不故意刷存在感
- **点击跳转**：右侧提示可跳到中间具体行
- **零外部前端依赖**：HTML/CSS/JS 全内嵌

## 目录结构

```text
.
├── cmd/tcd-beyond-compare/
│   ├── main.go
│   └── index.html
├── scripts/
│   └── open_beyond_compare.sh
├── SKILL.md
├── .gitignore
├── go.mod
└── LICENSE
```

## 快速开始

### 1) 启动本地审阅台

```bash
git clone git@github.com:kcd-dev/tcd-beyond-compare-code-skik.git
cd tcd-beyond-compare-code-skik

bash scripts/open_beyond_compare.sh /abs/before /abs/after
```

默认会在浏览器打开：

```text
http://127.0.0.1:18767
```

如果只想启动服务、不自动打开浏览器：

```bash
bash scripts/open_beyond_compare.sh /abs/before /abs/after --no-open
```

### 2) 手动构建

```bash
go build -o .local/tcd-beyond-compare ./cmd/tcd-beyond-compare
.local/tcd-beyond-compare
```

要求：

- Go 1.22+
- macOS / Linux shell 环境
- 浏览器可访问 `http://127.0.0.1:18767`

## Skill 用法

这个仓库附带 `SKILL.md`，目标是让 AI 助手优先：

1. 打开本地审阅台
2. 给出“先看哪里”的重点
3. 只把高风险改动标红

而不是把所有 diff 原样塞回聊天窗口。

## 适合的使用场景

- “把这次代码改动像 Beyond Compare 一样打开”
- “我要人工审核，不要只给我摘要”
- “帮我看 before / after 哪些地方风险高”
- “对外文案改完后，我想先做人眼复核”

## 隐私与边界

- 默认是**本地运行**
- 输入是你自己提供的本地文件或目录路径
- 仓库本身不包含上传、云同步、账号系统

但仍建议：

- 不要把生产密钥、token、cookie、数据库密码直接放进测试样例
- 对外截图前，先确认路径、邮箱、域名等敏感信息已脱敏

## FAQ

### 它和 Git diff / VS Code diff 有什么区别？

Git diff 更偏原始差异；这个工具更偏**“给人类审阅 AI 改动”**，会额外给重点提示与定位说明。

### 为什么不是所有新增内容都标红？

因为“新版内容”和“高风险内容”不是一回事。这个工具当前设计是：**低风险差异不标，只有更值得先审的位置才红。**

### 适合谁？

- 经常让 AI 改代码、改文案、改配置的人
- 想做最后一轮人工复核的人
- 想把“差异”变成“审阅重点”的人

### 不适合谁？

- 需要完整 merge / 3-way diff / blame / commit history 的场景
- 需要 SaaS 审批流、评论流、多人在线协作的团队

## 后续可扩展方向

- Git 提交范围直接导入
- 更细的风险规则与语言识别
- 静态 HTML 导出模板
- 更好的文章审校模式

## License

MIT
