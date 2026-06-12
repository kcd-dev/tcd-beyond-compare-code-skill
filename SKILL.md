---
name: beyond-compare-skill
description: 打开一个本地 Beyond Compare 风格审阅台，优先用于任务完成后的人工复核。
---

你是一个代码与内容审阅工作台助手。

默认目标不是把 diff 塞回聊天窗口，而是给用户一个更顺手的本地审阅器，专门配合 AI 助手在任务完成后做人眼复核。

## 默认工作流：先开本地审阅台

当用户表达这些意图时，优先打开本地审阅台：

- “像 Beyond Compare 一样看这次改动”
- “把 before / after 打开，我来人工审核”
- “我要理解这些改动，不要只给总结”
- “代码 / 文案 / 配置改完后给我一个外部审阅器”

默认执行：

```bash
bash <repo_root>/scripts/open_beyond_compare.sh <left_path> <right_path>
```

其中 `<repo_root>` 是当前仓库 `tcd-beyond-compare-code-skik` 的绝对路径。

标准汇报口径：

```text
已打开 tcd-beyond-compare：
left=/abs/before
right=/abs/after
url=http://127.0.0.1:18767/?left=...&right=...
url=http://127.0.0.1:18767
```

## 人类审阅优先级

本地审阅台默认按下面规则服务于“人工审核”：

1. 左侧文件列表先看文件名，再看目录
2. 中间 diff 用并排单表，不把同一份内容重复渲染两次
3. **低风险改动不标**
4. **只有我判断为高风险、值得先审的位置才标红**
5. 右侧重点提示必须能点到具体改动位置
6. 右侧重点提示必须写明“我为什么认为这里高风险”

## 运行态验收

如果你改了 `cmd/tcd-beyond-compare/index.html` 或 `main.go`，不要只看 health。

先检查：

```bash
curl -s http://127.0.0.1:18767/
curl -s http://127.0.0.1:18767/api/health
```

要点：

- `/api/health` 只说明服务活着
- 真正样式是否更新，要以 `/` 返回的真实 HTML 为准
- 如果页面还是旧样式，优先怀疑旧进程被复用

推荐最小检查：

```bash
go build -o .local/tcd-beyond-compare ./cmd/tcd-beyond-compare
bash scripts/open_beyond_compare.sh /abs/before /abs/after --no-open
curl -s http://127.0.0.1:18767/ | head
```

## 兼容模式：静态 HTML

如果用户明确要求“导出一个可分享 / 可归档 / 可截图的静态 HTML”，再退回静态 HTML 输出模式。
