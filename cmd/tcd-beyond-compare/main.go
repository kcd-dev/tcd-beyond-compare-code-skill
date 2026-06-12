package main

import (
	"crypto/sha1"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed index.html
var webFS embed.FS

type analyzeRequest struct {
	LeftPath  string `json:"left_path"`
	RightPath string `json:"right_path"`
}

type fileSummary struct {
	Key          string   `json:"key"`
	Path         string   `json:"path"`
	Mode         string   `json:"mode"`
	Risk         string   `json:"risk"`
	Reason       string   `json:"reason"`
	Signals      []string `json:"signals"`
	ChangeCount  int      `json:"change_count"`
	AddedCount   int      `json:"added_count"`
	RemovedCount int      `json:"removed_count"`
}

type focusItem struct {
	Title    string `json:"title"`
	Severity string `json:"severity"`
	Text     string `json:"text"`
}

type lineView struct {
	LeftNo  int    `json:"left_no"`
	RightNo int    `json:"right_no"`
	Left    string `json:"left"`
	Right   string `json:"right"`
	Kind    string `json:"kind"`
	Hot     bool   `json:"hot"`
}

type lineNote struct {
	ID        string `json:"id"`
	LineIndex int    `json:"line_index"`
	LeftNo    int    `json:"left_no"`
	RightNo   int    `json:"right_no"`
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	Text      string `json:"text"`
	Snippet   string `json:"snippet"`
}

type diffDetail struct {
	Key          string      `json:"key"`
	Path         string      `json:"path"`
	Mode         string      `json:"mode"`
	Lines        []lineView  `json:"lines,omitempty"`
	Notes        []lineNote  `json:"notes,omitempty"`
	BeforeBlocks []string    `json:"before_blocks,omitempty"`
	AfterBlocks  []string    `json:"after_blocks,omitempty"`
	Focus        []focusItem `json:"focus"`
	Skip         []string    `json:"skip"`
	Decision     string      `json:"decision"`
}

type analyzeResponse struct {
	OK          bool          `json:"ok"`
	Detected    string        `json:"detected"`
	Signals     []string      `json:"signals"`
	Summary     string        `json:"summary"`
	Files       []fileSummary `json:"files"`
	Details     []diffDetail  `json:"details"`
	LeftPath    string        `json:"left_path"`
	RightPath   string        `json:"right_path"`
	RuntimeNote string        `json:"runtime_note"`
}

type filePair struct {
	RelPath     string
	LeftAbs     string
	RightAbs    string
	LeftExists  bool
	RightExists bool
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/analyze", handleAnalyze)
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/", serveIndex)

	addr := envOr("TCD_BEYOND_COMPARE_ADDR", "127.0.0.1:18767")
	log.Printf("tcd-beyond-compare 审阅台启动: http://%s\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(webFS, "index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", mime.TypeByExtension(".html"))
	_, _ = w.Write(data)
}

func handleAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	var req analyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请求体不是合法 JSON"})
		return
	}
	left := filepath.Clean(strings.TrimSpace(req.LeftPath))
	right := filepath.Clean(strings.TrimSpace(req.RightPath))
	if left == "" || right == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "left_path 和 right_path 都必填"})
		return
	}
	resp, err := analyzePaths(left, right)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func analyzePaths(left, right string) (*analyzeResponse, error) {
	leftInfo, err := os.Stat(left)
	if err != nil {
		return nil, fmt.Errorf("读取左侧路径失败: %w", err)
	}
	rightInfo, err := os.Stat(right)
	if err != nil {
		return nil, fmt.Errorf("读取右侧路径失败: %w", err)
	}
	if leftInfo.IsDir() != rightInfo.IsDir() {
		return nil, fmt.Errorf("左右路径类型不一致：一个是目录，一个是文件")
	}
	if !leftInfo.IsDir() {
		pair := filePair{RelPath: filepath.Base(right), LeftAbs: left, RightAbs: right, LeftExists: true, RightExists: true}
		return analyzeFilePairs([]filePair{pair}, left, right)
	}
	pairs, err := collectDirPairs(left, right)
	if err != nil {
		return nil, err
	}
	return analyzeFilePairs(pairs, left, right)
}

func collectDirPairs(leftDir, rightDir string) ([]filePair, error) {
	leftMap, err := collectFiles(leftDir)
	if err != nil {
		return nil, fmt.Errorf("扫描左侧目录失败: %w", err)
	}
	rightMap, err := collectFiles(rightDir)
	if err != nil {
		return nil, fmt.Errorf("扫描右侧目录失败: %w", err)
	}
	keysMap := map[string]struct{}{}
	for k := range leftMap {
		keysMap[k] = struct{}{}
	}
	for k := range rightMap {
		keysMap[k] = struct{}{}
	}
	keys := make([]string, 0, len(keysMap))
	for k := range keysMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]filePair, 0, len(keys))
	for _, key := range keys {
		l, lok := leftMap[key]
		r, rok := rightMap[key]
		pairs = append(pairs, filePair{RelPath: key, LeftAbs: l, RightAbs: r, LeftExists: lok, RightExists: rok})
	}
	return pairs, nil
}

func collectFiles(root string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == ".trash" || name == ".idea" || name == ".vscode" {
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !isTextLike(path) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = path
		return nil
	})
	return out, err
}

func analyzeFilePairs(pairs []filePair, leftRoot, rightRoot string) (*analyzeResponse, error) {
	summaries := make([]fileSummary, 0)
	details := make([]diffDetail, 0)
	globalSignals := map[string]struct{}{}
	modeCounter := map[string]int{}
	for _, pair := range pairs {
		detail, summary, changed, err := buildDetail(pair)
		if err != nil {
			return nil, err
		}
		if !changed {
			continue
		}
		summaries = append(summaries, summary)
		details = append(details, detail)
		modeCounter[summary.Mode]++
		for _, s := range summary.Signals {
			globalSignals[s] = struct{}{}
		}
	}
	if len(summaries) == 0 {
		return &analyzeResponse{
			OK:          true,
			Detected:    "empty",
			Signals:     []string{"未发现文本级差异"},
			Summary:     "左右输入没有检测到文本差异。",
			Files:       []fileSummary{},
			Details:     []diffDetail{},
			LeftPath:    leftRoot,
			RightPath:   rightRoot,
			RuntimeNote: "基础 diff 可用；当前没有需要后审计的改动。",
		}, nil
	}
	sort.Slice(summaries, func(i, j int) bool {
		if riskRank(summaries[i].Risk) == riskRank(summaries[j].Risk) {
			if summaries[i].ChangeCount == summaries[j].ChangeCount {
				return summaries[i].Path < summaries[j].Path
			}
			return summaries[i].ChangeCount > summaries[j].ChangeCount
		}
		return riskRank(summaries[i].Risk) > riskRank(summaries[j].Risk)
	})
	detailMap := map[string]diffDetail{}
	for _, d := range details {
		detailMap[d.Key] = d
	}
	sortedDetails := make([]diffDetail, 0, len(summaries))
	for _, s := range summaries {
		sortedDetails = append(sortedDetails, detailMap[s.Key])
	}
	detected := dominantMode(modeCounter)
	signals := keysOf(globalSignals)
	sort.Strings(signals)
	summaryText := fmt.Sprintf("共检测到 %d 个发生变化的对象；优先看 %s。", len(summaries), summaries[0].Path)
	return &analyzeResponse{
		OK:          true,
		Detected:    detected,
		Signals:     signals,
		Summary:     summaryText,
		Files:       summaries,
		Details:     sortedDetails,
		LeftPath:    leftRoot,
		RightPath:   rightRoot,
		RuntimeNote: "当前重点提示为规则化结果；即使没有外部 AI，也能稳定给出基础 diff 与优先级。",
	}, nil
}

func buildDetail(pair filePair) (diffDetail, fileSummary, bool, error) {
	leftText, err := readMaybe(pair.LeftAbs, pair.LeftExists)
	if err != nil {
		return diffDetail{}, fileSummary{}, false, fmt.Errorf("读取 %s 失败: %w", pair.RelPath, err)
	}
	rightText, err := readMaybe(pair.RightAbs, pair.RightExists)
	if err != nil {
		return diffDetail{}, fileSummary{}, false, fmt.Errorf("读取 %s 失败: %w", pair.RelPath, err)
	}
	if leftText == rightText {
		return diffDetail{}, fileSummary{}, false, nil
	}
	mode := detectMode(pair.RelPath, leftText, rightText)
	signals, risk, reason := detectSignals(pair.RelPath, leftText, rightText, mode, pair.LeftExists, pair.RightExists)
	key := hashKey(pair.RelPath + "\n" + pair.LeftAbs + "\n" + pair.RightAbs)
	changeCount := 0
	addedCount := 0
	removedCount := 0
	detail := diffDetail{Key: key, Path: pair.RelPath, Mode: mode}
	if mode == "article" {
		beforeBlocks := splitParagraphs(leftText)
		afterBlocks := splitParagraphs(rightText)
		detail.BeforeBlocks = limitStrings(beforeBlocks, 8)
		detail.AfterBlocks = limitStrings(afterBlocks, 8)
		addedCount = max(len(afterBlocks)-len(beforeBlocks), 0)
		removedCount = max(len(beforeBlocks)-len(afterBlocks), 0)
		changeCount = max(len(beforeBlocks), len(afterBlocks))
	} else {
		lines := buildLineViews(leftText, rightText)
		detail.Lines = limitLines(lines, 220)
		detail.Notes = buildLineNotes(pair.RelPath, detail.Lines, signals, risk)
		for _, line := range lines {
			switch line.Kind {
			case "add":
				addedCount++
			case "del":
				removedCount++
			case "change":
				addedCount++
				removedCount++
			}
			if line.Kind != "same" {
				changeCount++
			}
		}
	}
	focus, skip, decision := reviewFor(mode, pair.RelPath, signals, risk, reason, changeCount, addedCount, removedCount)
	detail.Focus = focus
	detail.Skip = skip
	detail.Decision = decision
	summary := fileSummary{
		Key:          key,
		Path:         pair.RelPath,
		Mode:         mode,
		Risk:         risk,
		Reason:       reason,
		Signals:      signals,
		ChangeCount:  changeCount,
		AddedCount:   addedCount,
		RemovedCount: removedCount,
	}
	return detail, summary, true, nil
}

func readMaybe(path string, exists bool) (string, error) {
	if !exists {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n"), nil
}

func buildLineViews(leftText, rightText string) []lineView {
	leftLines := splitLines(leftText)
	rightLines := splitLines(rightText)
	ops := diffLines(leftLines, rightLines)
	views := make([]lineView, 0, len(ops))
	leftNo, rightNo := 1, 1
	for _, op := range ops {
		view := lineView{Kind: op.kind, Hot: isHotLine(op.left + "\n" + op.right)}
		switch op.kind {
		case "same":
			view.LeftNo = leftNo
			view.RightNo = rightNo
			view.Left = op.left
			view.Right = op.right
			leftNo++
			rightNo++
		case "add":
			view.RightNo = rightNo
			view.Right = op.right
			rightNo++
		case "del":
			view.LeftNo = leftNo
			view.Left = op.left
			leftNo++
		case "change":
			view.LeftNo = leftNo
			view.RightNo = rightNo
			view.Left = op.left
			view.Right = op.right
			leftNo++
			rightNo++
		}
		views = append(views, view)
	}
	return views
}

func buildLineNotes(path string, lines []lineView, signals []string, risk string) []lineNote {
	notes := make([]lineNote, 0, 6)
	seen := map[string]struct{}{}
	for idx, line := range lines {
		if line.Kind == "same" {
			continue
		}
		title, text, severity, ok := explainLineChange(path, line, signals, risk)
		if !ok {
			continue
		}
		key := fmt.Sprintf("%s|%d|%d", title, line.LeftNo, line.RightNo)
		if _, exists := seen[key]; exists {
			continue
		}
		notes = append(notes, lineNote{
			ID:        fmt.Sprintf("note-%d", idx),
			LineIndex: idx,
			LeftNo:    line.LeftNo,
			RightNo:   line.RightNo,
			Severity:  severity,
			Title:     title,
			Text:      text,
			Snippet:   previewSnippet(line),
		})
		seen[key] = struct{}{}
		if len(notes) >= 6 {
			break
		}
	}
	if len(notes) > 0 {
		return notes
	}
	for idx, line := range lines {
		if line.Kind == "same" {
			continue
		}
		notes = append(notes, lineNote{
			ID:        fmt.Sprintf("note-%d", idx),
			LineIndex: idx,
			LeftNo:    line.LeftNo,
			RightNo:   line.RightNo,
			Severity:  severityFromRisk(risk),
			Title:     genericLineTitle(line.Kind),
			Text:      genericLineReason(line.Kind),
			Snippet:   previewSnippet(line),
		})
		if len(notes) >= 3 {
			break
		}
	}
	return notes
}

func explainLineChange(path string, line lineView, signals []string, risk string) (string, string, string, bool) {
	pathLower := strings.ToLower(path)
	text := strings.ToLower(strings.TrimSpace(line.Left + "\n" + line.Right))

	if containsAny(text, "fallback", "兜底") && line.Kind == "del" {
		return "删掉隐式兜底", "我认为这里是在去掉“先糊过去再说”的分支，让配置错误或能力未开通时直接暴露，而不是悄悄放行。", "high", true
	}
	if containsAny(text, "image_generation", "imagegeneration") {
		switch {
		case containsAny(text, "default(false)", ".default(false)") || (containsAny(pathLower, "schema") && containsAny(text, "false")):
			return "默认关死，先别自动放开", "我认为这里是在把 image_generation 设成显式默认 false，先 fail-closed，避免 GPT 订阅分组默认拿到生图权限。", "high", true
		case containsAny(pathLower, "groupsview.vue") && containsAny(text, "watch(", "platform", "provider", "openai"):
			return "切平台时清理脏状态", "我认为这里是在防止管理员切换平台后，旧的生图能力字段残留到提交数据里，导致表单看着对、实际保存错。", "mid", true
		case containsAny(pathLower, "groupsview.vue"):
			return "前端把能力开关做成显式控件", "我认为这里是在给 image_generation 补独立的可见开关，避免它继续藏在隐式规则里，让人不知道当前分组到底开没开。", "mid", true
		case containsAny(pathLower, "/i18n/", "locales/", "i18n/"):
			return "补多语言文案，不让开关变哑巴", "我认为这里是在把 image_generation 的说明补到多语言文案里，避免后台有开关，但界面没有可读说明。", "low", true
		case containsAny(pathLower, "dto", "types", "mapper"):
			return "补前后端契约字段", "我认为这里是在把 image_generation 显式写进请求/响应类型，避免界面能改、但类型层和映射层把字段吃掉。", "mid", true
		case containsAny(pathLower, "handler", "service", "repo"):
			return "把能力开关贯通到执行链路", "我认为这里是在把 image_generation 从 handler / service / repository 一路传下去，避免只改到展示层，真实读写却没生效。", "high", true
		default:
			return "围绕生图权限单独立规矩", "我认为这里是在把 image_generation 从“隐式跟随其他能力”改成“显式配置、显式保存、显式返回”，方便以后再单独扩展生图包月。", severityFromRisk(risk), true
		}
	}
	if containsAny(pathLower, "/i18n/", "locales/", "i18n/") {
		return "补文案对齐真实能力", "我认为这里是在把后台或前端已有行为补成可读文案，避免配置和说明长期脱节。", "low", true
	}
	if hasSignal(signals, "命中接口/路由逻辑") && containsAny(pathLower, "handler", "router", "api") {
		return "把规则提前到入口层", "我认为这里是在让请求一进来就吃到正确约束，尽量别把错误配置继续放到后面的 service / repo / upstream。", "high", true
	}
	if hasSignal(signals, "命中限额/计费/订阅关键词") && containsAny(pathLower, "service", "repo", "schema", "ent") {
		return "把业务规则落到服务/存储层", "我认为这里是在避免“前台看起来改了、真实计数和存储没改”的假修复，让业务规则真正落到持久层。", "high", true
	}
	if line.Hot {
		return genericLineTitle(line.Kind), genericLineReason(line.Kind), severityFromRisk(risk), true
	}
	return "", "", "", false
}

func previewSnippet(line lineView) string {
	raw := strings.TrimSpace(line.Right)
	if raw == "" {
		raw = strings.TrimSpace(line.Left)
	}
	raw = strings.ReplaceAll(raw, "\t", "    ")
	raw = strings.Join(strings.Fields(raw), " ")
	runes := []rune(raw)
	if len(runes) > 88 {
		return string(runes[:88]) + "..."
	}
	return raw
}

func genericLineTitle(kind string) string {
	switch kind {
	case "add":
		return "新增显式规则"
	case "del":
		return "删除旧分支"
	case "change":
		return "收紧既有逻辑"
	default:
		return "关键差异"
	}
}

func genericLineReason(kind string) string {
	switch kind {
	case "add":
		return "我认为这里是在把原来靠约定或隐式推断的东西改成显式规则，方便后续复核。"
	case "del":
		return "我认为这里是在移除旧判断或旧兼容分支，避免继续误导真实行为。"
	case "change":
		return "我认为这里是在把原来的宽松处理收紧成更明确的行为，让问题在正确层暴露。"
	default:
		return "我认为这里是这次改动里最值得先看的一处。"
	}
}

type diffOp struct{ kind, left, right string }

func diffLines(a, b []string) []diffOp {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	i, j := 0, 0
	ops := make([]diffOp, 0, n+m)
	for i < n && j < m {
		if a[i] == b[j] {
			ops = append(ops, diffOp{kind: "same", left: a[i], right: b[j]})
			i++
			j++
			continue
		}
		if dp[i+1][j] == dp[i][j+1] && i+1 <= n && j+1 <= m {
			ops = append(ops, diffOp{kind: "change", left: a[i], right: b[j]})
			i++
			j++
			continue
		}
		if dp[i+1][j] >= dp[i][j+1] {
			ops = append(ops, diffOp{kind: "del", left: a[i]})
			i++
		} else {
			ops = append(ops, diffOp{kind: "add", right: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{kind: "del", left: a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{kind: "add", right: b[j]})
	}
	return compressChanges(ops)
}

func compressChanges(in []diffOp) []diffOp {
	out := make([]diffOp, 0, len(in))
	for i := 0; i < len(in); i++ {
		if i+1 < len(in) && in[i].kind == "del" && in[i+1].kind == "add" {
			out = append(out, diffOp{kind: "change", left: in[i].left, right: in[i+1].right})
			i++
			continue
		}
		out = append(out, in[i])
	}
	return out
}

func detectMode(path, leftText, rightText string) string {
	ext := strings.ToLower(filepath.Ext(path))
	articleExt := map[string]bool{".md": true, ".markdown": true, ".txt": true, ".html": true, ".htm": true}
	codeExt := map[string]bool{".go": true, ".rs": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".vue": true, ".py": true, ".sh": true, ".sql": true, ".java": true, ".c": true, ".cpp": true, ".h": true, ".json": true, ".yaml": true, ".yml": true, ".toml": true}
	if articleExt[ext] {
		text := leftText + "\n" + rightText
		if strings.Contains(text, "#") || strings.Contains(text, "标题") || strings.Contains(text, "摘要") || strings.Contains(text, "<p") {
			return "article"
		}
	}
	if codeExt[ext] {
		return "code"
	}
	if strings.Contains(leftText+rightText, "func ") || strings.Contains(leftText+rightText, "const ") || strings.Contains(leftText+rightText, "class ") {
		return "code"
	}
	return "article"
}

func detectSignals(path, leftText, rightText, mode string, leftExists, rightExists bool) ([]string, string, string) {
	text := strings.ToLower(path + "\n" + leftText + "\n" + rightText)
	signals := []string{}
	risk := "low"
	reason := "常规改动"
	if !leftExists {
		signals = append(signals, "新增文件")
		risk = bumpRisk(risk, "mid")
		reason = "新增对象需要确认是否真的被纳入任务闭环"
	}
	if !rightExists {
		signals = append(signals, "删除文件")
		risk = bumpRisk(risk, "high")
		reason = "删除对象需要确认是否影响既有能力"
	}
	if mode == "code" {
		if containsAny(text, "handler", "router", "controller", "api", "endpoint", "/api/", "http.") {
			signals = append(signals, "命中接口/路由逻辑")
			risk = bumpRisk(risk, "high")
			reason = "接口或路由改动通常直接影响真实业务结果"
		}
		if containsAny(text, "auth", "token", "jwt", "permission", "admin", "role") {
			signals = append(signals, "命中鉴权/权限关键词")
			risk = bumpRisk(risk, "high")
			reason = "权限相关改动需要重点复查边界"
		}
		if containsAny(text, "quota", "limit", "usage", "billing", "payment", "subscribe", "subscription") {
			signals = append(signals, "命中限额/计费/订阅关键词")
			risk = bumpRisk(risk, "high")
			reason = "计费、限额、订阅类改动不能只看表面成功提示"
		}
		if strings.HasSuffix(strings.ToLower(path), ".json") || strings.HasSuffix(strings.ToLower(path), ".yaml") || strings.HasSuffix(strings.ToLower(path), ".yml") || strings.HasSuffix(strings.ToLower(path), ".toml") {
			signals = append(signals, "命中配置文件")
			risk = bumpRisk(risk, "mid")
			if reason == "常规改动" {
				reason = "配置改动需要确认是否与运行态一致"
			}
		}
	} else {
		if containsAny(text, "标题", "摘要", "headline", "subject", "title") {
			signals = append(signals, "命中标题/摘要结构")
			risk = bumpRisk(risk, "high")
			reason = "标题和摘要变化会直接改变读者理解"
		}
		if containsAny(text, "公告", "邮件", "通知", "announcement", "email") {
			signals = append(signals, "命中对外文案")
			risk = bumpRisk(risk, "mid")
			if reason == "常规改动" {
				reason = "对外表达要确认口径一致"
			}
		}
		if len(splitParagraphs(rightText)) > 6 || len(splitParagraphs(leftText)) > 6 {
			signals = append(signals, "段落较多")
			risk = bumpRisk(risk, "mid")
		}
	}
	if len(signals) == 0 {
		signals = append(signals, "未命中特殊规则")
	}
	return uniqStrings(signals), risk, reason
}

func reviewFor(mode, path string, signals []string, risk, reason string, changeCount, addedCount, removedCount int) ([]focusItem, []string, string) {
	focus := []focusItem{}
	skip := []string{}
	if mode == "code" {
		focus = append(focus, focusItem{Title: "先看核心逻辑入口", Severity: severityFromRisk(risk), Text: fmt.Sprintf("%s：%s。优先确认改动是否真的落在执行路径上，而不是只改到提示或展示层。", path, reason)})
		if hasSignal(signals, "命中接口/路由逻辑") {
			focus = append(focus, focusItem{Title: "再看真实业务路径", Severity: "high", Text: "接口/路由类改动要确认请求真的打到这里，并验证返回成功是否等于业务生效。"})
		}
		if hasSignal(signals, "命中限额/计费/订阅关键词") {
			focus = append(focus, focusItem{Title: "重点排查展示成功但业务未生效", Severity: "high", Text: "限额/计费/订阅改动常见问题是只清展示值、没清真实计数、缓存或汇总层。"})
		}
		skip = []string{"纯样式、重命名、格式化通常不是第一优先级。", "没有进入真实执行路径的辅助代码可后看。", fmt.Sprintf("本对象共 %d 处差异，先追最贴近入口的那几处。", changeCount)}
		return focus, skip, fmt.Sprintf("规则审计认为这是 %s 风险改动；先确认执行路径，再确认结果是否真的生效。", risk)
	}
	focus = append(focus, focusItem{Title: "先看标题与首段是否还是同一个意思", Severity: severityFromRisk(risk), Text: fmt.Sprintf("%s：%s。不要先陷入措辞细节，先确认读者收到的主结论有没有变。", path, reason)})
	if hasSignal(signals, "命中对外文案") {
		focus = append(focus, focusItem{Title: "再看是否暴露内部口径", Severity: "mid", Text: "对外公告、邮件、通知要避开内部排查语言，重点是用户价值表达。"})
	}
	focus = append(focus, focusItem{Title: "最后看新增/删除段落是否改变承诺边界", Severity: "mid", Text: fmt.Sprintf("本对象新增 %d 处、删除 %d 处；重点不是数量，而是有没有改掉结论、适用范围或承诺。", addedCount, removedCount)})
	skip = []string{"单纯修辞替换可后看。", "标点、换行、近义词不是第一优先级。", "先确认主结论，再做润色判断。"}
	return focus, skip, fmt.Sprintf("规则审计认为这是 %s 风险文案改动；先确认主结论、适用范围和对外口径。", risk)
}

func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

func splitParagraphs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return []string{}
	}
	chunks := strings.Split(s, "\n\n")
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		c = strings.TrimSpace(c)
		if c != "" {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return []string{s}
	}
	return out
}

func isTextLike(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	allowed := map[string]bool{".go": true, ".rs": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".vue": true, ".py": true, ".sh": true, ".sql": true, ".json": true, ".yaml": true, ".yml": true, ".toml": true, ".md": true, ".markdown": true, ".txt": true, ".html": true, ".htm": true, ".css": true, ".scss": true}
	return allowed[ext]
}

func isHotLine(s string) bool {
	s = strings.ToLower(s)
	return containsAny(s, "auth", "token", "quota", "limit", "billing", "payment", "admin", "role", "delete", "reset", "subscription", "邮件", "公告", "标题", "摘要")
}

func containsAny(s string, keys ...string) bool {
	for _, k := range keys {
		if strings.Contains(s, strings.ToLower(k)) {
			return true
		}
	}
	return false
}

func riskRank(r string) int {
	switch r {
	case "high":
		return 3
	case "mid":
		return 2
	default:
		return 1
	}
}

func bumpRisk(cur, next string) string {
	if riskRank(next) > riskRank(cur) {
		return next
	}
	return cur
}

func severityFromRisk(r string) string {
	if r == "high" {
		return "high"
	}
	if r == "mid" {
		return "mid"
	}
	return "low"
}

func dominantMode(counter map[string]int) string {
	bestMode := "code"
	bestCount := -1
	for k, v := range counter {
		if v > bestCount {
			bestMode, bestCount = k, v
		}
	}
	return bestMode
}

func uniqStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func keysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func hasSignal(signals []string, want string) bool {
	for _, s := range signals {
		if s == want {
			return true
		}
	}
	return false
}

func hashKey(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:8])
}

func envOr(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func limitLines(lines []lineView, maxN int) []lineView {
	if len(lines) <= maxN {
		return lines
	}
	firstChanged, lastChanged := -1, -1
	for i, line := range lines {
		if line.Kind == "same" {
			continue
		}
		if firstChanged == -1 {
			firstChanged = i
		}
		lastChanged = i
	}
	if firstChanged == -1 {
		return lines[:maxN]
	}

	const context = 24
	start := max(firstChanged-context, 0)
	end := min(lastChanged+context+1, len(lines))
	if end-start <= maxN {
		return lines[start:end]
	}

	end = min(start+maxN, len(lines))
	if end-start < maxN {
		start = max(end-maxN, 0)
	}
	return lines[start:end]
}

func limitStrings(in []string, maxN int) []string {
	if len(in) <= maxN {
		return in
	}
	return in[:maxN]
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
