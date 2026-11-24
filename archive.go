package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ---------- 归档目录配置 ----------

// 设置项 archive_dir，每行一个目录，支持 Windows 与 Linux 路径
func archiveDirs() []string {
	raw := getSetting("archive_dir")
	out := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

type archiveFile struct {
	Name string
	Path string
	Key  string // 归一化后的文件名，用于包含判断
}

// 归一化：只保留汉字、字母、数字，去掉空格与各类括号标点
// 「国发〔2026〕102号」与「国发[2026]102号xxx.pdf」都会被还原成 国发2026102号…
var nonKeyChar = regexp.MustCompile(`[^\p{Han}A-Za-z0-9]+`)

func normalizeKey(s string) string {
	return nonKeyChar.ReplaceAllString(strings.ToLower(s), "")
}

// 包含判断：如果关键词以数字结尾，要求后面不能再跟数字
// 避免「公文11」命中「公文118」，同时不影响「…的通知」命中「…的通知附件」
func containsBounded(hay, needle string) bool {
	if needle == "" {
		return false
	}
	h, n := []rune(hay), []rune(needle)
	for i := 0; i+len(n) <= len(h); i++ {
		if string(h[i:i+len(n)]) != needle {
			continue
		}
		end := i + len(n)
		if end < len(h) && isDigitRune(h[end-1]) && isDigitRune(h[end]) {
			continue
		}
		return true
	}
	return false
}

func isDigitRune(r rune) bool { return r >= '0' && r <= '9' }

// 扫描目录，递归读取文件名；跳过隐藏目录与临时文件
func scanArchiveFiles(dirs []string) ([]archiveFile, []string, error) {
	files := []archiveFile{}
	scanned := []string{}
	for _, dir := range dirs {
		stat, err := os.Stat(dir)
		if err != nil {
			return nil, nil, errPath(dir, err)
		}
		if !stat.IsDir() {
			return nil, nil, errPath(dir, nil)
		}
		scanned = append(scanned, dir)
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			name := d.Name()
			if d.IsDir() {
				if p != dir && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "__pycache__") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~$") {
				return nil
			}
			files = append(files, archiveFile{Name: name, Path: p, Key: normalizeKey(name)})
			if len(files) >= 200000 {
				return filepath.SkipAll
			}
			return nil
		})
	}
	return files, scanned, nil
}

type pathErr struct {
	dir string
	msg string
}

func (e *pathErr) Error() string { return e.msg }

func errPath(dir string, err error) error {
	msg := "目录不存在或无法访问：" + dir
	if err != nil && !os.IsNotExist(err) {
		msg += "（" + err.Error() + "）"
	}
	return &pathErr{dir: dir, msg: msg}
}

// ---------- 自动匹配 ----------

// 通用词不适合做匹配依据，容易误命中
var genericTitles = map[string]bool{
	"通知": true, "报告": true, "请示": true, "批复": true, "函": true, "纪要": true,
	"决定": true, "意见": true, "公告": true, "通告": true, "方案": true, "总结": true,
	"说明": true, "办法": true, "规定": true, "制度": true, "公文": true,
}

type matchReq struct {
	OnlyMark bool `json:"only_mark"` // 只标记命中项，不清除已有归档
}

type matchDetail struct {
	ID        int64  `json:"id"`
	DocNumber string `json:"doc_number"`
	Title     string `json:"title"`
	File      string `json:"file"`
	By        string `json:"by"`
}

type matchResult struct {
	Files         int           `json:"files"`
	Dirs          []string      `json:"dirs"`
	Records       int           `json:"records"`
	Matched       int           `json:"matched"`
	ArchivedNow   int           `json:"archived_now"`
	UnarchivedNow int           `json:"unarchived_now"`
	Unmatched     int           `json:"unmatched"`
	OnlyMark      bool          `json:"only_mark"`
	Details       []matchDetail `json:"details"`
	Message       string        `json:"message"`
}

func matchArchive(w http.ResponseWriter, r *http.Request) {
	var req matchReq
	_ = json.NewDecoder(r.Body).Decode(&req)

	dirs := archiveDirs()
	if len(dirs) == 0 {
		fail(w, 400, "请先在系统设置里填写归档目录")
		return
	}
	files, scanned, err := scanArchiveFiles(dirs)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if len(files) == 0 {
		fail(w, 400, "归档目录里没有可读取的文件，请检查路径")
		return
	}

	type recordRow struct {
		ID    int64
		Doc   string
		Title string
		Arch  int
	}
	rows, err := db.Query("SELECT id, doc_number, title, COALESCE(is_archived,0) FROM doc_number_records")
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	recs := []recordRow{}
	for rows.Next() {
		var x recordRow
		if err := rows.Scan(&x.ID, &x.Doc, &x.Title, &x.Arch); err != nil {
			rows.Close()
			fail(w, 500, err.Error())
			return
		}
		recs = append(recs, x)
	}
	rows.Close()

	res := matchResult{Files: len(files), Dirs: scanned, Records: len(recs), OnlyMark: req.OnlyMark, Details: []matchDetail{}}

	tx, err := db.Begin()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	defer tx.Rollback()

	for _, rc := range recs {
		docKey := normalizeKey(rc.Doc)
		titleKey := normalizeKey(rc.Title)
		// 标题太短或本身就是「通知」这类通用词时，不作标题匹配，避免误命中
		if len([]rune(titleKey)) < 4 || titleKey == docKey || genericTitles[titleKey] {
			titleKey = ""
		}

		hitPath, hitBy := "", ""
		for _, f := range files {
			if containsBounded(f.Key, docKey) {
				hitPath, hitBy = f.Path, "字号"
				break
			}
			if containsBounded(f.Key, titleKey) {
				hitPath, hitBy = f.Path, "标题"
				// 标题命中不立即跳出，继续找是否有字号命中的文件
			}
		}

		if hitPath != "" {
			res.Matched++
			if rc.Arch != 1 {
				res.ArchivedNow++
			}
			if _, err := tx.Exec("UPDATE doc_number_records SET is_archived=1, archive_file=?, updated_at=? WHERE id=?", hitPath, nowStr(), rc.ID); err != nil {
				fail(w, 500, err.Error())
				return
			}
			if len(res.Details) < 200 {
				res.Details = append(res.Details, matchDetail{ID: rc.ID, DocNumber: rc.Doc, Title: rc.Title, File: hitPath, By: hitBy})
			}
			continue
		}

		res.Unmatched++
		if !req.OnlyMark {
			if rc.Arch == 1 {
				res.UnarchivedNow++
			}
			if _, err := tx.Exec("UPDATE doc_number_records SET is_archived=0, archive_file='', updated_at=? WHERE id=?", nowStr(), rc.ID); err != nil {
				fail(w, 500, err.Error())
				return
			}
		}
	}

	if err := tx.Commit(); err != nil {
		fail(w, 500, "保存匹配结果失败")
		return
	}

	res.Message = "扫描 " + strconv.Itoa(res.Files) + " 个文件，命中 " + strconv.Itoa(res.Matched) + " 条，"
	if req.OnlyMark {
		res.Message += "仅标记命中项，未清除已有归档"
	} else {
		res.Message += "新归档 " + strconv.Itoa(res.ArchivedNow) + " 条，转未归档 " + strconv.Itoa(res.UnarchivedNow) + " 条"
	}
	writeJSON(w, 200, res)
}

// 设置页「检测目录」按钮
func checkArchiveDir(w http.ResponseWriter) {
	dirs := archiveDirs()
	type item struct {
		Dir   string `json:"dir"`
		OK    bool   `json:"ok"`
		Files int    `json:"files"`
		Msg   string `json:"msg"`
	}
	list := []item{}
	for _, d := range dirs {
		it := item{Dir: d}
		if st, err := os.Stat(d); err != nil {
			it.Msg = "不存在或无权限"
		} else if !st.IsDir() {
			it.Msg = "不是目录"
		} else {
			it.OK = true
			_ = filepath.WalkDir(d, func(p string, de fs.DirEntry, err error) error {
				if err != nil || de.IsDir() {
					return nil
				}
				it.Files++
				if it.Files > 200000 {
					return filepath.SkipAll
				}
				return nil
			})
			it.Msg = "可访问，共 " + strconv.Itoa(it.Files) + " 个文件"
		}
		list = append(list, it)
	}
	if len(list) == 0 {
		fail(w, 400, "还没有填写归档目录")
		return
	}
	writeJSON(w, 200, map[string]any{"dirs": list})
}

// ---------- 导出 CSV ----------

func exportRecords(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	list, total, err := queryRecords(qs.Get("q"), qs.Get("archived"), 1, 200000, 200000)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	_ = total

	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF") // BOM，Excel 直接打开不乱码
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{"发文字号", "公文标题", "领取人", "归档状态", "归档文件", "领取时间", "备注"})
	for _, it := range list {
		arch := "未归档"
		if it.IsArchived {
			arch = "已归档"
		}
		_ = cw.Write([]string{
			it.DocNumber, it.Title, it.Receiver, arch, it.ArchiveFile, it.ReceivedAt, it.Remark,
		})
	}
	cw.Flush()

	name := "领取记录_" + strings.ReplaceAll(nowStr()[:16], ":", "") + ".csv"
	disposition := "attachment; filename=\"records.csv\"; filename*=UTF-8''" + url.PathEscape(name)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}
