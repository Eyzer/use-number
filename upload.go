package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ---------- 上传归档文件 ----------

// 文件名里的发文字号：汉字/字母前缀 + 括号年份 + 序号 + 号
// 兼容 〔2026〕 [2026] （2026） (2026) 【2026】，数字兼容全角
var docNumPattern = regexp.MustCompile(`[\p{Han}A-Za-z]{1,20}\s*[〔\[（(【]\s*[0-9０-９]{2,4}\s*[〕\]）)】]\s*[0-9０-９]{1,6}\s*号`)

// 命名规则里的占位符，如 {字号} {标题} {doc} {title}
var ruleToken = regexp.MustCompile(`\{\s*([^{}]+?)\s*\}`)

// 把命名规则模板编译成正则；返回 nil 表示规则里没有可用占位符，不做校验
func buildRuleRegex(rule string) *regexp.Regexp {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		rule = "{字号}-{标题}"
	}
	var sb strings.Builder
	sb.WriteString(`(?s)^.*?`)
	first := true
	for _, m := range ruleToken.FindAllStringSubmatch(rule, -1) {
		name := strings.ToLower(strings.TrimSpace(m[1]))
		seg := ""
		switch name {
		case "字号", "文号", "doc", "number", "doc_number", "docnumber":
			seg = `(?:` + docNumPattern.String() + `)`
		case "标题", "title":
			seg = `(?:[\p{Han}A-Za-z0-9][^\\/:*?"<>|\r\n]{1,})`
		default:
			continue
		}
		if !first {
			// 占位符之间允许任意短分隔符，- _ 空格 · 等都能过
			sb.WriteString(`.{0,8}?`)
		}
		sb.WriteString(seg)
		first = false
	}
	if first {
		return nil
	}
	sb.WriteString(`.*$`)
	re, err := regexp.Compile(sb.String())
	if err != nil {
		return nil
	}
	return re
}

func checkNameRule(name, rule string) (bool, string) {
	re := buildRuleRegex(rule)
	if re == nil {
		return true, ""
	}
	if re.MatchString(name) {
		return true, ""
	}
	return false, "文件名不符合命名规则：" + strings.TrimSpace(rule)
}

func atoiDefault(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func uploadExtSet() map[string]bool {
	raw := getSetting("upload_ext")
	if strings.TrimSpace(raw) == "" {
		raw = "pdf,doc,docx,ofd,wps,xls,xlsx,ppt,pptx,jpg,jpeg,png,zip,rar,7z"
	}
	set := map[string]bool{}
	for _, p := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == ' ' || r == ';' || r == '；'
	}) {
		p = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(p), "."))
		if p != "" {
			set[p] = true
		}
	}
	return set
}

func safeFileName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '|', '?', '*', 0:
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, ". ")
	if name == "" || name == "." || name == ".." {
		return ""
	}
	return name
}

// 同名文件不覆盖，追加 (1) (2)
func uniquePath(dir, name string) string {
	p := filepath.Join(dir, name)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return p
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; i <= 500; i++ {
		p = filepath.Join(dir, fmt.Sprintf("%s(%d)%s", base, i, ext))
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
	}
	return filepath.Join(dir, base+"-"+randHex(4)+ext)
}

type recRow struct {
	ID    int64
	Doc   string
	Title string
	Arch  int
}

func loadRecordRows() ([]recRow, error) {
	rows, err := db.Query("SELECT id, doc_number, title, COALESCE(is_archived,0) FROM doc_number_records")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []recRow{}
	for rows.Next() {
		var x recRow
		if err := rows.Scan(&x.ID, &x.Doc, &x.Title, &x.Arch); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, nil
}

// 给单个文件找对应记录：字号精确 → 字号包含 → 标题包含
func pickRecord(name, fileKey string, recs []recRow) (recRow, string, bool) {
	docs := docNumPattern.FindAllString(name, -1)
	for _, rc := range recs {
		rk := normalizeKey(rc.Doc)
		if rk == "" {
			continue
		}
		for _, d := range docs {
			if normalizeKey(d) == rk {
				return rc, "字号", true
			}
		}
	}
	for _, rc := range recs {
		rk := normalizeKey(rc.Doc)
		if rk != "" && containsBounded(fileKey, rk) {
			return rc, "字号", true
		}
	}
	for _, rc := range recs {
		tk := normalizeKey(rc.Title)
		if len([]rune(tk)) < 4 || tk == normalizeKey(rc.Doc) || genericTitles[tk] {
			continue
		}
		if containsBounded(fileKey, tk) {
			return rc, "标题", true
		}
	}
	return recRow{}, "", false
}

type uploadItem struct {
	File      string `json:"file"`
	Saved     string `json:"saved"`
	OK        bool   `json:"ok"`
	RuleOK    bool   `json:"rule_ok"`
	Archived  bool   `json:"archived"`
	DocNumber string `json:"doc_number"`
	Title     string `json:"title"`
	By        string `json:"by"`
	Message   string `json:"message"`
}

func uploadConfig(w http.ResponseWriter) {
	dirs := archiveDirs()
	dir := ""
	if len(dirs) > 0 {
		dir = dirs[0]
	}
	writeJSON(w, 200, map[string]any{
		"enabled": getSetting("upload_enabled") != "0",
		"rule":    getSetting("upload_rule"),
		"ext":     getSetting("upload_ext"),
		"max_mb":  atoiDefault(getSetting("upload_max_mb"), 50),
		"dir":     dir,
	})
}

func publicUpload(w http.ResponseWriter, r *http.Request) {
	if getSetting("upload_enabled") == "0" {
		fail(w, 403, "前台上传已关闭，请联系管理员")
		return
	}
	dirs := archiveDirs()
	if len(dirs) == 0 {
		fail(w, 400, "请先在后台「系统设置」里填写归档目录，上传文件会保存到第一个目录")
		return
	}
	dir := dirs[0]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail(w, 400, "上传目录无法创建或没有权限："+dir+"（"+err.Error()+"）")
		return
	}

	maxMB := atoiDefault(getSetting("upload_max_mb"), 50)
	perLimit := int64(maxMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, (perLimit+1<<20)*10)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		fail(w, 400, fmt.Sprintf("上传失败：内容过大或格式错误（单文件上限 %d MB）", maxMB))
		return
	}
	if r.MultipartForm == nil || len(r.MultipartForm.File["file"]) == 0 {
		fail(w, 400, "没有收到文件")
		return
	}

	rule := getSetting("upload_rule")
	exts := uploadExtSet()
	recs, err := loadRecordRows()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}

	items := []uploadItem{}
	archivedCount := 0
	for _, fh := range r.MultipartForm.File["file"] {
		it := uploadItem{File: fh.Filename}
		name := safeFileName(fh.Filename)
		if name == "" {
			it.Message = "文件名不可用"
			items = append(items, it)
			continue
		}
		it.File = name
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
		if len(exts) > 0 && !exts[ext] {
			it.Message = "不允许的文件类型（." + ext + "）"
			items = append(items, it)
			continue
		}
		if fh.Size > perLimit {
			it.Message = fmt.Sprintf("文件超过 %d MB", maxMB)
			items = append(items, it)
			continue
		}

		src, err := fh.Open()
		if err != nil {
			it.Message = "读取上传内容失败"
			items = append(items, it)
			continue
		}
		dstPath := uniquePath(dir, name)
		dst, err := os.Create(dstPath)
		if err != nil {
			src.Close()
			it.Message = "写入失败：" + err.Error()
			items = append(items, it)
			continue
		}
		_, cerr := io.Copy(dst, src)
		dst.Close()
		src.Close()
		if cerr != nil {
			_ = os.Remove(dstPath)
			it.Message = "写入失败：" + cerr.Error()
			items = append(items, it)
			continue
		}

		it.OK = true
		it.Saved = dstPath
		savedName := filepath.Base(dstPath)
		it.RuleOK, _ = checkNameRule(savedName, rule)

		rc, by, hit := pickRecord(savedName, normalizeKey(savedName), recs)
		if !it.RuleOK {
			it.Message = "已保存，但文件名不符合命名规则，未自动归档"
			if hit {
				it.Message += "（已找到疑似记录 " + rc.Doc + "，请按规则重命名后重新上传）"
			}
			items = append(items, it)
			continue
		}
		if !hit {
			it.Message = "已保存，但没有找到对应的领取记录"
			items = append(items, it)
			continue
		}

		if _, err := db.Exec("UPDATE doc_number_records SET is_archived=1, archive_file=?, updated_at=? WHERE id=?", dstPath, nowStr(), rc.ID); err != nil {
			it.Message = "已保存，但更新归档状态失败：" + err.Error()
			items = append(items, it)
			continue
		}
		it.Archived = true
		it.DocNumber = rc.Doc
		it.Title = rc.Title
		it.By = by
		it.Message = "已归档到 " + rc.Doc
		if by == "标题" {
			it.Message += "（按标题匹配）"
		}
		archivedCount++
		items = append(items, it)
	}

	writeJSON(w, 200, map[string]any{
		"dir":      dir,
		"rule":     rule,
		"items":    items,
		"archived": archivedCount,
	})
}

// 后台在设置页里试跑规则用
func testUploadRule(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Rule string `json:"rule"`
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	ok, msg := checkNameRule(b.Name, b.Rule)
	writeJSON(w, 200, map[string]any{"ok": ok, "message": msg})
}
