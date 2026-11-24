package main

import (
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func getSessionUser(r *http.Request) *sessionUser {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	return lookupSession(c.Value)
}

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   7 * 24 * 3600,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// 本地账号登录（管理员 admin 或后台自建账号）
func loginLocal(username, password string) (*sessionUser, error) {
	var hash, nickname, source string
	var isAdmin int
	err := db.QueryRow("SELECT password_hash, COALESCE(nickname,''), COALESCE(auth_source,'local'), is_admin FROM users WHERE username=?", username).
		Scan(&hash, &nickname, &source, &isAdmin)
	if err == sql.ErrNoRows {
		return nil, errors.New("账号或密码错误")
	}
	if err != nil {
		return nil, err
	}
	if source == "dzz" {
		return nil, errors.New("该账号为 DzzOffice 账号，请使用 DzzOffice 登录")
	}
	if !verifyPassword(hash, password) {
		return nil, errors.New("账号或密码错误")
	}
	if isAdmin != 1 {
		return nil, errors.New("该账号没有后台管理权限")
	}
	if nickname == "" {
		nickname = username
	}
	return &sessionUser{Username: username, Nickname: nickname, IsAdmin: true, Source: "local"}, nil
}

var safeIdent = regexp.MustCompile(`^[A-Za-z0-9_]*$`)

func dzzDSN() (string, string, error) {
	host := strings.TrimSpace(getSetting("dzz_host"))
	port := strings.TrimSpace(getSetting("dzz_port"))
	user := getSetting("dzz_user")
	pass := getSetting("dzz_pass")
	name := strings.TrimSpace(getSetting("dzz_db"))
	prefix := getSetting("dzz_prefix")
	if host == "" || port == "" || user == "" || name == "" {
		return "", "", errors.New("DzzOffice 连接参数不完整，请在系统设置中填写")
	}
	if !safeIdent.MatchString(prefix) {
		prefix = "dzz_"
	}
	if prefix != "" && !strings.HasSuffix(prefix, "_") {
		prefix += "_"
	}
	// IPv6 地址需要方括号，否则 tcp 地址解析失败
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&timeout=5s&readTimeout=8s&writeTimeout=8s",
		user, pass, host, port, name)
	return dsn, prefix, nil
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// DzzOffice 共库验证登录，并映射为本地后台账号
func loginDzz(username, password string) (*sessionUser, error) {
	if getSetting("dzz_enabled") != "1" {
		return nil, errors.New("DzzOffice 登录未启用")
	}
	dsn, prefix, err := dzzDSN()
	if err != nil {
		return nil, err
	}
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, errors.New("DzzOffice 数据库连接失败")
	}
	defer conn.Close()
	conn.SetConnMaxLifetime(30 * time.Second)
	if err := conn.Ping(); err != nil {
		return nil, errors.New("DzzOffice 数据库连接失败，请检查地址与账号")
	}

	query := fmt.Sprintf(`SELECT u.uid, u.username, u.password, u.salt, COALESCE(u.nickname,''), u.status, u.adminid, u.groupid,
		COALESCE(ug.radminid,0), COALESCE(ug.grouptitle,'')
		FROM %suser u LEFT JOIN %susergroup ug ON ug.groupid = u.groupid
		WHERE u.username = ? LIMIT 1`, prefix, prefix)

	var uid, status int
	var adminid, groupid, radminid int
	var uname, stored, salt, nickname, groupTitle string
	err = conn.QueryRow(query, username).Scan(&uid, &uname, &stored, &salt, &nickname, &status, &adminid, &groupid, &radminid, &groupTitle)
	if err == sql.ErrNoRows {
		return nil, errors.New("DzzOffice 账号不存在")
	}
	if err != nil {
		return nil, errors.New("DzzOffice 数据表结构异常，请联系管理员")
	}
	if status > 0 {
		return nil, errors.New("该 DzzOffice 账号已被禁用")
	}
	if md5hex(md5hex(password)+salt) != stored {
		return nil, errors.New("账号或密码错误")
	}

	isAdmin := groupid == 1 || radminid == 1 || adminid == 1
	if !isAdmin {
		return nil, errors.New("该账号不在 DzzOffice 管理员组，无法进入后台")
	}
	if nickname == "" {
		nickname = uname
	}

	// 映射本地账号
	var localSource string
	var localID int64
	err = db.QueryRow("SELECT id, COALESCE(auth_source,'local') FROM users WHERE username=?", uname).Scan(&localID, &localSource)
	if err == sql.ErrNoRows {
		res, insErr := db.Exec("INSERT INTO users(username, password_hash, nickname, is_admin, auth_source, dzz_uid, remark) VALUES(?,?,?,1,'dzz',?,?)",
			uname, hashPassword(randHex(16)), nickname, uid, "DzzOffice 同步账号")
		if insErr != nil {
			return nil, insErr
		}
		localID, _ = res.LastInsertId()
	} else if err != nil {
		return nil, err
	} else {
		if localSource == "local" {
			return nil, errors.New("该用户名已被本系统本地账号占用")
		}
		_, _ = db.Exec("UPDATE users SET nickname=?, is_admin=1, dzz_uid=?, updated_at=? WHERE id=?", nickname, uid, nowStr(), localID)
	}

	return &sessionUser{Username: uname, Nickname: nickname, IsAdmin: true, Source: "dzz"}, nil
}

// 测试 DzzOffice 连接，供系统设置页使用
func testDzz() (string, error) {
	dsn, prefix, err := dzzDSN()
	if err != nil {
		return "", err
	}
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return "", errors.New("连接参数格式错误")
	}
	defer conn.Close()
	if err := conn.Ping(); err != nil {
		return "", errors.New("连接失败：" + err.Error())
	}
	var n int
	if err := conn.QueryRow("SELECT COUNT(*) FROM " + prefix + "user").Scan(&n); err != nil {
		return "", errors.New("连接成功，但用户表不可读：" + err.Error())
	}
	return fmt.Sprintf("连接成功，读取到 %d 个 DzzOffice 账号", n), nil
}

// 管理员判定中间件
func requireAdmin(w http.ResponseWriter, r *http.Request) *sessionUser {
	u := getSessionUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "未登录或登录已过期"})
		return nil
	}
	if !u.IsAdmin {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "无后台管理权限"})
		return nil
	}
	return u
}
