package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	_ "modernc.org/sqlite"
)

var db *sql.DB

const schemaSQL = `
CREATE TABLE IF NOT EXISTS user_groups (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name VARCHAR(100) UNIQUE NOT NULL,
	description TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	username VARCHAR(50) UNIQUE NOT NULL,
	password_hash VARCHAR(255) NOT NULL,
	nickname VARCHAR(100),
	group_id INTEGER,
	is_admin BOOLEAN DEFAULT 0,
	remark TEXT,
	auth_source VARCHAR(20) DEFAULT 'local',
	dzz_uid INTEGER DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS doc_number_types (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	prefix VARCHAR(100) NOT NULL,
	suffix VARCHAR(20) NOT NULL,
	current_num INTEGER DEFAULT 1,
	group_id INTEGER,
	description TEXT,
	is_active BOOLEAN DEFAULT 1,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS doc_number_records (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	doc_number VARCHAR(200) NOT NULL,
	title VARCHAR(500) NOT NULL,
	user_id INTEGER,
	group_id INTEGER,
	type_id INTEGER,
	seq_num INTEGER DEFAULT 0,
	receive_person VARCHAR(100) DEFAULT '',
	is_archived BOOLEAN DEFAULT 0,
	archive_file VARCHAR(500) DEFAULT '',
	received_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	remark TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS settings (
	key VARCHAR(100) PRIMARY KEY,
	value TEXT
);

CREATE TABLE IF NOT EXISTS sessions (
	token VARCHAR(64) PRIMARY KEY,
	username VARCHAR(50) NOT NULL,
	is_admin BOOLEAN DEFAULT 0,
	expires_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_records_received ON doc_number_records(received_at);
CREATE INDEX IF NOT EXISTS idx_records_type ON doc_number_records(type_id);
`

func initDB(dir string) error {
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	dbPath := filepath.Join(dataDir, "use-number.db")
	dsn := "file:" + filepath.ToSlash(dbPath)

	var err error
	db, err = sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	// SQLite 单写者，串行化连接可彻底避免并发取号重号
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	if _, err = db.Exec("PRAGMA busy_timeout=8000"); err != nil {
		return fmt.Errorf("设置 busy_timeout 失败: %w", err)
	}
	if _, err = db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("设置 WAL 失败: %w", err)
	}
	if _, err = db.Exec(schemaSQL); err != nil {
		return err
	}
	if err = migrate(); err != nil {
		return err
	}
	return seed()
}

func hasColumn(table, column string) bool {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}

func migrate() error {
	additions := []struct{ table, column, ddl string }{
		{"doc_number_records", "receive_person", "ALTER TABLE doc_number_records ADD COLUMN receive_person VARCHAR(100) DEFAULT ''"},
		{"doc_number_records", "is_archived", "ALTER TABLE doc_number_records ADD COLUMN is_archived BOOLEAN DEFAULT 0"},
		{"doc_number_records", "seq_num", "ALTER TABLE doc_number_records ADD COLUMN seq_num INTEGER DEFAULT 0"},
		{"doc_number_records", "archive_file", "ALTER TABLE doc_number_records ADD COLUMN archive_file VARCHAR(500) DEFAULT ''"},
		{"users", "auth_source", "ALTER TABLE users ADD COLUMN auth_source VARCHAR(20) DEFAULT 'local'"},
		{"users", "dzz_uid", "ALTER TABLE users ADD COLUMN dzz_uid INTEGER DEFAULT 0"},
	}
	for _, a := range additions {
		if !hasColumn(a.table, a.column) {
			if _, err := db.Exec(a.ddl); err != nil {
				return fmt.Errorf("迁移字段 %s.%s 失败: %w", a.table, a.column, err)
			}
		}
	}
	// 旧记录没有领取人字段，用空串兜底
	if _, err := db.Exec("UPDATE doc_number_records SET receive_person='' WHERE receive_person IS NULL"); err != nil {
		return err
	}
	// 老数据没有序号列，从发文字号里回填，保证「按号码倒序」不是字符串排序
	if err := backfillSeq(); err != nil {
		return err
	}
	return nil
}

func backfillSeq() error {
	rows, err := db.Query("SELECT id, doc_number FROM doc_number_records WHERE COALESCE(seq_num,0) = 0")
	if err != nil {
		return err
	}
	type pair struct {
		id  int64
		num int
	}
	fix := []pair{}
	for rows.Next() {
		var id int64
		var doc string
		if err := rows.Scan(&id, &doc); err != nil {
			rows.Close()
			return err
		}
		if n := extractSeq(doc); n > 0 {
			fix = append(fix, pair{id, n})
		}
	}
	rows.Close()
	for _, p := range fix {
		if _, err := db.Exec("UPDATE doc_number_records SET seq_num=? WHERE id=?", p.num, p.id); err != nil {
			return err
		}
	}
	return nil
}

// 从发文字号里取数字部分：「国发〔2026〕102号」→ 102
// 从尾部往前跳过后缀的非数字字符，再取连续数字，兼容各种括号写法
func extractSeq(docNumber string) int {
	rs := []rune(strings.TrimSpace(docNumber))
	i := len(rs) - 1
	for i >= 0 && !unicode.IsDigit(rs[i]) {
		i--
	}
	if i < 0 {
		return 0
	}
	end := i
	for i >= 0 && unicode.IsDigit(rs[i]) {
		i--
	}
	n, err := strconv.Atoi(string(rs[i+1 : end+1]))
	if err != nil {
		return 0
	}
	return n
}

func seed() error {
	// 管理员账号：原库可能带旧哈希，统一重置为本程序可识别的格式
	var adminHash string
	err := db.QueryRow("SELECT password_hash FROM users WHERE username='admin'").Scan(&adminHash)
	if err == sql.ErrNoRows {
		_, err = db.Exec("INSERT INTO users(username, password_hash, nickname, is_admin, auth_source) VALUES(?,?,?,1,'local')",
			"admin", hashPassword("admin123"), "系统管理员")
		if err != nil {
			return err
		}
	} else if err == nil {
		if !strings.HasPrefix(adminHash, "sha256$") {
			_, err = db.Exec("UPDATE users SET password_hash=?, is_admin=1, auth_source='local' WHERE username='admin'", hashPassword("admin123"))
			if err != nil {
				return err
			}
		}
	}

	// 初始库只放配置项，不预置任何字号类型与示例数据
	defaults := map[string]string{
		"dzz_enabled":    "0",
		"dzz_host":       "127.0.0.1",
		"dzz_port":       "3306",
		"dzz_user":       "root",
		"dzz_pass":       "",
		"dzz_db":         "dzzoffice",
		"dzz_prefix":     "dzz_",
		"site_name":      "发文字号取号系统",
		"site_sub":       "内部公文发文字号统一管理",
		"archive_dir":    "",
		"upload_enabled": "1",
		"upload_rule":    "{字号}-{标题}",
		"upload_ext":     "pdf,doc,docx,ofd,wps,xls,xlsx,ppt,pptx,jpg,jpeg,png,zip,rar,7z",
		"upload_max_mb":  "50",
	}
	for k, v := range defaults {
		if _, err := db.Exec("INSERT OR IGNORE INTO settings(key, value) VALUES(?,?)", k, v); err != nil {
			return err
		}
	}
	return nil
}

// ---------- 口令 ----------

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func hashPassword(pw string) string {
	salt := randHex(8)
	sum := sha256.Sum256([]byte(salt + pw))
	return "sha256$" + salt + "$" + hex.EncodeToString(sum[:])
}

func verifyPassword(stored, pw string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 3 || parts[0] != "sha256" {
		return false
	}
	sum := sha256.Sum256([]byte(parts[1] + pw))
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(parts[2])) == 1
}

// ---------- 设置 ----------

func getSetting(key string) string {
	var v sql.NullString
	_ = db.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&v)
	return v.String
}

func setSetting(key, value string) error {
	_, err := db.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value)
	return err
}

// ---------- 会话 ----------

const sessionCookie = "use_number_sess"

type sessionUser struct {
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	IsAdmin  bool   `json:"is_admin"`
	Source   string `json:"source"`
}

func createSession(username string, isAdmin bool) (string, error) {
	token := randHex(24)
	exp := time.Now().Add(7 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	_, err := db.Exec("INSERT INTO sessions(token, username, is_admin, expires_at) VALUES(?,?,?,?)", token, username, boolToInt(isAdmin), exp)
	return token, err
}

func lookupSession(token string) *sessionUser {
	var username, exp string
	var isAdmin int
	err := db.QueryRow("SELECT username, is_admin, expires_at FROM sessions WHERE token=?", token).Scan(&username, &isAdmin, &exp)
	if err != nil {
		return nil
	}
	if exp < time.Now().Format("2006-01-02 15:04:05") {
		_, _ = db.Exec("DELETE FROM sessions WHERE token=?", token)
		return nil
	}
	u := &sessionUser{Username: username, IsAdmin: isAdmin == 1}
	_ = db.QueryRow("SELECT COALESCE(nickname,''), COALESCE(auth_source,'local') FROM users WHERE username=?", username).
		Scan(&u.Nickname, &u.Source)
	if u.Nickname == "" {
		u.Nickname = username
	}
	return u
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nowStr() string {
	return time.Now().Format("2006-01-02 15:04:05")
}
