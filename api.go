package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}

func apiHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	method := r.Method

	switch {
	// ---------- 前台公开接口 ----------
	case path == "/api/public/site" && method == http.MethodGet:
		writeJSON(w, 200, map[string]any{
			"site_name": getSetting("site_name"),
			"site_sub":  getSetting("site_sub"),
		})

	case path == "/api/public/types" && method == http.MethodGet:
		listDocTypes(w)

	case path == "/api/public/next" && method == http.MethodGet:
		previewNext(w, r)

	case path == "/api/public/take" && method == http.MethodPost:
		takeNumber(w, r)

	case path == "/api/public/records" && method == http.MethodGet:
		publicRecords(w, r)

	case path == "/api/public/upload-config" && method == http.MethodGet:
		uploadConfig(w)

	case path == "/api/public/upload" && method == http.MethodPost:
		publicUpload(w, r)

	// ---------- 认证 ----------
	case path == "/api/auth/login" && method == http.MethodPost:
		doLogin(w, r)

	case path == "/api/auth/logout" && method == http.MethodPost:
		doLogout(w, r)

	case path == "/api/auth/check" && method == http.MethodGet:
		u := getSessionUser(r)
		if u == nil {
			writeJSON(w, 200, map[string]any{"authenticated": false})
			return
		}
		writeJSON(w, 200, map[string]any{"authenticated": true, "user": u})

	// ---------- 后台接口 ----------
	case strings.HasPrefix(path, "/api/admin/"):
		adminRoute(w, r)

	default:
		fail(w, 404, "接口不存在")
	}
}

func adminRoute(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/")
	method := r.Method

	switch {
	case path == "stats" && method == http.MethodGet:
		adminStats(w)
	case path == "trend" && method == http.MethodGet:
		adminTrend(w, r)

	case path == "types" && method == http.MethodGet:
		listDocTypes(w)
	case path == "types" && method == http.MethodPost:
		createDocType(w, r)
	case strings.HasPrefix(path, "types/") && method == http.MethodPut:
		updateDocType(w, r, parseID(path, "types/"))
	case strings.HasPrefix(path, "types/") && method == http.MethodDelete:
		deleteDocType(w, parseID(path, "types/"))

	case path == "groups" && method == http.MethodGet:
		// 用户组功能已下线，保留空实现以兼容旧前端缓存
		writeJSON(w, 200, []any{})

	case path == "users" && method == http.MethodGet:
		listUsers(w)
	case path == "users" && method == http.MethodPost:
		createUser(w, r)
	case strings.HasPrefix(path, "users/") && strings.HasSuffix(path, "/reset-password") && method == http.MethodPost:
		resetUserPassword(w, r, parseID(path, "users/"))
	case strings.HasPrefix(path, "users/") && method == http.MethodPut:
		updateUser(w, r, parseID(path, "users/"))
	case strings.HasPrefix(path, "users/") && method == http.MethodDelete:
		deleteUser(w, parseID(path, "users/"))

	case path == "records" && method == http.MethodGet:
		adminRecords(w, r)
	case path == "records/export" && method == http.MethodGet:
		exportRecords(w, r)
	case strings.HasPrefix(path, "records/") && method == http.MethodPut:
		updateRecord(w, r, parseID(path, "records/"))
	case strings.HasPrefix(path, "records/") && method == http.MethodDelete:
		deleteRecord(w, parseID(path, "records/"))

	case path == "archive/match" && method == http.MethodPost:
		matchArchive(w, r)
	case path == "archive/check" && method == http.MethodPost:
		checkArchiveDir(w)

	case path == "import" && method == http.MethodPost:
		importRecords(w, r)

	case path == "settings" && method == http.MethodGet:
		readSettings(w)
	case path == "settings" && method == http.MethodPut:
		saveSettings(w, r)
	case path == "settings/test-dzz" && method == http.MethodPost:
		msg, err := testDzz()
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"message": msg})

	case path == "settings/test-rule" && method == http.MethodPost:
		testUploadRule(w, r)

	default:
		fail(w, 404, "接口不存在")
	}
}

func parseID(path, prefix string) int64 {
	s := strings.TrimPrefix(path, prefix)
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	id, _ := strconv.ParseInt(s, 10, 64)
	return id
}

// ---------- 发文字号类型 ----------

type docTypeVO struct {
	ID           int64  `json:"id"`
	Prefix       string `json:"prefix"`
	Suffix       string `json:"suffix"`
	CurrentNum   int    `json:"current_num"`
	Description  string `json:"description"`
	IsActive     bool   `json:"is_active"`
	ReceiveCount int    `json:"receive_count"`
}

func queryDocTypes() ([]docTypeVO, error) {
	rows, err := db.Query(`
		SELECT t.id, t.prefix, t.suffix, COALESCE(t.current_num,1),
		       COALESCE(t.description,''), COALESCE(t.is_active,1),
		       (SELECT COUNT(*) FROM doc_number_records r WHERE r.type_id = t.id)
		FROM doc_number_types t
		ORDER BY t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []docTypeVO{}
	for rows.Next() {
		var t docTypeVO
		var active int
		if err := rows.Scan(&t.ID, &t.Prefix, &t.Suffix, &t.CurrentNum, &t.Description, &active, &t.ReceiveCount); err != nil {
			return nil, err
		}
		t.IsActive = active == 1
		list = append(list, t)
	}
	return list, nil
}

func listDocTypes(w http.ResponseWriter) {
	list, err := queryDocTypes()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func previewNext(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("type_id"), 10, 64)
	var prefix, suffix string
	var cur int
	var active int
	err := db.QueryRow("SELECT prefix, suffix, COALESCE(current_num,1), COALESCE(is_active,1) FROM doc_number_types WHERE id=?", id).
		Scan(&prefix, &suffix, &cur, &active)
	if err != nil {
		fail(w, 404, "字号类型不存在")
		return
	}
	if active != 1 {
		fail(w, 400, "该字号类型已停用")
		return
	}
	// 与取号逻辑保持一致：以已发放最大序号为准，避免预览与实际发出的号不一致
	var maxSeq int
	_ = db.QueryRow("SELECT COALESCE(MAX(seq_num),0) FROM doc_number_records WHERE type_id=?", id).Scan(&maxSeq)
	if cur < 1 {
		cur = 1
	}
	if cur <= maxSeq {
		cur = maxSeq + 1
	}
	writeJSON(w, 200, map[string]any{"next_number": fmt.Sprintf("%s%d%s", prefix, cur, suffix), "current_num": cur})
}

type takeReq struct {
	TypeID   int64  `json:"type_id"`
	Title    string `json:"title"`
	Receiver string `json:"receiver"`
	Archived bool   `json:"archived"`
	Remark   string `json:"remark"`
}

func takeNumber(w http.ResponseWriter, r *http.Request) {
	var req takeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Receiver = strings.TrimSpace(req.Receiver)
	req.Remark = strings.TrimSpace(req.Remark)
	if req.TypeID <= 0 {
		fail(w, 400, "请选择发文字号类型")
		return
	}
	if req.Title == "" {
		fail(w, 400, "请填写公文标题")
		return
	}
	if req.Receiver == "" {
		fail(w, 400, "请填写领取人")
		return
	}

	tx, err := db.Begin()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	defer tx.Rollback()

	var prefix, suffix string
	var cur int
	var active int
	err = tx.QueryRow("SELECT prefix, suffix, COALESCE(current_num,1), COALESCE(is_active,1) FROM doc_number_types WHERE id=?", req.TypeID).
		Scan(&prefix, &suffix, &cur, &active)
	if err != nil {
		fail(w, 404, "发文字号类型不存在")
		return
	}
	if active != 1 {
		fail(w, 400, "该发文字号类型已停用")
		return
	}
	if cur < 1 {
		cur = 1
	}
	// 兜底：以已发放的最大序号为准，避免 current_num 漂移导致发重号
	var maxSeq int
	_ = tx.QueryRow("SELECT COALESCE(MAX(seq_num),0) FROM doc_number_records WHERE type_id=?", req.TypeID).Scan(&maxSeq)
	if cur <= maxSeq {
		cur = maxSeq + 1
	}

	docNumber := fmt.Sprintf("%s%d%s", prefix, cur, suffix)
	if _, err = tx.Exec("UPDATE doc_number_types SET current_num=?, updated_at=? WHERE id=?", cur+1, nowStr(), req.TypeID); err != nil {
		fail(w, 500, "编号递增失败")
		return
	}
	_, err = tx.Exec(`INSERT INTO doc_number_records(doc_number, title, type_id, seq_num, receive_person, is_archived, remark, received_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		docNumber, req.Title, req.TypeID, cur, req.Receiver, boolToInt(req.Archived), req.Remark, nowStr())
	if err != nil {
		fail(w, 500, "保存记录失败")
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, 500, "提交失败")
		return
	}

	writeJSON(w, 200, map[string]any{
		"doc_number":  docNumber,
		"receiver":    req.Receiver,
		"archived":    req.Archived,
		"next_number": fmt.Sprintf("%s%d%s", prefix, cur+1, suffix),
		"title":       req.Title,
	})
}

func createDocType(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Prefix      string `json:"prefix"`
		Suffix      string `json:"suffix"`
		CurrentNum  int    `json:"current_num"`
		Description string `json:"description"`
		IsActive    bool   `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	if strings.TrimSpace(b.Prefix) == "" || strings.TrimSpace(b.Suffix) == "" {
		fail(w, 400, "前部分与后部分不能为空")
		return
	}
	if b.CurrentNum <= 0 {
		b.CurrentNum = 1
	}
	_, err := db.Exec("INSERT INTO doc_number_types(prefix, suffix, current_num, description, is_active) VALUES(?,?,?,?,?)",
		strings.TrimSpace(b.Prefix), strings.TrimSpace(b.Suffix), b.CurrentNum, strings.TrimSpace(b.Description), boolToInt(b.IsActive))
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	listDocTypes(w)
}

func updateDocType(w http.ResponseWriter, r *http.Request, id int64) {
	var b struct {
		Prefix      string `json:"prefix"`
		Suffix      string `json:"suffix"`
		CurrentNum  int    `json:"current_num"`
		Description string `json:"description"`
		IsActive    bool   `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	// 编号只允许往后调，为空或回退都保留原值，避免误操作导致重号
	var old int
	_ = db.QueryRow("SELECT COALESCE(current_num,1) FROM doc_number_types WHERE id=?", id).Scan(&old)
	next := b.CurrentNum
	if next < old {
		next = old
	}
	if next < 1 {
		next = 1
	}
	_, err := db.Exec("UPDATE doc_number_types SET prefix=?, suffix=?, current_num=?, description=?, is_active=?, updated_at=? WHERE id=?",
		strings.TrimSpace(b.Prefix), strings.TrimSpace(b.Suffix), next, strings.TrimSpace(b.Description), boolToInt(b.IsActive), nowStr(), id)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	listDocTypes(w)
}

func deleteDocType(w http.ResponseWriter, id int64) {
	var n int
	_ = db.QueryRow("SELECT COUNT(*) FROM doc_number_records WHERE type_id=?", id).Scan(&n)
	if n > 0 {
		fail(w, 400, fmt.Sprintf("该类型下已有 %d 条领取记录，不能删除，可改为停用", n))
		return
	}
	_, _ = db.Exec("DELETE FROM doc_number_types WHERE id=?", id)
	listDocTypes(w)
}

// ---------- 后台用户 ----------

func listUsers(w http.ResponseWriter) {
	rows, err := db.Query(`SELECT u.id, u.username, COALESCE(u.nickname,''),
		u.is_admin, COALESCE(u.remark,''), COALESCE(u.auth_source,'local')
		FROM users u ORDER BY u.id`)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	defer rows.Close()
	list := []map[string]any{}
	for rows.Next() {
		var id int64
		var admin int
		var username, nickname, remark, source string
		if err := rows.Scan(&id, &username, &nickname, &admin, &remark, &source); err != nil {
			fail(w, 500, err.Error())
			return
		}
		list = append(list, map[string]any{
			"id": id, "username": username, "nickname": nickname,
			"is_admin": admin == 1, "remark": remark, "auth_source": source,
		})
	}
	writeJSON(w, 200, list)
}

func createUser(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Nickname string `json:"nickname"`
		IsAdmin  bool   `json:"is_admin"`
		Remark   string `json:"remark"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	b.Username = strings.TrimSpace(b.Username)
	if b.Username == "" || len(b.Password) < 6 {
		fail(w, 400, "用户名不能为空，密码至少 6 位")
		return
	}
	_, err := db.Exec("INSERT INTO users(username, password_hash, nickname, is_admin, remark, auth_source) VALUES(?,?,?,?,?,'local')",
		b.Username, hashPassword(b.Password), strings.TrimSpace(b.Nickname), boolToInt(b.IsAdmin), strings.TrimSpace(b.Remark))
	if err != nil {
		fail(w, 400, "用户名已存在")
		return
	}
	listUsers(w)
}

func updateUser(w http.ResponseWriter, r *http.Request, id int64) {
	var b struct {
		Nickname string `json:"nickname"`
		IsAdmin  bool   `json:"is_admin"`
		Remark   string `json:"remark"`
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if b.Password != "" {
		if len(b.Password) < 6 {
			fail(w, 400, "密码至少 6 位")
			return
		}
		_, _ = db.Exec("UPDATE users SET password_hash=? WHERE id=?", hashPassword(b.Password), id)
	}
	_, err := db.Exec("UPDATE users SET nickname=?, is_admin=?, remark=?, updated_at=? WHERE id=?",
		strings.TrimSpace(b.Nickname), boolToInt(b.IsAdmin), strings.TrimSpace(b.Remark), nowStr(), id)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	listUsers(w)
}

func deleteUser(w http.ResponseWriter, id int64) {
	var username string
	_ = db.QueryRow("SELECT username FROM users WHERE id=?", id).Scan(&username)
	if username == "admin" {
		fail(w, 400, "内置管理员 admin 不能删除")
		return
	}
	_, _ = db.Exec("DELETE FROM users WHERE id=?", id)
	listUsers(w)
}

func resetUserPassword(w http.ResponseWriter, r *http.Request, id int64) {
	var b struct {
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if len(b.Password) < 6 {
		fail(w, 400, "密码至少 6 位")
		return
	}
	_, err := db.Exec("UPDATE users SET password_hash=?, updated_at=? WHERE id=?", hashPassword(b.Password), nowStr(), id)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"message": "密码已重置"})
}

// ---------- 领取记录 ----------

type recordVO struct {
	ID          int64  `json:"id"`
	DocNumber   string `json:"doc_number"`
	Title       string `json:"title"`
	SeqNum      int    `json:"seq_num"`
	Receiver    string `json:"receive_person"`
	IsArchived  bool   `json:"is_archived"`
	ArchiveFile string `json:"archive_file"`
	Remark      string `json:"remark"`
	ReceivedAt  string `json:"received_at"`
	TypeName    string `json:"type_name"`
}

func recordsWhere(q, archived string) (string, []any) {
	where := " WHERE 1=1"
	args := []any{}
	if strings.TrimSpace(q) != "" {
		kw := "%" + strings.TrimSpace(q) + "%"
		where += " AND (r.doc_number LIKE ? OR r.title LIKE ? OR COALESCE(r.receive_person,'') LIKE ?)"
		args = append(args, kw, kw, kw)
	}
	if archived == "1" {
		where += " AND COALESCE(r.is_archived,0) = 1"
	} else if archived == "0" {
		where += " AND COALESCE(r.is_archived,0) = 0"
	}
	return where, args
}

func queryRecords(q string, archived string, page, size, maxSize int) ([]recordVO, int, error) {
	where, args := recordsWhere(q, archived)

	var total int
	if err := db.QueryRow("SELECT COUNT(*) FROM doc_number_records r"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if size <= 0 || size > maxSize {
		size = 20
	}

	// 按号码倒序：seq_num 是数字列，100 会排在 99 之前，不会被当成字符串比较
	sqlStr := `SELECT r.id, r.doc_number, r.title, COALESCE(r.seq_num,0), COALESCE(r.receive_person,''),
		COALESCE(r.is_archived,0), COALESCE(r.archive_file,''), COALESCE(r.remark,''),
		COALESCE(strftime('%Y-%m-%d %H:%M', r.received_at), r.received_at),
		COALESCE(t.prefix || t.suffix, '')
		FROM doc_number_records r
		LEFT JOIN doc_number_types t ON t.id = r.type_id` + where + `
		ORDER BY COALESCE(r.seq_num,0) DESC, r.id DESC LIMIT ? OFFSET ?`
	args = append(args, size, (page-1)*size)

	rows, err := db.Query(sqlStr, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := []recordVO{}
	for rows.Next() {
		var rec recordVO
		var arch int
		if err := rows.Scan(&rec.ID, &rec.DocNumber, &rec.Title, &rec.SeqNum, &rec.Receiver, &arch, &rec.ArchiveFile, &rec.Remark, &rec.ReceivedAt, &rec.TypeName); err != nil {
			return nil, 0, err
		}
		rec.IsArchived = arch == 1
		list = append(list, rec)
	}
	return list, total, nil
}

func publicRecords(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	page, _ := strconv.Atoi(qs.Get("page"))
	size, _ := strconv.Atoi(qs.Get("size"))
	const maxSize = 500
	list, total, err := queryRecords(qs.Get("q"), qs.Get("archived"), page, size, maxSize)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	if page < 1 {
		page = 1
	}
	if size <= 0 || size > maxSize {
		size = 20
	}
	writeJSON(w, 200, map[string]any{
		"records": list, "total": total, "page": page, "size": size,
		"pages": max((total+size-1)/size, 1),
	})
}

func adminRecords(w http.ResponseWriter, r *http.Request) {
	publicRecords(w, r)
}

func updateRecord(w http.ResponseWriter, r *http.Request, id int64) {
	var b struct {
		Title      string `json:"title"`
		Receiver   string `json:"receive_person"`
		IsArchived bool   `json:"is_archived"`
		Remark     string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	_, err := db.Exec("UPDATE doc_number_records SET title=?, receive_person=?, is_archived=?, remark=?, updated_at=? WHERE id=?",
		strings.TrimSpace(b.Title), strings.TrimSpace(b.Receiver), boolToInt(b.IsArchived), strings.TrimSpace(b.Remark), nowStr(), id)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"message": "已保存"})
}

func deleteRecord(w http.ResponseWriter, id int64) {
	_, err := db.Exec("DELETE FROM doc_number_records WHERE id=?", id)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"message": "已删除"})
}

// ---------- 批量导入 ----------

type importReq struct {
	TypeID int64  `json:"type_id"`
	Text   string `json:"text"`
}

func splitImportLine(line string) (string, string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", ""
	}
	for _, sep := range []string{"\t", "|", "｜"} {
		if i := strings.Index(line, sep); i >= 0 {
			return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+len(sep):])
		}
	}
	// 无分隔符时，按第一个空白切开：编号在前，标题在后
	for i, r := range line {
		if unicode.IsSpace(r) {
			return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i:])
		}
	}
	return line, ""
}

func importRecords(w http.ResponseWriter, r *http.Request) {
	var req importReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	if req.TypeID <= 0 {
		fail(w, 400, "请选择要导入的发文字号类型")
		return
	}
	var prefix, suffix string
	var cur int
	err := db.QueryRow("SELECT prefix, suffix, COALESCE(current_num,1) FROM doc_number_types WHERE id=?", req.TypeID).
		Scan(&prefix, &suffix, &cur)
	if err != nil {
		fail(w, 404, "发文字号类型不存在")
		return
	}

	lines := strings.Split(strings.ReplaceAll(req.Text, "\r\n", "\n"), "\n")
	inserted, skipped := 0, 0
	reasons := []string{}
	// 下一个可用编号，从当前值与已发放最大序号中取较大者起步，只增不减
	nextNum := cur
	var maxSeq int
	_ = db.QueryRow("SELECT COALESCE(MAX(seq_num),0) FROM doc_number_records WHERE type_id=?", req.TypeID).Scan(&maxSeq)
	if maxSeq+1 > nextNum {
		nextNum = maxSeq + 1
	}
	if nextNum < 1 {
		nextNum = 1
	}

	tx, err := db.Begin()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	defer tx.Rollback()

	for idx, raw := range lines {
		lineNo := idx + 1
		docNumber, title := splitImportLine(raw)
		if docNumber == "" {
			continue
		}
		if !strings.HasPrefix(docNumber, prefix) || !strings.HasSuffix(docNumber, suffix) {
			skipped++
			reasons = append(reasons, fmt.Sprintf("第%d行「%s」不符合前缀 %s / 后缀 %s", lineNo, docNumber, prefix, suffix))
			continue
		}
		mid := strings.TrimSuffix(strings.TrimPrefix(docNumber, prefix), suffix)
		num, convErr := strconv.Atoi(strings.TrimSpace(mid))
		if convErr != nil {
			skipped++
			reasons = append(reasons, fmt.Sprintf("第%d行「%s」中间不是数字", lineNo, docNumber))
			continue
		}
		var exists int
		_ = tx.QueryRow("SELECT COUNT(*) FROM doc_number_records WHERE doc_number=?", docNumber).Scan(&exists)
		if exists > 0 {
			skipped++
			reasons = append(reasons, fmt.Sprintf("第%d行「%s」已存在，跳过", lineNo, docNumber))
			continue
		}
		if title == "" {
			title = docNumber
		}
		if _, err := tx.Exec(`INSERT INTO doc_number_records(doc_number, title, type_id, seq_num, receive_person, is_archived, remark, received_at)
			VALUES(?,?,?,?,'',0,'历史导入',?)`, docNumber, title, req.TypeID, num, nowStr()); err != nil {
			skipped++
			reasons = append(reasons, fmt.Sprintf("第%d行写入失败", lineNo))
			continue
		}
		inserted++
		if num >= nextNum {
			nextNum = num + 1
		}
	}

	if inserted > 0 {
		if _, err := tx.Exec("UPDATE doc_number_types SET current_num=?, updated_at=? WHERE id=?", nextNum, nowStr(), req.TypeID); err != nil {
			fail(w, 500, "更新当前编号失败")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		fail(w, 500, "提交失败")
		return
	}

	if len(reasons) > 10 {
		reasons = reasons[:10]
	}
	writeJSON(w, 200, map[string]any{
		"inserted": inserted,
		"skipped":  skipped,
		"next_num": nextNum,
		"reasons":  reasons,
	})
}

// ---------- 统计与趋势 ----------

func adminStats(w http.ResponseWriter) {
	var users, types, records, archived, month, unarchived int
	_ = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&users)
	_ = db.QueryRow("SELECT COUNT(*) FROM doc_number_types WHERE COALESCE(is_active,1)=1").Scan(&types)
	_ = db.QueryRow("SELECT COUNT(*) FROM doc_number_records").Scan(&records)
	_ = db.QueryRow("SELECT COUNT(*) FROM doc_number_records WHERE COALESCE(is_archived,0)=1").Scan(&archived)
	_ = db.QueryRow("SELECT COUNT(*) FROM doc_number_records WHERE COALESCE(is_archived,0)=0").Scan(&unarchived)
	monthPrefix := nowStr()[:7] + "%"
	_ = db.QueryRow("SELECT COUNT(*) FROM doc_number_records WHERE received_at LIKE ?", monthPrefix).Scan(&month)
	writeJSON(w, 200, map[string]any{
		"users": users, "types": types,
		"records": records, "archived": archived, "unarchived": unarchived, "month": month,
	})
}

func adminTrend(w http.ResponseWriter, r *http.Request) {
	rng := r.URL.Query().Get("range")
	labels := []string{}
	counts := []int{}

	switch rng {
	case "weekly":
		// 最近 8 周
		for i := 7; i >= 0; i-- {
			start := mondayOf(timeNow().AddDate(0, 0, -7*i))
			end := start.AddDate(0, 0, 7)
			labels = append(labels, start.Format("01-02")+"周")
			counts = append(counts, countBetween(start, end))
		}
	case "monthly":
		for i := 5; i >= 0; i-- {
			d := timeNow().AddDate(0, -i, 0)
			start := timeDate(d.Year(), d.Month(), 1)
			end := start.AddDate(0, 1, 0)
			labels = append(labels, fmt.Sprintf("%d月", int(d.Month())))
			counts = append(counts, countBetween(start, end))
		}
	default:
		for i := 6; i >= 0; i-- {
			d := timeNow().AddDate(0, 0, -i)
			start := timeDate(d.Year(), d.Month(), d.Day())
			labels = append(labels, start.Format("01-02"))
			counts = append(counts, countBetween(start, start.AddDate(0, 0, 1)))
		}
	}
	writeJSON(w, 200, map[string]any{"labels": labels, "values": counts})
}

func countBetween(start, end timeT) int {
	var n int
	_ = db.QueryRow("SELECT COUNT(*) FROM doc_number_records WHERE received_at >= ? AND received_at < ?",
		start.Format("2006-01-02 15:04:05"), end.Format("2006-01-02 15:04:05")).Scan(&n)
	return n
}

// ---------- 系统设置 ----------

func readSettings(w http.ResponseWriter) {
	keys := []string{"dzz_enabled", "dzz_host", "dzz_port", "dzz_user", "dzz_pass", "dzz_db", "dzz_prefix", "site_name", "site_sub", "archive_dir",
		"upload_enabled", "upload_rule", "upload_ext", "upload_max_mb"}
	out := map[string]string{}
	for _, k := range keys {
		out[k] = getSetting(k)
	}
	writeJSON(w, 200, out)
}

func saveSettings(w http.ResponseWriter, r *http.Request) {
	var b map[string]string
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	allowed := map[string]bool{
		"dzz_enabled": true, "dzz_host": true, "dzz_port": true, "dzz_user": true,
		"dzz_pass": true, "dzz_db": true, "dzz_prefix": true, "site_name": true, "site_sub": true,
		"archive_dir":    true,
		"upload_enabled": true, "upload_rule": true, "upload_ext": true, "upload_max_mb": true,
	}
	for k, v := range b {
		if !allowed[k] {
			continue
		}
		if err := setSetting(k, strings.TrimSpace(v)); err != nil {
			fail(w, 500, err.Error())
			return
		}
	}
	writeJSON(w, 200, map[string]any{"message": "设置已保存"})
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
