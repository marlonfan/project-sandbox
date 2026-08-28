package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const historyMax = 20

type SMTPConfig struct {
	Host     string
	Port     int
	User     string
	Pass     string
	FromName string
}

type Config struct {
	Addr     string `json:"addr"`
	DBPath   string `json:"db"`
	HTMLPath string `json:"html"`
	Debug    bool   `json:"debug"`
	SMTP     SMTPConfig `json:"-"`
}

type Board struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

var (
	db  *sql.DB
	mu  sync.Mutex // SQLite 单写者，写操作串行化
	cfg Config
)

func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		fail(w, 400, "请求体不是合法 JSON")
		return false
	}
	return true
}

func logf(format string, args ...any) {
	log.Printf(format, args...)
}

// ---------- 配置加载：默认值 < config.json < .env < 环境变量 < 命令行参数 ----------
// SMTP 凭证只从 .env / 环境变量读取，不落 config.json（避免密码进版本库）。

// loadEnvFile 极简 .env 解析：KEY=VALUE，# 注释，忽略空行，支持成对引号。
// 已存在的环境变量优先，不被 .env 覆盖。
func loadEnvFile(dir string) {
	raw, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
}

func envStr(key string) string {
	return os.Getenv(key)
}

func loadConfig() {
	cfg = Config{
		Addr: ":8787", DBPath: "sandbox.db", HTMLPath: "project-sandbox.html",
		SMTP: SMTPConfig{Port: 465, FromName: "项目沙盘"},
	}

	// .env 先于 config.json 读入环境（不覆盖已有环境变量）；
	// 之后的 os.Getenv 阶段统一生效，优先级：环境变量 > .env
	// Docker 部署约定数据卷挂载在 /data，也查找 /data/.env
	loadEnvFile(".")
	loadEnvFile("/data")
	loadEnvFile(filepath.Dir(os.Args[0]))

	// config.json：只承载非敏感项（addr/db/html/debug）
	for _, dir := range []string{".", filepath.Dir(os.Args[0])} {
		p := filepath.Join(dir, "config.json")
		if raw, err := os.ReadFile(p); err == nil {
			if err := json.Unmarshal(raw, &cfg); err != nil {
				log.Fatalf("解析 %s 失败: %v", p, err)
			}
			break
		}
	}

	if v := envStr("PSB_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := envStr("PSB_DB"); v != "" {
		cfg.DBPath = v
	}
	if v := envStr("PSB_HTML"); v != "" {
		cfg.HTMLPath = v
	}
	if envStr("PSB_DEBUG") == "1" {
		cfg.Debug = true
	}
	if v := envStr("PSB_SMTP_HOST"); v != "" {
		cfg.SMTP.Host = v
	}
	if v := envStr("PSB_SMTP_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.SMTP.Port = n
		}
	}
	if v := envStr("PSB_SMTP_USER"); v != "" {
		cfg.SMTP.User = v
	}
	if v := envStr("PSB_SMTP_PASS"); v != "" {
		cfg.SMTP.Pass = v
	}
	if v := envStr("PSB_SMTP_FROM"); v != "" {
		cfg.SMTP.FromName = v
	}

	flag.StringVar(&cfg.Addr, "addr", cfg.Addr, "监听地址")
	flag.StringVar(&cfg.DBPath, "db", cfg.DBPath, "SQLite 数据库文件路径")
	flag.StringVar(&cfg.HTMLPath, "html", cfg.HTMLPath, "前端 HTML 文件路径")
	flag.Parse()
}

// ---------- boards ----------

func handleListBoards(w http.ResponseWriter, r *http.Request) {
	uid := requireUser(w, r)
	if uid == "" {
		return
	}
	rows, err := db.Query(`SELECT id, name, created_at, updated_at FROM boards WHERE owner = ? ORDER BY updated_at DESC`, uid)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	defer rows.Close()
	boards := []Board{}
	for rows.Next() {
		var b Board
		if err := rows.Scan(&b.ID, &b.Name, &b.CreatedAt, &b.UpdatedAt); err != nil {
			fail(w, 500, err.Error())
			return
		}
		boards = append(boards, b)
	}
	writeJSON(w, 200, boards)
}

func handleCreateBoard(w http.ResponseWriter, r *http.Request) {
	uid := requireUser(w, r)
	if uid == "" {
		return
	}
	var req struct {
		Name  string          `json:"name"`
		State json.RawMessage `json:"state"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "未命名沙盘"
	}
	now := time.Now().UnixMilli()
	b := Board{ID: newID(), Name: name, CreatedAt: now, UpdatedAt: now}

	mu.Lock()
	tx, err := db.Begin()
	if err != nil {
		mu.Unlock()
		fail(w, 500, err.Error())
		return
	}
	if _, err := tx.Exec(`INSERT INTO boards (id, name, state, owner, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		b.ID, b.Name, string(req.State), uid, b.CreatedAt, b.UpdatedAt); err != nil {
		tx.Rollback()
		mu.Unlock()
		fail(w, 500, err.Error())
		return
	}
	if len(req.State) > 0 {
		if _, err := tx.Exec(`INSERT INTO snapshots (board_id, ts, data) VALUES (?, ?, ?)`,
			b.ID, now, string(req.State)); err != nil {
			tx.Rollback()
			mu.Unlock()
			fail(w, 500, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		mu.Unlock()
		fail(w, 500, err.Error())
		return
	}
	mu.Unlock()
	writeJSON(w, 200, b)
}

func handlePatchBoard(w http.ResponseWriter, r *http.Request) {
	uid := requireUser(w, r)
	if uid == "" {
		return
	}
	id := r.PathValue("id")
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		fail(w, 400, "名称不能为空")
		return
	}
	mu.Lock()
	defer mu.Unlock()
	res, err := db.Exec(`UPDATE boards SET name = ?, updated_at = ? WHERE id = ? AND owner = ?`, name, time.Now().UnixMilli(), id, uid)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		fail(w, 404, "沙盘不存在")
		return
	}
	var b Board
	err = db.QueryRow(`SELECT id, name, created_at, updated_at FROM boards WHERE id = ?`, id).
		Scan(&b.ID, &b.Name, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, b)
}

func handleDeleteBoard(w http.ResponseWriter, r *http.Request) {
	uid := requireUser(w, r)
	if uid == "" {
		return
	}
	id := r.PathValue("id")
	mu.Lock()
	defer mu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	tx.Exec(`DELETE FROM snapshots WHERE board_id IN (SELECT id FROM boards WHERE id = ? AND owner = ?)`, id, uid)
	res, err := tx.Exec(`DELETE FROM boards WHERE id = ? AND owner = ?`, id, uid)
	if err != nil {
		tx.Rollback()
		fail(w, 500, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		fail(w, 404, "沙盘不存在")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------- state / history ----------

func handleGetState(w http.ResponseWriter, r *http.Request) {
	uid := requireUser(w, r)
	if uid == "" {
		return
	}
	id := r.PathValue("id")
	var raw sql.NullString
	err := db.QueryRow(`SELECT state FROM boards WHERE id = ? AND owner = ?`, id, uid).Scan(&raw)
	if err == sql.ErrNoRows {
		fail(w, 404, "沙盘不存在")
		return
	}
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	var state json.RawMessage
	if raw.Valid && strings.TrimSpace(raw.String) != "" {
		state = json.RawMessage(raw.String)
	}
	writeJSON(w, 200, map[string]any{"state": state})
}

func handlePutState(w http.ResponseWriter, r *http.Request) {
	uid := requireUser(w, r)
	if uid == "" {
		return
	}
	id := r.PathValue("id")
	var req struct {
		State json.RawMessage `json:"state"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	data := strings.TrimSpace(string(req.State))

	mu.Lock()
	defer mu.Unlock()
	var exists int
	if err := db.QueryRow(`SELECT COUNT(1) FROM boards WHERE id = ? AND owner = ?`, id, uid).Scan(&exists); err != nil || exists == 0 {
		fail(w, 404, "沙盘不存在")
		return
	}

	now := time.Now().UnixMilli()
	tx, err := db.Begin()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	if _, err := tx.Exec(`UPDATE boards SET state = ?, updated_at = ? WHERE id = ?`, data, now, id); err != nil {
		tx.Rollback()
		fail(w, 500, err.Error())
		return
	}

	// 与最新快照相同则不留档（与前端 pushHistory 去重语义一致）
	var lastData string
	same := false
	err = tx.QueryRow(`SELECT data FROM snapshots WHERE board_id = ? ORDER BY ts DESC LIMIT 1`, id).Scan(&lastData)
	if err == nil && lastData == data {
		same = true
	}
	if !same {
		if _, err := tx.Exec(`INSERT INTO snapshots (board_id, ts, data) VALUES (?, ?, ?)`, id, now, data); err != nil {
			tx.Rollback()
			fail(w, 500, err.Error())
			return
		}
		if _, err := tx.Exec(`DELETE FROM snapshots WHERE board_id = ? AND ts NOT IN (
			SELECT ts FROM snapshots WHERE board_id = ? ORDER BY ts DESC LIMIT ?)`, id, id, historyMax); err != nil {
			tx.Rollback()
			fail(w, 500, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleGetHistory(w http.ResponseWriter, r *http.Request) {
	uid := requireUser(w, r)
	if uid == "" {
		return
	}
	id := r.PathValue("id")
	rows, err := db.Query(`
		SELECT s.ts, s.data FROM snapshots s
		JOIN boards b ON b.id = s.board_id
		WHERE s.board_id = ? AND b.owner = ?
		ORDER BY s.ts ASC`, id, uid)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	defer rows.Close()
	type snap struct {
		Ts   int64           `json:"ts"`
		Data json.RawMessage `json:"data"`
	}
	list := []snap{}
	for rows.Next() {
		var s snap
		var data string
		if err := rows.Scan(&s.Ts, &data); err != nil {
			fail(w, 500, err.Error())
			return
		}
		s.Data = json.RawMessage(data)
		list = append(list, s)
	}
	writeJSON(w, 200, list)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------- static ----------

func serveIndex(htmlPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		// 单文件前端更新频繁，禁掉缓存避免浏览器跑旧代码
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, htmlPath)
	}
}

func resolveHTMLPath(flagVal string) string {
	candidates := []string{
		flagVal,
		filepath.Join(filepath.Dir(os.Args[0]), filepath.Base(flagVal)),
		filepath.Join(filepath.Dir(os.Args[0]), "..", filepath.Base(flagVal)),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return flagVal
}

func main() {
	loadConfig()

	var err error
	db, err = sql.Open("sqlite", cfg.DBPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		log.Fatal("打开数据库失败:", err)
	}
	schema := `
	CREATE TABLE IF NOT EXISTS boards (
		id         TEXT PRIMARY KEY,
		name       TEXT NOT NULL,
		state      TEXT,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS snapshots (
		board_id TEXT NOT NULL,
		ts       INTEGER NOT NULL,
		data     TEXT NOT NULL,
		PRIMARY KEY (board_id, ts)
	);
	CREATE TABLE IF NOT EXISTS users (
		id         TEXT PRIMARY KEY,
		email      TEXT UNIQUE NOT NULL,
		pw_hash    TEXT NOT NULL,
		verified   INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS sessions (
		token      TEXT PRIMARY KEY,
		user_id    TEXT NOT NULL,
		expires_at INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS email_codes (
		email      TEXT PRIMARY KEY,
		code       TEXT NOT NULL,
		expires_at INTEGER NOT NULL,
		attempts   INTEGER NOT NULL DEFAULT 0,
		sent_at    INTEGER NOT NULL
	);`
	if _, err := db.Exec(schema); err != nil {
		log.Fatal("初始化表结构失败:", err)
	}
	// 旧库迁移：boards 补 owner 列
	if !columnExists(db, "boards", "owner") {
		if _, err := db.Exec(`ALTER TABLE boards ADD COLUMN owner TEXT NOT NULL DEFAULT ''`); err != nil {
			log.Fatal("迁移 boards.owner 失败:", err)
		}
		log.Printf("已迁移：boards 表新增 owner 列，老数据将在首个用户登录时归属")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", handleHealth)
	mux.HandleFunc("POST /api/auth/register", handleRegister)
	mux.HandleFunc("POST /api/auth/verify", handleVerify)
	mux.HandleFunc("POST /api/auth/login", handleLogin)
	mux.HandleFunc("POST /api/auth/logout", handleLogout)
	mux.HandleFunc("GET /api/auth/me", handleMe)
	mux.HandleFunc("GET /api/boards", handleListBoards)
	mux.HandleFunc("POST /api/boards", handleCreateBoard)
	mux.HandleFunc("PATCH /api/boards/{id}", handlePatchBoard)
	mux.HandleFunc("DELETE /api/boards/{id}", handleDeleteBoard)
	mux.HandleFunc("GET /api/boards/{id}/state", handleGetState)
	mux.HandleFunc("PUT /api/boards/{id}/state", handlePutState)
	mux.HandleFunc("GET /api/boards/{id}/history", handleGetHistory)

	staticPath := resolveHTMLPath(cfg.HTMLPath)
	if _, err := os.Stat(staticPath); err != nil {
		log.Printf("警告：找不到前端文件 %s，仅提供 API", staticPath)
	} else {
		mux.HandleFunc("GET /{$}", serveIndex(staticPath))
	}

	// 跨域：托管在远程的页面也可以指向本地/自建服务端（?api=http://host:port）。
	// 登录态靠 Cookie，credentialed 请求必须回显具体 Origin 而不是 *。
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		mux.ServeHTTP(w, r)
	})

	fmt.Printf("项目沙盘服务端已启动: http://localhost%s  (db: %s)\n", cfg.Addr, cfg.DBPath)
	if cfg.SMTP.Host != "" && cfg.SMTP.User != "" && cfg.SMTP.Pass != "" {
		log.Printf("SMTP 已配置: %s:%d (%s)", cfg.SMTP.Host, cfg.SMTP.Port, cfg.SMTP.User)
	} else {
		log.Printf("提示：SMTP 未完整配置（.env 或 PSB_SMTP_* 环境变量），注册验证码将无法发送")
	}
	log.Fatal(http.ListenAndServe(cfg.Addr, handler))
}

func columnExists(db *sql.DB, table, col string) bool {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			continue
		}
		if name == col {
			return true
		}
	}
	return false
}
