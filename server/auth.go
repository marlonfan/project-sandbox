package main

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	sessionCookie    = "psb_session"
	sessionLifetime  = 30 * 24 * time.Hour
	codeLifetime     = 15 * time.Minute
	codeSendInterval = 60 * time.Second
	codeMaxAttempts  = 5
	pwIterations     = 300000
)

var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// ---------- 密码散列（PBKDF2-HMAC-SHA256，格式 pbkdf2$sha256$iter$salt$hash）----------

func hashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	h, err := pbkdf2.Key(sha256.New, pw, salt, pwIterations, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2$sha256$%d$%s$%s", pwIterations, hex.EncodeToString(salt), hex.EncodeToString(h)), nil
}

func checkPassword(pw, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 5 || parts[0] != "pbkdf2" || parts[1] != "sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[2])
	if err != nil || iter < 1 {
		return false
	}
	salt, err := hex.DecodeString(parts[3])
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(parts[4])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ---------- 会话 ----------

func newSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func setSessionCookie(w http.ResponseWriter, token string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(maxAge.Seconds()),
	})
}

func currentUserID(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return ""
	}
	var uid string
	var exp int64
	if err := db.QueryRow(`SELECT user_id, expires_at FROM sessions WHERE token = ?`, c.Value).Scan(&uid, &exp); err != nil {
		return ""
	}
	if exp < time.Now().UnixMilli() {
		return ""
	}
	return uid
}

func requireUser(w http.ResponseWriter, r *http.Request) string {
	uid := currentUserID(r)
	if uid == "" {
		fail(w, 401, "未登录")
	}
	return uid
}

func userByID(id string) (User, bool) {
	var u User
	err := db.QueryRow(`SELECT id, email FROM users WHERE id = ?`, id).Scan(&u.ID, &u.Email)
	return u, err == nil
}

func createSession(w http.ResponseWriter, userID string) error {
	// 顺带清理过期会话
	_, _ = db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now().UnixMilli())
	token, err := newSessionToken()
	if err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)`,
		token, userID, time.Now().Add(sessionLifetime).UnixMilli()); err != nil {
		return err
	}
	setSessionCookie(w, token, sessionLifetime)
	return nil
}

// claimOrphanBoards 把历史遗留（无归属）的沙盘划给当前登录的首个用户。
func claimOrphanBoards(userID string) {
	_, _ = db.Exec(`UPDATE boards SET owner = ? WHERE owner = ''`, userID)
}

// ---------- 注册 / 验证 / 登录 / 登出 ----------

func handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !emailRe.MatchString(email) {
		fail(w, 400, "邮箱格式不正确")
		return
	}
	if len(req.Password) < 8 {
		fail(w, 400, "密码至少 8 位")
		return
	}

	var verified int
	var existsID string
	err := db.QueryRow(`SELECT id, verified FROM users WHERE email = ?`, email).Scan(&existsID, &verified)
	if err == nil && verified == 1 {
		fail(w, 409, "该邮箱已注册，直接登录即可")
		return
	}

	code, err := func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		// 发送频率限制
		var sentAt int64
		_ = db.QueryRow(`SELECT sent_at FROM email_codes WHERE email = ?`, email).Scan(&sentAt)
		if time.Now().UnixMilli()-sentAt < codeSendInterval.Milliseconds() {
			return "", fmt.Errorf("rate")
		}

		hash, err := hashPassword(req.Password)
		if err != nil {
			return "", err
		}
		if existsID == "" {
			if _, err = db.Exec(`INSERT INTO users (id, email, pw_hash, verified, created_at) VALUES (?, ?, ?, 0, ?)`,
				newID(), email, hash, time.Now().UnixMilli()); err != nil {
				return "", err
			}
		} else {
			if _, err = db.Exec(`UPDATE users SET pw_hash = ? WHERE email = ?`, hash, email); err != nil {
				return "", err
			}
		}

		code := fmt.Sprintf("%06d", randInt(1000000))
		if _, err = db.Exec(`
			INSERT INTO email_codes (email, code, expires_at, attempts, sent_at) VALUES (?, ?, ?, 0, ?)
			ON CONFLICT(email) DO UPDATE SET code = excluded.code, expires_at = excluded.expires_at, attempts = 0, sent_at = excluded.sent_at`,
			email, code, time.Now().Add(codeLifetime).UnixMilli(), time.Now().UnixMilli()); err != nil {
			return "", err
		}
		return code, nil
	}()
	if err != nil {
		if err.Error() == "rate" {
			fail(w, 429, "验证码发送太频繁，请 1 分钟后再试")
		} else {
			fail(w, 500, err.Error())
		}
		return
	}

	// 发信放在锁外：网络操作可能耗时数秒，不能阻塞其他写请求
	if err := sendVerificationCode(cfg.SMTP, email, code); err != nil {
		fail(w, 502, "验证邮件发送失败: "+err.Error())
		return
	}
	if cfg.Debug {
		logf("[DEBUG] 注册验证码 %s -> %s", email, code)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	code := strings.TrimSpace(req.Code)

	mu.Lock()
	defer mu.Unlock()
	var wantCode string
	var exp, attempts int64
	err := db.QueryRow(`SELECT code, expires_at, attempts FROM email_codes WHERE email = ?`, email).
		Scan(&wantCode, &exp, &attempts)
	if err != nil {
		fail(w, 400, "请先获取验证码")
		return
	}
	if exp < time.Now().UnixMilli() {
		fail(w, 400, "验证码已过期，请重新发送")
		return
	}
	if attempts >= codeMaxAttempts {
		fail(w, 429, "尝试次数过多，请重新发送验证码")
		return
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(wantCode)) != 1 {
		_, _ = db.Exec(`UPDATE email_codes SET attempts = attempts + 1 WHERE email = ?`, email)
		remain := codeMaxAttempts - int(attempts) - 1
		fail(w, 400, fmt.Sprintf("验证码不正确（还可尝试 %d 次）", remain))
		return
	}

	var uid string
	if err := db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&uid); err != nil {
		fail(w, 400, "请先获取验证码")
		return
	}
	if _, err := db.Exec(`UPDATE users SET verified = 1 WHERE id = ?`, uid); err != nil {
		fail(w, 500, err.Error())
		return
	}
	if _, err := db.Exec(`DELETE FROM email_codes WHERE email = ?`, email); err != nil {
		fail(w, 500, err.Error())
		return
	}
	claimOrphanBoards(uid)
	if err := createSession(w, uid); err != nil {
		fail(w, 500, err.Error())
		return
	}
	u, _ := userByID(uid)
	writeJSON(w, 200, u)
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))

	mu.Lock()
	defer mu.Unlock()
	var id, pwHash string
	var verified int
	err := db.QueryRow(`SELECT id, pw_hash, verified FROM users WHERE email = ?`, email).Scan(&id, &pwHash, &verified)
	if err != nil || !checkPassword(req.Password, pwHash) {
		fail(w, 401, "邮箱或密码不正确")
		return
	}
	if verified != 1 {
		writeJSON(w, 403, map[string]string{"error": "邮箱尚未验证，请先完成注册验证", "code": "unverified"})
		return
	}
	claimOrphanBoards(id)
	if err := createSession(w, id); err != nil {
		fail(w, 500, err.Error())
		return
	}
	u, _ := userByID(id)
	writeJSON(w, 200, u)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		_, _ = db.Exec(`DELETE FROM sessions WHERE token = ?`, c.Value)
	}
	setSessionCookie(w, "", 0)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleMe(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	if uid == "" {
		fail(w, 401, "未登录")
		return
	}
	u, ok := userByID(uid)
	if !ok {
		fail(w, 401, "未登录")
		return
	}
	writeJSON(w, 200, u)
}

func randInt(n int) int {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	v := int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])
	if v < 0 {
		v = -v
	}
	return v % n
}
