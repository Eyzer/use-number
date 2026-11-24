package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

func doLogin(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Mode     string `json:"mode"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	b.Username = strings.TrimSpace(b.Username)
	if b.Username == "" || b.Password == "" {
		fail(w, 400, "请输入账号和密码")
		return
	}

	var u *sessionUser
	var err error
	if b.Mode == "dzz" {
		u, err = loginDzz(b.Username, b.Password)
	} else {
		u, err = loginLocal(b.Username, b.Password)
	}
	if err != nil {
		fail(w, 401, err.Error())
		return
	}

	token, err := createSession(u.Username, u.IsAdmin)
	if err != nil {
		fail(w, 500, "创建会话失败")
		return
	}
	setSessionCookie(w, token)
	writeJSON(w, 200, map[string]any{"user": u})
}

func doLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_, _ = db.Exec("DELETE FROM sessions WHERE token=?", c.Value)
	}
	clearSessionCookie(w)
	writeJSON(w, 200, map[string]any{"message": "已退出"})
}
