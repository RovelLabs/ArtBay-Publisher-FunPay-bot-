package main

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	htmltemplate "html/template"
	"io"
	"log"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/textproto"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	appName                   = "ArtBay Publisher"
	appVersion                = "4.0.3"
	listenAddr                = "127.0.0.1:8765"
	autoPublishBatchThreshold = 10
)

type Config struct {
	TelegramToken string `json:"telegram_token"`
	TelegramBot   string `json:"telegram_bot,omitempty"`
	GoldenKey     string `json:"golden_key"`
	FunPayUser    string `json:"funpay_user,omitempty"`
	UserAgent     string `json:"user_agent"`
	AdminUserID   int64  `json:"admin_user_id"`
	AdminChatID   int64  `json:"admin_chat_id"`
	PairCode      string `json:"pair_code"`
	DashboardKey  string `json:"dashboard_key"`
	AutoStart     bool   `json:"autostart"`
	NotifyOrders  bool   `json:"notify_orders"`
	KnownNodes    []int  `json:"known_nodes,omitempty"`
}

type LotPackage struct {
	Version             int               `json:"version"`
	LotID               int               `json:"lot_id,omitempty"`
	NodeID              int               `json:"node_id,omitempty"`
	CategoryPath        string            `json:"category_path,omitempty"`
	CategoryURL         string            `json:"category_url,omitempty"`
	TitleRU             string            `json:"title_ru"`
	TitleEN             string            `json:"title_en"`
	DescriptionRU       string            `json:"description_ru"`
	DescriptionEN       string            `json:"description_en"`
	PaymentMsgRU        string            `json:"payment_msg_ru"`
	PaymentMsgEN        string            `json:"payment_msg_en"`
	Price               float64           `json:"price"`
	Amount              *int              `json:"amount,omitempty"`
	Active              *bool             `json:"active,omitempty"`
	DeactivateAfterSale *bool             `json:"deactivate_after_sale,omitempty"`
	RawFields           map[string]string `json:"raw_fields,omitempty"`
	ImageFiles          []string          `json:"image_files,omitempty"`
}

type StagedLot struct {
	Token      string
	TempDir    string
	ZipPath    string
	Package    LotPackage
	ImagePaths []string
	Warnings   []string
	Duplicate  bool
	Publishing bool
	CreatedAt  time.Time
}

type BatchStage struct {
	Token                 string
	Lots                  []*StagedLot
	Selected              map[int]bool
	CreatedAt             time.Time
	NextIndex             int
	OKCount               int
	FailCount             int
	SkippedCount          int
	LimitSkippedCount     int
	DuplicateSkippedCount int
	BlockedNodes          map[int]bool
	Status                string
	UpdatedAt             time.Time
}

type BulkProgress struct {
	Token                 string       `json:"token"`
	NextIndex             int          `json:"next_index"`
	OKCount               int          `json:"ok_count"`
	FailCount             int          `json:"fail_count"`
	SkippedCount          int          `json:"skipped_count"`
	LimitSkippedCount     int          `json:"limit_skipped_count"`
	DuplicateSkippedCount int          `json:"duplicate_skipped_count"`
	BlockedNodes          map[int]bool `json:"blocked_nodes,omitempty"`
	Status                string       `json:"status"`
	UpdatedAt             time.Time    `json:"updated_at"`
}

type PendingAction struct {
	Token       string
	Kind        string
	LotIDs      []int
	NewPrices   map[int]float64
	ImagePaths  map[int]string
	TempDir     string
	Description string
	Executing   bool
	CreatedAt   time.Time
}

type BackupRecord struct {
	LotID     int               `json:"lot_id"`
	NodeID    int               `json:"node_id,omitempty"`
	Action    string            `json:"action"`
	CreatedAt time.Time         `json:"created_at"`
	Fields    map[string]string `json:"fields"`
}

type CategoryInfo struct {
	ID   int
	Game string
	Name string
	URL  string
}

type ExistingLot struct {
	ID       int
	NodeID   int
	Title    string
	Price    float64
	Amount   *int
	Active   bool
	Currency string
}

type OrderBrief struct {
	ID          string
	Description string
	Price       float64
	Currency    string
	Status      string
	DateText    string
	Buyer       string
}

type App struct {
	mu               sync.RWMutex
	cfg              Config
	cfgPath          string
	dataDir          string
	logPath          string
	backupDir        string
	bulkJobPath      string
	bulkProgressPath string
	logger           *log.Logger
	fp               *FunPayClient
	staged           map[string]*StagedLot
	batches          map[string]*BatchStage
	actions          map[string]*PendingAction
	seenOrders       map[string]bool
	ordersReady      bool
	tgOffset         int64
	lastError        string
	lastNotice       string
	bulkRunning      bool
	bulkCancel       bool
	bulkDiscard      bool
	bulkActiveToken  string
}

type cachedCreateFields struct {
	at     time.Time
	fields map[string]string
}

type cachedNodeLots struct {
	at   time.Time
	lots []ExistingLot
}

type FunPayClient struct {
	mu                sync.Mutex
	goldenKey         string
	userAgent         string
	client            *http.Client
	pageCSRFToken     string
	username          string
	userID            int
	categories        []CategoryInfo
	initAt            time.Time
	createFieldsCache map[int]cachedCreateFields
	nodeLotsCache     map[int]cachedNodeLots
	imageCache        map[string]int
	lastRequestAt     time.Time
}

type TGResponse[T any] struct {
	OK          bool   `json:"ok"`
	Result      T      `json:"result"`
	Description string `json:"description"`
}

type TGUpdate struct {
	UpdateID      int64       `json:"update_id"`
	Message       *TGMessage  `json:"message"`
	CallbackQuery *TGCallback `json:"callback_query"`
}

type TGUser struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

type TGChat struct {
	ID int64 `json:"id"`
}

type TGDocument struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
}

type TGMessage struct {
	MessageID int64       `json:"message_id"`
	From      *TGUser     `json:"from"`
	Chat      TGChat      `json:"chat"`
	Text      string      `json:"text"`
	Document  *TGDocument `json:"document"`
}

type TGCallback struct {
	ID      string     `json:"id"`
	From    TGUser     `json:"from"`
	Message *TGMessage `json:"message"`
	Data    string     `json:"data"`
}

type TGFile struct {
	FilePath string `json:"file_path"`
}

type TGBotUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

func main() {
	app, err := newApp()
	if err != nil {
		return
	}
	if hasArg("--cancel-bulk") {
		fmt.Println(app.discardBulk())
		return
	}
	defer func() {
		if r := recover(); r != nil {
			app.logger.Printf("PANIC: %v", r)
		}
	}()

	go app.cleanupLoop()
	go app.telegramLoop()
	go app.orderWatchLoop()

	mux := http.NewServeMux()
	app.registerHTTP(mux)
	srv := &http.Server{Addr: listenAddr, Handler: mux, ReadHeaderTimeout: 8 * time.Second}

	lnErr := make(chan error, 1)
	go func() {
		app.logger.Printf("Starting dashboard on http://%s", listenAddr)
		lnErr <- srv.ListenAndServe()
	}()

	if !hasArg("--background") {
		time.Sleep(250 * time.Millisecond)
		app.openDashboard()
	}

	if err := <-lnErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		app.logger.Printf("HTTP server error: %v", err)
		if strings.Contains(strings.ToLower(err.Error()), "address already in use") {
			app.openDashboard()
		}
	}
}

func newApp() (*App, error) {
	base := os.Getenv("APPDATA")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".artbaypublisher")
	} else {
		base = filepath.Join(base, "ArtBayPublisher")
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		return nil, err
	}
	logPath := filepath.Join(base, "app.log")
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	logger := log.New(lf, "", log.LstdFlags|log.Lmicroseconds)
	cfgPath := filepath.Join(base, "config.json")
	cfg := Config{
		UserAgent:    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/152 Safari/537.36",
		NotifyOrders: true,
	}
	if b, err := os.ReadFile(cfgPath); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	if cfg.DashboardKey == "" {
		cfg.DashboardKey = randomToken(16)
	}
	if cfg.PairCode == "" {
		cfg.PairCode = randomDigits(8)
	}
	backupDir := filepath.Join(base, "backups")
	_ = os.MkdirAll(backupDir, 0700)
	app := &App{
		cfg: cfg, cfgPath: cfgPath, dataDir: base, logPath: logPath, backupDir: backupDir, logger: logger,
		bulkJobPath: filepath.Join(base, "bulk-job.json"), bulkProgressPath: filepath.Join(base, "bulk-progress.json"),
		staged: map[string]*StagedLot{}, batches: map[string]*BatchStage{}, actions: map[string]*PendingAction{},
		seenOrders: map[string]bool{},
	}
	_ = app.saveConfig()
	app.resetFunPayClient()
	app.loadBulkJob()
	logger.Printf("%s v%s started (%s/%s)", appName, appVersion, runtime.GOOS, runtime.GOARCH)
	return app, nil
}

func (a *App) resetFunPayClient() {
	a.mu.RLock()
	cfg := a.cfg
	a.mu.RUnlock()
	a.fp = NewFunPayClient(cfg.GoldenKey, cfg.UserAgent)
}

func (a *App) saveConfig() error {
	a.mu.RLock()
	cfg := a.cfg
	a.mu.RUnlock()
	b, _ := json.MarshalIndent(cfg, "", "  ")
	tmp := a.cfgPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, a.cfgPath)
}

func writeJSONAtomic(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (a *App) saveBulkManifest(batch *BatchStage) error {
	batch.UpdatedAt = time.Now()
	if batch.Status == "" {
		batch.Status = "ready"
	}
	if err := writeJSONAtomic(a.bulkJobPath, batch); err != nil {
		return err
	}
	return a.saveBulkProgress(batch)
}

func (a *App) saveBulkProgress(batch *BatchStage) error {
	batch.UpdatedAt = time.Now()
	p := BulkProgress{Token: batch.Token, NextIndex: batch.NextIndex, OKCount: batch.OKCount, FailCount: batch.FailCount, SkippedCount: batch.SkippedCount, LimitSkippedCount: batch.LimitSkippedCount, DuplicateSkippedCount: batch.DuplicateSkippedCount, BlockedNodes: batch.BlockedNodes, Status: batch.Status, UpdatedAt: batch.UpdatedAt}
	return writeJSONAtomic(a.bulkProgressPath, &p)
}

func (a *App) loadBulkJob() {
	b, err := os.ReadFile(a.bulkJobPath)
	if err != nil {
		return
	}
	var batch BatchStage
	if err := json.Unmarshal(b, &batch); err != nil || batch.Token == "" || len(batch.Lots) == 0 {
		a.logger.Printf("bulk recovery: invalid manifest: %v", err)
		return
	}
	if pData, err := os.ReadFile(a.bulkProgressPath); err == nil {
		var p BulkProgress
		if json.Unmarshal(pData, &p) == nil && p.Token == batch.Token {
			batch.NextIndex, batch.OKCount, batch.FailCount, batch.SkippedCount = p.NextIndex, p.OKCount, p.FailCount, p.SkippedCount
			batch.LimitSkippedCount, batch.DuplicateSkippedCount, batch.BlockedNodes = p.LimitSkippedCount, p.DuplicateSkippedCount, p.BlockedNodes
			batch.Status, batch.UpdatedAt = p.Status, p.UpdatedAt
		}
	}
	if batch.Status == "complete" || batch.Status == "cancelled" {
		return
	}
	batch.Status = "paused"
	a.batches[batch.Token] = &batch
	a.logger.Printf("bulk recovery: restored token=%s next=%d total=%d", batch.Token, batch.NextIndex, len(batch.Lots))
}

func (a *App) clearBulkCheckpoint(token string) {
	b, err := os.ReadFile(a.bulkProgressPath)
	if err == nil {
		var p BulkProgress
		if json.Unmarshal(b, &p) == nil && p.Token != "" && p.Token != token {
			return
		}
	}
	_ = os.Remove(a.bulkProgressPath)
	_ = os.Remove(a.bulkJobPath)
}

func (a *App) registerHTTP(mux *http.ServeMux) {
	mux.HandleFunc("/", a.handleDashboard)
	mux.HandleFunc("/save", a.handleSave)
	mux.HandleFunc("/connect-telegram", a.handleConnectTelegram)
	mux.HandleFunc("/connect-funpay", a.handleConnectFunPay)
	mux.HandleFunc("/newpair", a.handleNewPair)
	mux.HandleFunc("/test-funpay", a.handleTestFunPay)
	mux.HandleFunc("/test-telegram", a.handleTestTelegram)
	mux.HandleFunc("/autostart", a.handleAutoStart)
	mux.HandleFunc("/logs", a.handleLogs)
}

func (a *App) requireKey(r *http.Request) bool {
	a.mu.RLock()
	k := a.cfg.DashboardKey
	a.mu.RUnlock()
	return r.URL.Query().Get("k") == k || r.FormValue("k") == k
}

var dashboardTemplate = htmltemplate.Must(htmltemplate.New("dashboard").Parse(`<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>ArtBay Publisher</title><style>
:root{color-scheme:dark;--bg:#07090f;--panel:#111520;--line:#252c3c;--text:#f6f7fb;--muted:#929cb0;--blue:#6d82ff;--cyan:#62d9ff;--green:#65e6a7;--red:#ff7184;--amber:#ffc857}*{box-sizing:border-box}body{margin:0;background:radial-gradient(circle at 20% -10%,#26336a 0,transparent 34%),radial-gradient(circle at 90% 12%,#17344c 0,transparent 26%),var(--bg);color:var(--text);font:15px/1.55 Inter,Segoe UI,Arial,sans-serif}.shell{max-width:1120px;margin:auto;padding:42px 22px 70px}.top{display:flex;justify-content:space-between;gap:24px;align-items:flex-end;margin-bottom:26px}.brand{font-size:13px;letter-spacing:.14em;text-transform:uppercase;color:var(--cyan);font-weight:800}.title{font-size:clamp(34px,5vw,62px);line-height:1;margin:10px 0 12px;letter-spacing:-.045em}.lead{color:var(--muted);max-width:720px;font-size:17px}.version{color:var(--muted);white-space:nowrap}.grid{display:grid;grid-template-columns:repeat(12,minmax(0,1fr));gap:16px}.card{grid-column:span 6;background:linear-gradient(145deg,#151a27e8,#0e121ce8);border:1px solid var(--line);border-radius:22px;padding:24px;box-shadow:0 22px 65px #0007;backdrop-filter:blur(14px)}.wide{grid-column:1/-1}.step{display:flex;gap:16px;align-items:flex-start}.number{width:38px;height:38px;display:grid;place-items:center;border-radius:12px;background:#28345f;color:#dfe5ff;font-weight:900}.step h2{margin:1px 0 5px;font-size:20px}.muted{color:var(--muted)}.state{display:inline-flex;align-items:center;gap:8px;margin:12px 0 18px;padding:7px 11px;border-radius:999px;background:#171d2a;border:1px solid var(--line);font-weight:700}.dot{width:8px;height:8px;border-radius:50%;background:var(--amber);box-shadow:0 0 16px currentColor}.ready .dot{background:var(--green)}label{display:block;margin:12px 0 6px;font-weight:700}input{width:100%;border:1px solid #30394d;border-radius:13px;background:#090c13;color:#fff;padding:13px 14px;outline:none}input:focus{border-color:var(--blue);box-shadow:0 0 0 3px #6d82ff24}.actions{display:flex;gap:9px;flex-wrap:wrap;margin-top:15px}button,.button{appearance:none;border:0;border-radius:13px;padding:12px 16px;background:linear-gradient(135deg,var(--blue),#8c67ee);color:white;font-weight:800;text-decoration:none;cursor:pointer}.secondary{background:#242b3b}.danger{background:#5a2630}.notice,.error{grid-column:1/-1;border-radius:15px;padding:13px 16px}.notice{background:#133827;border:1px solid #236b49;color:#b7f8d6}.error{background:#431d26;border:1px solid #7d3343;color:#ffd2d9}.flow{display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin-top:18px}.flow div{padding:16px;border-radius:16px;background:#090c13;border:1px solid var(--line)}.flow b{display:block;margin-bottom:5px}.footer{display:flex;justify-content:space-between;gap:16px;align-items:center;margin-top:18px;color:var(--muted)}code{background:#090c13;border:1px solid var(--line);border-radius:7px;padding:2px 6px}@media(max-width:760px){.top{display:block}.version{margin-top:12px}.card{grid-column:1/-1}.flow{grid-template-columns:1fr}.shell{padding:26px 14px 50px}}
</style></head><body><main class="shell"><header class="top"><div><div class="brand">ArtBay automation</div><h1 class="title">Подключи один раз.<br>Публикуй тысячами.</h1><div class="lead">Пошаговая настройка Telegram и FunPay. Ключи проверяются до сохранения, а секреты никогда не показываются обратно.</div></div><div class="version">Publisher v{{.Version}}</div></header>
<section class="grid">{{if .Notice}}<div class="notice">{{.Notice}}</div>{{end}}{{if .Error}}<div class="error">{{.Error}}</div>{{end}}
<article class="card"><div class="step"><div class="number">1</div><div><h2>Telegram-бот</h2><div class="muted">Вставь токен от BotFather. Мы проверим его через Telegram и только потом сохраним.</div></div></div><div class="state {{if .TelegramReady}}ready{{end}}"><span class="dot"></span>{{.TelegramState}}</div><form method="post" action="/connect-telegram"><input type="hidden" name="k" value="{{.Key}}"><label>Bot token</label><input name="telegram_token" type="password" autocomplete="off" placeholder="123456789:AA..." required><div class="actions"><button>Проверить и подключить</button>{{if .TelegramConfigured}}<button class="secondary" formaction="/test-telegram" formnovalidate>Тест сообщения</button>{{end}}</div></form></article>
<article class="card"><div class="step"><div class="number">2</div><div><h2>FunPay</h2><div class="muted">Golden Key проверяется реальным входом. Нерабочий ключ не заменит текущие настройки.</div></div></div><div class="state {{if .FunPayReady}}ready{{end}}"><span class="dot"></span>{{.FunPayState}}</div><form method="post" action="/connect-funpay"><input type="hidden" name="k" value="{{.Key}}"><label>Golden Key</label><input name="golden_key" type="password" autocomplete="off" placeholder="Вставь значение cookie golden_key" required><label>User-Agent</label><input name="user_agent" value="{{.UserAgent}}"><div class="actions"><button>Проверить и подключить</button>{{if .FunPayConfigured}}<button class="secondary" formaction="/test-funpay" formnovalidate>Проверить снова</button>{{end}}</div></form></article>
<article class="card wide"><div class="step"><div class="number">3</div><div><h2>Привязка владельца</h2><div class="muted">После проверки токена открой персональную ссылку. Первым владельцем станет только пользователь с одноразовым кодом.</div></div></div>{{if .PairURL}}<div class="actions"><a class="button" href="{{.PairURL}}" target="_blank" rel="noreferrer">Открыть бота и привязать</a><form method="post" action="/newpair"><input type="hidden" name="k" value="{{.Key}}"><button class="secondary">Обновить ссылку</button></form></div><p class="muted">Если ссылка не открылась: отправь боту <code>/start {{.PairCode}}</code></p>{{else}}<p class="muted">Сначала подключи Telegram-токен на шаге 1.</p>{{end}}</article>
<article class="card wide"><h2>Как будет работать публикация</h2><div class="flow"><div><b>1. Загрузи пакет</b>Отправь боту одиночный ZIP, общий ZIP с папками/вложенными ZIP или пакет карточек по ID.</div><div><b>2. Автопроверка</b>Бот проверит структуру, категории, дубликаты и подготовит очередь без тысяч предварительных запросов.</div><div><b>3. Надёжная очередь</b>Публикация идёт последовательно, учитывает лимиты FunPay и показывает живой прогресс.</div></div></article>
<article class="card"><h2>Windows</h2><div class="muted">Автозапуск: {{if .AutoStart}}включён{{else}}выключен{{end}}</div><form class="actions" method="post" action="/autostart"><input type="hidden" name="k" value="{{.Key}}"><button name="enable" value="1">Включить</button><button class="danger" name="enable" value="0">Выключить</button></form></article>
<article class="card"><h2>Диагностика</h2><div class="muted">Журнал автоматически скрывает токены и ключи.</div><div class="actions"><a class="button secondary" href="/logs?k={{.Key}}">Открыть журнал</a></div></article></section><div class="footer"><span>Работает только на этом компьютере: 127.0.0.1</span><span>Форматы: ZIP · JSON manifests · PNG/JPG/WEBP</span></div></main></body></html>`))

func (a *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if !a.requireKey(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	a.mu.RLock()
	cfg, lastErr, notice := a.cfg, a.lastError, a.lastNotice
	a.mu.RUnlock()
	tgState := "Не подключён"
	if cfg.TelegramToken != "" {
		tgState = "Старый токен · проверь заново"
	}
	if cfg.TelegramBot != "" {
		tgState = "@" + cfg.TelegramBot + " проверен"
	}
	if cfg.AdminUserID != 0 {
		tgState += " · владелец привязан"
	}
	fpState := "Не подключён"
	if cfg.GoldenKey != "" {
		fpState = "Старый ключ · проверь заново"
	}
	if cfg.FunPayUser != "" {
		fpState = cfg.FunPayUser + " · вход подтверждён"
	}
	pairURL := ""
	if cfg.TelegramBot != "" {
		pairURL = "https://t.me/" + url.PathEscape(cfg.TelegramBot) + "?start=" + url.QueryEscape(cfg.PairCode)
	}
	data := map[string]any{
		"Version": appVersion, "Key": cfg.DashboardKey, "Notice": notice, "Error": lastErr,
		"TelegramConfigured": cfg.TelegramBot != "", "TelegramReady": cfg.AdminUserID != 0,
		"TelegramState": tgState, "FunPayConfigured": cfg.FunPayUser != "", "FunPayReady": cfg.FunPayUser != "",
		"FunPayState": fpState, "PairURL": pairURL, "PairCode": cfg.PairCode,
		"UserAgent": cfg.UserAgent, "AutoStart": cfg.AutoStart,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := dashboardTemplate.Execute(w, data); err != nil {
		a.logger.Printf("dashboard template: %v", err)
	}
}

func errHTML(s string) string {
	if s == "" {
		return ""
	}
	return `<div class="bad" style="margin-top:12px">Последняя ошибка: ` + html.EscapeString(s) + `</div>`
}

func (a *App) handleSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || !a.requireKey(r) {
		http.Error(w, "Forbidden", 403)
		return
	}
	a.mu.Lock()
	ua := strings.TrimSpace(r.FormValue("user_agent"))
	if ua != "" {
		a.cfg.UserAgent = ua
	}
	a.mu.Unlock()
	_ = a.saveConfig()
	a.resetFunPayClient()
	a.setResult(nil, "Настройки сохранены.")
	a.redirectDashboard(w, r, "saved")
}

func (a *App) handleConnectTelegram(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !a.requireKey(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	token := strings.TrimSpace(r.FormValue("telegram_token"))
	if token == "" {
		a.setResult(errors.New("вставь Telegram Bot Token"), "")
		a.redirectDashboard(w, r, "telegram")
		return
	}
	bot, err := tgGetBot(token)
	if err != nil {
		a.setResult(fmt.Errorf("Telegram не принял токен: %w", sanitizeSecretError(err, token)), "")
		a.redirectDashboard(w, r, "telegram")
		return
	}
	a.mu.Lock()
	changed := a.cfg.TelegramToken != token
	a.cfg.TelegramToken = token
	a.cfg.TelegramBot = strings.TrimPrefix(bot.Username, "@")
	if changed {
		a.cfg.AdminUserID = 0
		a.cfg.AdminChatID = 0
		a.cfg.PairCode = randomDigits(8)
	}
	a.mu.Unlock()
	if err := a.saveConfig(); err != nil {
		a.setResult(err, "")
		a.redirectDashboard(w, r, "telegram")
		return
	}
	_ = a.configureTelegramUI(token)
	a.setResult(nil, "Telegram-бот проверен. Теперь открой ссылку на шаге 3 и нажми Start.")
	a.redirectDashboard(w, r, "telegram")
}

func (a *App) handleConnectFunPay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !a.requireKey(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	key := strings.TrimSpace(r.FormValue("golden_key"))
	ua := strings.TrimSpace(r.FormValue("user_agent"))
	if key == "" {
		a.setResult(errors.New("вставь FunPay golden_key"), "")
		a.redirectDashboard(w, r, "funpay")
		return
	}
	if ua == "" {
		a.mu.RLock()
		ua = a.cfg.UserAgent
		a.mu.RUnlock()
	}
	client := NewFunPayClient(key, ua)
	name, err := client.Init()
	if err != nil {
		a.setResult(fmt.Errorf("FunPay не подтвердил вход: %w", sanitizeSecretError(err, key)), "")
		a.redirectDashboard(w, r, "funpay")
		return
	}
	a.mu.Lock()
	a.cfg.GoldenKey = key
	a.cfg.UserAgent = ua
	a.cfg.FunPayUser = name
	a.fp = client
	a.mu.Unlock()
	if err := a.saveConfig(); err != nil {
		a.setResult(err, "")
	} else {
		a.setResult(nil, "FunPay подключён: "+name)
	}
	a.redirectDashboard(w, r, "funpay")
}

func (a *App) handleNewPair(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || !a.requireKey(r) {
		http.Error(w, "Forbidden", 403)
		return
	}
	a.mu.Lock()
	a.cfg.PairCode = randomDigits(8)
	a.cfg.AdminUserID = 0
	a.cfg.AdminChatID = 0
	a.mu.Unlock()
	_ = a.saveConfig()
	a.setResult(nil, "Создана новая одноразовая ссылка привязки.")
	a.redirectDashboard(w, r, "pair")
}

func (a *App) handleTestFunPay(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || !a.requireKey(r) {
		http.Error(w, "Forbidden", 403)
		return
	}
	name, err := a.fp.Init()
	if err == nil {
		a.mu.Lock()
		a.cfg.FunPayUser = name
		a.mu.Unlock()
		_ = a.saveConfig()
	}
	a.setResult(err, map[bool]string{true: "FunPay работает: " + name, false: ""}[err == nil])
	if err == nil {
		a.logger.Printf("FunPay connected as %s", name)
	}
	a.redirectDashboard(w, r, "test")
}

func (a *App) handleTestTelegram(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || !a.requireKey(r) {
		http.Error(w, "Forbidden", 403)
		return
	}
	a.mu.RLock()
	token := a.cfg.TelegramToken
	chat := a.cfg.AdminChatID
	a.mu.RUnlock()
	var err error
	if token == "" {
		err = errors.New("Telegram token не задан")
	} else if chat == 0 {
		err = errors.New("Telegram ещё не привязан")
	} else {
		err = a.tgSendMessage(chat, "✅ ArtBay Publisher: Telegram работает.", nil)
	}
	a.setResult(err, map[bool]string{true: "Тестовое сообщение отправлено в Telegram.", false: ""}[err == nil])
	a.redirectDashboard(w, r, "testtg")
}

func (a *App) handleAutoStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || !a.requireKey(r) {
		http.Error(w, "Forbidden", 403)
		return
	}
	enable := r.FormValue("enable") == "1"
	err := setWindowsAutoStart(enable)
	if err == nil {
		a.mu.Lock()
		a.cfg.AutoStart = enable
		a.mu.Unlock()
		_ = a.saveConfig()
	}
	a.setLastError(err)
	a.redirectDashboard(w, r, "autostart")
}

func (a *App) handleLogs(w http.ResponseWriter, r *http.Request) {
	if !a.requireKey(r) {
		http.Error(w, "Forbidden", 403)
		return
	}
	b, _ := os.ReadFile(a.logPath)
	if len(b) > 100000 {
		b = b[len(b)-100000:]
	}
	a.mu.RLock()
	token, key := a.cfg.TelegramToken, a.cfg.GoldenKey
	a.mu.RUnlock()
	safe := string(b)
	for _, secret := range []string{token, key} {
		if strings.TrimSpace(secret) != "" {
			safe = strings.ReplaceAll(safe, secret, "[secret]")
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, safe)
}

func (a *App) redirectDashboard(w http.ResponseWriter, r *http.Request, _ string) {
	a.mu.RLock()
	k := a.cfg.DashboardKey
	a.mu.RUnlock()
	http.Redirect(w, r, "/?k="+url.QueryEscape(k), http.StatusSeeOther)
}

func (a *App) setLastError(err error) {
	a.setResult(err, "")
}

func (a *App) setResult(err error, notice string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastNotice = notice
	if err == nil {
		a.lastError = ""
	} else {
		safe := sanitizeSecretError(err, a.cfg.TelegramToken, a.cfg.GoldenKey)
		a.lastError = safe.Error()
		a.lastNotice = ""
		a.logger.Printf("ERROR: %v", safe)
	}
}

func sanitizeSecretError(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	s := err.Error()
	for _, secret := range secrets {
		if strings.TrimSpace(secret) != "" {
			s = strings.ReplaceAll(s, secret, "[secret]")
		}
	}
	return errors.New(s)
}

func (a *App) openDashboard() {
	a.mu.RLock()
	k := a.cfg.DashboardKey
	a.mu.RUnlock()
	u := "http://" + listenAddr + "/?k=" + url.QueryEscape(k)
	if runtime.GOOS == "windows" {
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	} else {
		a.logger.Printf("Dashboard: %s", u)
	}
}

func setWindowsAutoStart(enable bool) error {
	if runtime.GOOS != "windows" {
		return errors.New("автозапуск поддерживается только в Windows")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.Abs(exe)
	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	if enable {
		val := fmt.Sprintf("\"%s\" --background", exe)
		return exec.Command("reg", "add", key, "/v", "ArtBayPublisher", "/t", "REG_SZ", "/d", val, "/f").Run()
	}
	return exec.Command("reg", "delete", key, "/v", "ArtBayPublisher", "/f").Run()
}

func (a *App) telegramLoop() {
	lastConfiguredToken := ""
	for {
		a.mu.RLock()
		token := a.cfg.TelegramToken
		a.mu.RUnlock()
		if token == "" {
			time.Sleep(2 * time.Second)
			continue
		}
		if token != lastConfiguredToken {
			if err := a.configureTelegramUI(token); err != nil {
				a.logger.Printf("Telegram UI setup: %v", err)
			} else {
				lastConfiguredToken = token
			}
		}
		updates, err := a.tgGetUpdates(token, a.tgOffset)
		if err != nil {
			a.logger.Printf("Telegram getUpdates: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}
		for _, u := range updates {
			if u.UpdateID >= a.tgOffset {
				a.tgOffset = u.UpdateID + 1
			}
			if u.Message != nil {
				a.handleTGMessage(u.Message)
			}
			if u.CallbackQuery != nil {
				a.handleTGCallback(u.CallbackQuery)
			}
		}
	}
}

func (a *App) configureTelegramUI(token string) error {
	commands := []map[string]string{
		{"command": "start", "description": "Открыть главное меню"},
		{"command": "lots", "description": "Мои лоты"},
		{"command": "orders", "description": "Последние заказы"},
		{"command": "stats", "description": "Статистика"},
		{"command": "categories", "description": "Категории FunPay"},
		{"command": "rollback", "description": "Откатить последнее изменение"},
		{"command": "status", "description": "Статус ArtBay Publisher"},
		{"command": "version", "description": "Версия запущенного бота"},
		{"command": "lot_format", "description": "Формат ZIP / lot.json"},
		{"command": "stop", "description": "Поставить массовую публикацию на паузу"},
		{"command": "resume", "description": "Продолжить сохранённую публикацию"},
		{"command": "cancel", "description": "Отменить и удалить сохранённую очередь"},
	}
	b, _ := json.Marshal(commands)
	if _, err := tgPost(token, "setMyCommands", url.Values{"commands": {string(b)}}); err != nil {
		return err
	}
	menuButton, _ := json.Marshal(map[string]string{"type": "commands"})
	_, _ = tgPost(token, "setChatMenuButton", url.Values{"menu_button": {string(menuButton)}})
	return nil
}

func (a *App) tgGetUpdates(token string, offset int64) ([]TGUpdate, error) {
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates", token)
	form := url.Values{"timeout": {"25"}, "offset": {strconv.FormatInt(offset, 10)}, "allowed_updates": {`["message","callback_query"]`}}
	req, _ := http.NewRequest("POST", endpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cl := &http.Client{Timeout: 35 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, sanitizeSecretError(err, token)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var out TGResponse[[]TGUpdate]
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, errors.New(out.Description)
	}
	return out.Result, nil
}

func (a *App) handleTGMessage(m *TGMessage) {
	if m.From == nil {
		return
	}
	a.mu.RLock()
	cfg := a.cfg
	a.mu.RUnlock()
	txt := strings.TrimSpace(m.Text)
	if cfg.AdminUserID == 0 {
		pairValue := ""
		if strings.HasPrefix(txt, "/pair ") {
			pairValue = strings.TrimSpace(strings.TrimPrefix(txt, "/pair "))
		} else if strings.HasPrefix(txt, "/start ") {
			pairValue = strings.TrimSpace(strings.TrimPrefix(txt, "/start "))
		}
		if pairValue != "" && pairValue == cfg.PairCode {
			a.mu.Lock()
			a.cfg.AdminUserID = m.From.ID
			a.cfg.AdminChatID = m.Chat.ID
			a.cfg.PairCode = randomDigits(8)
			a.mu.Unlock()
			_ = a.saveConfig()
			_ = a.tgSendMessage(m.Chat.ID, `✅ <b>Готово!</b> Ты привязан как владелец ArtBay Publisher V4.

Нижнее меню теперь всегда под рукой 👇`, persistentMenuKB())
			a.sendMainMenu(m.Chat.ID)
		} else if strings.HasPrefix(txt, "/start") {
			_ = a.tgSendMessage(m.Chat.ID, "🔐 Эта ссылка привязки недействительна. Открой локальную панель ArtBay Publisher и нажми «Открыть бота и привязать».", nil)
		}
		return
	}
	if m.From.ID != cfg.AdminUserID {
		return
	}

	switch {
	case txt == "/start" || txt == "/help":
		_ = a.tgSendMessage(m.Chat.ID, welcomeText(), persistentMenuKB())
		a.sendMainMenu(m.Chat.ID)
	case txt == "🏠 Меню":
		a.sendMainMenu(m.Chat.ID)
	case txt == "📦 Импорт":
		a.sendImportMenu(m.Chat.ID)
	case txt == "🛒 Лоты":
		go a.sendLots(m.Chat.ID)
	case txt == "📊 Статистика":
		go a.sendStats(m.Chat.ID)
	case txt == "💰 Цены":
		a.sendPricesMenu(m.Chat.ID)
	case txt == "🔔 Заказы":
		go a.sendRecentOrders(m.Chat.ID)
	case txt == "⚙️ Настройки":
		a.sendSettings(m.Chat.ID)
	case txt == "/status":
		a.sendStatus(m.Chat.ID)
	case txt == "/stop":
		a.mu.Lock()
		running := a.bulkRunning
		if running {
			a.bulkCancel = true
		}
		a.mu.Unlock()
		if running {
			_ = a.tgSendMessage(m.Chat.ID, "⛔ Останавливаю массовую публикацию после текущего лота...", nil)
		} else {
			_ = a.tgSendMessage(m.Chat.ID, "ℹ️ Сейчас массовая публикация не запущена.", nil)
		}
	case txt == "/cancel":
		switch a.discardBulk() {
		case "stopping":
			_ = a.tgSendMessage(m.Chat.ID, "❌ Отменяю массовую публикацию после текущего лота и удаляю сохранённую очередь...", nil)
		case "discarded":
			_ = a.tgSendMessage(m.Chat.ID, "✅ Массовая публикация отменена. Сохранённая очередь удалена — можно отправлять новый ZIP.", mainMenuKB())
		default:
			_ = a.tgSendMessage(m.Chat.ID, "ℹ️ Сохранённой массовой публикации нет.", nil)
		}
	case txt == "/resume":
		go a.resumeBulk(m.Chat.ID)
	case txt == "/version":
		_ = a.tgSendMessage(m.Chat.ID, fmt.Sprintf("✅ <b>ArtBay Publisher v%s</b>\n\nПанель подключения, безопасные секреты и возобновляемая массовая очередь активны.", appVersion), persistentMenuKB())
	case txt == "/lots":
		go a.sendLots(m.Chat.ID)
	case txt == "/stats":
		go a.sendStats(m.Chat.ID)
	case txt == "/orders":
		go a.sendRecentOrders(m.Chat.ID)
	case txt == "/rollback":
		go a.rollbackLast(m.Chat.ID)
	case txt == "/categories":
		go a.sendCategories(m.Chat.ID, "")
	case strings.HasPrefix(txt, "/categories "):
		go a.sendCategories(m.Chat.ID, strings.TrimSpace(strings.TrimPrefix(txt, "/categories ")))
	case strings.HasPrefix(txt, "/price "):
		go a.commandPrice(m.Chat.ID, txt)
	case strings.HasPrefix(txt, "/prices "):
		go a.commandBulkPrices(m.Chat.ID, txt)
	case strings.HasPrefix(txt, "/on "):
		go a.commandToggle(m.Chat.ID, txt, true)
	case strings.HasPrefix(txt, "/off "):
		go a.commandToggle(m.Chat.ID, txt, false)
	case strings.HasPrefix(txt, "/clone "):
		go a.commandClone(m.Chat.ID, txt)
	case strings.HasPrefix(txt, "/delete "):
		go a.commandDelete(m.Chat.ID, txt)
	case strings.HasPrefix(txt, "/export "):
		go a.commandExport(m.Chat.ID, txt)
	case strings.HasPrefix(txt, "/notify "):
		v := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(txt, "/notify ")))
		a.mu.Lock()
		if v == "on" || v == "1" || v == "да" {
			a.cfg.NotifyOrders = true
		} else if v == "off" || v == "0" || v == "нет" {
			a.cfg.NotifyOrders = false
		}
		on := a.cfg.NotifyOrders
		a.mu.Unlock()
		_ = a.saveConfig()
		_ = a.tgSendMessage(m.Chat.ID, fmt.Sprintf("🔔 Уведомления о новых заказах: <b>%v</b>", on), nil)
	case txt == "/lot_format":
		_ = a.tgSendMessage(m.Chat.ID, lotFormatText(), nil)
	}
	if m.Document != nil {
		go a.handleLotDocument(m)
	}
}

func (a *App) handleLotDocument(m *TGMessage) {
	a.mu.RLock()
	pendingBulk := a.bulkRunning
	if !pendingBulk {
		for _, b := range a.batches {
			if b.Status == "paused" || b.NextIndex > 0 {
				pendingBulk = true
				break
			}
		}
	}
	a.mu.RUnlock()
	if pendingBulk {
		_ = a.tgSendMessage(m.Chat.ID, "⏸ Сначала продолжи сохранённую массовую публикацию командой /resume или окончательно отмени её командой /cancel.", nil)
		return
	}
	name := m.Document.FileName
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".zip" && ext != ".json" {
		_ = a.tgSendMessage(m.Chat.ID, "❌ Поддерживаются ZIP-пакеты и JSON-манифесты. Изображения PNG/JPG/WEBP/GIF положи внутрь ZIP.", nil)
		return
	}
	_ = a.tgSendMessage(m.Chat.ID, "⏳ Получил файл. Проверяю структуру и готовлю очередь...", nil)
	zipPath, err := a.tgDownloadDocument(m.Document.FileID, name)
	if err != nil {
		_ = a.tgSendMessage(m.Chat.ID, "❌ Не смог скачать файл: "+escapeTG(err.Error()), nil)
		return
	}
	var lots []*StagedLot
	var imageAction *PendingAction
	if ext == ".zip" {
		lots, imageAction, err = a.stageIncomingZip(zipPath)
	} else {
		lots, err = a.stageIncomingJSON(zipPath)
	}
	if err != nil {
		_ = os.Remove(zipPath)
		_ = a.tgSendMessage(m.Chat.ID, "❌ Пакет не прошёл проверку:\n<code>"+escapeTG(err.Error())+"</code>", nil)
		return
	}
	if imageAction != nil {
		a.mu.Lock()
		a.actions[imageAction.Token] = imageAction
		a.mu.Unlock()
		_ = a.tgSendMessage(m.Chat.ID,
			fmt.Sprintf("🖼 <b>ПАКЕТ ЗАМЕНЫ КАРТОЧЕК</b>\n\nНайдено файлов: <b>%d</b>\nФормат: имя файла = ID лота.\n\nПосле подтверждения старые изображения будут заменены.", len(imageAction.ImagePaths)),
			actionConfirmKB(imageAction.Token))
		return
	}
	if len(lots) == 0 {
		_ = a.tgSendMessage(m.Chat.ID, "❌ В ZIP не найдено ни одного lot.json.", nil)
		return
	}
	bulkMode := len(lots) > autoPublishBatchThreshold
	if bulkMode {
		// V3.4 BULK: для больших архивов НЕ делаем сетевой PreflightLot по каждому
		// лоту. stageLot уже проверил JSON, цену, категорию/node и файлы локально.
		// Сетевые проверки выполняются только непосредственно во время публикации.
		_ = a.tgSendMessage(m.Chat.ID, fmt.Sprintf("⚡ Найдено <b>%d</b> лотов. Локальная BULK-проверка без запросов FunPay...", len(lots)), nil)
		a.prepareBulkStagedLots(lots)
	} else {
		for _, st := range lots {
			if err := a.prepareStagedLot(st); err != nil {
				st.Warnings = append(st.Warnings, "❌ "+err.Error())
			}
		}
	}
	if len(lots) == 1 {
		st := lots[0]
		a.mu.Lock()
		a.staged[st.Token] = st
		a.mu.Unlock()
		a.sendSinglePreview(m.Chat.ID, st)
		return
	}
	batch := &BatchStage{Token: randomToken(8), Lots: lots, Selected: map[int]bool{}, BlockedNodes: map[int]bool{}, CreatedAt: time.Now()}
	valid := 0
	skippedDuplicates := 0
	autoBulk := shouldAutoPublishBatch(len(lots))
	for i, st := range lots {
		on := !stageHasFatalWarning(st)
		// В автопачке точный дубликат не публикуем повторно. Это особенно важно
		// после частично успешной предыдущей попытки.
		if autoBulk && st.Duplicate {
			on = false
			skippedDuplicates++
		}
		batch.Selected[i] = on
		if on {
			valid++
		}
	}
	a.mu.Lock()
	a.batches[batch.Token] = batch
	a.mu.Unlock()
	if shouldAutoPublishBatch(len(lots)) {
		if valid == 0 {
			a.removeBatch(batch.Token)
			_ = a.tgSendMessage(m.Chat.ID, "❌ В большом пакете нет лотов, прошедших проверку.", nil)
			return
		}
		_ = a.tgSendMessage(m.Chat.ID, fmt.Sprintf(`⚡ <b>FAST BULK MODE</b>

Лотов в пакете: <b>%d</b>
Прошли проверку: <b>%d</b>
Пропущено локальных дубликатов: <b>%d</b>

Сетевой preflight для больших пачек отключён — FunPay проверяется по ходу публикации.
Пакеты больше %d лотов публикуются автоматически без подтверждения.`, len(lots), valid, skippedDuplicates, autoPublishBatchThreshold), nil)
		go a.publishBatch(m.Chat.ID, batch)
		return
	}
	a.sendBatchPreview(m.Chat.ID, 0, batch, false)
}

func (a *App) handleTGCallback(c *TGCallback) {
	a.mu.RLock()
	cfg := a.cfg
	a.mu.RUnlock()
	if c.From.ID != cfg.AdminUserID {
		return
	}
	_ = a.tgAnswerCallback(c.ID, "")
	chatID := cfg.AdminChatID
	var msgID int64
	if c.Message != nil {
		chatID = c.Message.Chat.ID
		msgID = c.Message.MessageID
	}

	switch c.Data {
	case "menu:home":
		a.sendMainMenu(chatID)
		return
	case "menu:import":
		a.sendImportMenu(chatID)
		return
	case "menu:lots":
		go a.sendLots(chatID)
		return
	case "menu:prices":
		a.sendPricesMenu(chatID)
		return
	case "menu:stats":
		go a.sendStats(chatID)
		return
	case "menu:orders":
		go a.sendRecentOrders(chatID)
		return
	case "menu:tools":
		a.sendToolsMenu(chatID)
		return
	case "menu:settings":
		a.sendSettings(chatID)
		return
	case "menu:categories":
		go a.sendCategories(chatID, "")
		return
	case "menu:format":
		_ = a.tgSendMessage(chatID, lotFormatText(), backHomeKB())
		return
	case "menu:rollback":
		go a.askRollback(chatID)
		return
	case "menu:status":
		a.sendStatus(chatID)
		return
	case "setting:notify:toggle":
		a.mu.Lock()
		a.cfg.NotifyOrders = !a.cfg.NotifyOrders
		on := a.cfg.NotifyOrders
		cfg2 := a.cfg
		a.mu.Unlock()
		_ = a.saveConfig()
		_ = a.tgSendMessage(chatID, fmt.Sprintf("🔔 Уведомления о заказах: <b>%s</b>", onOff(on)), settingsKB(cfg2))
		return
	case "setting:autostart:toggle":
		a.mu.RLock()
		cur := a.cfg.AutoStart
		a.mu.RUnlock()
		if err := setWindowsAutoStart(!cur); err != nil {
			_ = a.tgSendMessage(chatID, "❌ Не удалось изменить автозапуск: <code>"+escapeTG(err.Error())+"</code>", backHomeKB())
			return
		}
		a.mu.Lock()
		a.cfg.AutoStart = !cur
		cfg2 := a.cfg
		a.mu.Unlock()
		_ = a.saveConfig()
		_ = a.tgSendMessage(chatID, fmt.Sprintf("🖥 Автозапуск Windows: <b>%s</b>", onOff(cfg2.AutoStart)), settingsKB(cfg2))
		return
	case "rollback:yes":
		go a.rollbackLast(chatID)
		return
	}

	parts := strings.Split(c.Data, ":")
	if len(parts) == 0 {
		return
	}
	switch parts[0] {
	case "pub", "cancel":
		if len(parts) != 2 {
			return
		}
		token := parts[1]
		a.mu.RLock()
		st := a.staged[token]
		a.mu.RUnlock()
		if st == nil {
			_ = a.tgSendMessage(chatID, "⚠️ Это превью уже устарело.", nil)
			return
		}
		if parts[0] == "cancel" {
			a.removeStage(token)
			_ = a.tgSendMessage(chatID, "❌ Публикация отменена.", nil)
			return
		}
		a.mu.Lock()
		if st.Publishing {
			a.mu.Unlock()
			_ = a.tgSendMessage(chatID, "⏳ Этот лот уже публикуется.", nil)
			return
		}
		st.Publishing = true
		a.mu.Unlock()
		go a.publishSingle(chatID, st)
	case "bt":
		if len(parts) != 3 {
			return
		}
		idx, _ := strconv.Atoi(parts[2])
		a.mu.Lock()
		batch := a.batches[parts[1]]
		if batch != nil && idx >= 0 && idx < len(batch.Lots) {
			batch.Selected[idx] = !batch.Selected[idx]
		}
		a.mu.Unlock()
		if batch != nil {
			a.sendBatchPreview(chatID, msgID, batch, msgID != 0)
		}
	case "bpub", "bcancel":
		if len(parts) != 2 {
			return
		}
		a.mu.RLock()
		batch := a.batches[parts[1]]
		a.mu.RUnlock()
		if batch == nil && parts[0] == "bcancel" {
			a.mu.RLock()
			active := a.bulkRunning && a.bulkActiveToken == parts[1]
			a.mu.RUnlock()
			if active {
				if a.discardBulk() == "stopping" {
					_ = a.tgSendMessage(chatID, "❌ Отменяю массовую публикацию после текущего лота и удаляю сохранённую очередь...", nil)
				}
				return
			}
		}
		if batch == nil {
			_ = a.tgSendMessage(chatID, "⚠️ Пакет уже устарел.", nil)
			return
		}
		if parts[0] == "bcancel" {
			a.removeBatch(parts[1])
			_ = a.tgSendMessage(chatID, "❌ Массовая публикация отменена.", nil)
			return
		}
		go a.publishBatch(chatID, batch)
	case "lot":
		if len(parts) != 2 {
			return
		}
		id, _ := strconv.Atoi(parts[1])
		go a.sendLotDetails(chatID, id)
	case "toggle":
		if len(parts) != 3 {
			return
		}
		id, _ := strconv.Atoi(parts[1])
		on := parts[2] == "1"
		go a.toggleLot(chatID, id, on)
	case "clone":
		if len(parts) != 2 {
			return
		}
		id, _ := strconv.Atoi(parts[1])
		go a.cloneLot(chatID, id)
	case "export":
		if len(parts) != 2 {
			return
		}
		id, _ := strconv.Atoi(parts[1])
		go a.exportLot(chatID, id)
	case "delask":
		if len(parts) != 2 {
			return
		}
		id, _ := strconv.Atoi(parts[1])
		kb := map[string]any{"inline_keyboard": [][]map[string]string{{{"text": "🗑 Да, удалить", "callback_data": fmt.Sprintf("del:%d", id)}, {"text": "Отмена", "callback_data": fmt.Sprintf("lot:%d", id)}}}}
		_ = a.tgSendMessage(chatID, fmt.Sprintf("⚠️ Удалить лот <code>%d</code>? Перед удалением будет создан backup.", id), kb)
	case "del":
		if len(parts) != 2 {
			return
		}
		id, _ := strconv.Atoi(parts[1])
		go a.deleteLot(chatID, id)
	case "act", "actcancel":
		if len(parts) != 2 {
			return
		}
		token := parts[1]
		a.mu.Lock()
		act := a.actions[token]
		if act != nil && parts[0] == "act" {
			if act.Executing {
				a.mu.Unlock()
				_ = a.tgSendMessage(chatID, "⏳ Это действие уже выполняется.", nil)
				return
			}
			act.Executing = true
		}
		a.mu.Unlock()
		if act == nil {
			_ = a.tgSendMessage(chatID, "⚠️ Действие уже устарело.", nil)
			return
		}
		if parts[0] == "actcancel" {
			a.removeAction(token)
			_ = a.tgSendMessage(chatID, "❌ Действие отменено.", nil)
			return
		}
		go a.executeAction(chatID, act)
	}
}

func (a *App) removeStage(token string) {
	a.mu.Lock()
	st := a.staged[token]
	delete(a.staged, token)
	a.mu.Unlock()
	if st != nil {
		_ = os.RemoveAll(st.TempDir)
		_ = os.Remove(st.ZipPath)
	}
}

func (a *App) cleanupLoop() {
	for {
		time.Sleep(30 * time.Minute)
		cutoff := time.Now().Add(-2 * time.Hour)
		var oldStages, oldBatches, oldActions []string
		a.mu.RLock()
		for k, v := range a.staged {
			if v.CreatedAt.Before(cutoff) {
				oldStages = append(oldStages, k)
			}
		}
		for k, v := range a.batches {
			if v.Status != "paused" && v.CreatedAt.Before(cutoff) {
				oldBatches = append(oldBatches, k)
			}
		}
		for k, v := range a.actions {
			if v.CreatedAt.Before(cutoff) {
				oldActions = append(oldActions, k)
			}
		}
		a.mu.RUnlock()
		for _, k := range oldStages {
			a.removeStage(k)
		}
		for _, k := range oldBatches {
			a.removeBatch(k)
		}
		for _, k := range oldActions {
			a.removeAction(k)
		}
	}
}

func (a *App) stageLot(zipPath string) (*StagedLot, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	tempDir, err := os.MkdirTemp(a.dataDir, "lot-")
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(tempDir)
		}
	}()
	for _, f := range zr.File {
		n := filepath.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		if strings.HasPrefix(n, "../") || filepath.IsAbs(n) {
			return nil, errors.New("опасный путь внутри ZIP")
		}
		dest := filepath.Join(tempDir, n)
		if !strings.HasPrefix(filepath.Clean(dest), filepath.Clean(tempDir)+string(os.PathSeparator)) && filepath.Clean(dest) != filepath.Clean(tempDir) {
			return nil, errors.New("опасный путь внутри ZIP")
		}
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(dest, 0700)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return nil, err
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		out, err := os.Create(dest)
		if err != nil {
			rc.Close()
			return nil, err
		}
		_, cpErr := io.Copy(out, io.LimitReader(rc, 25<<20))
		out.Close()
		rc.Close()
		if cpErr != nil {
			return nil, cpErr
		}
	}
	lotJSON := filepath.Join(tempDir, "lot.json")
	b, err := os.ReadFile(lotJSON)
	if err != nil {
		return nil, errors.New("в корне ZIP нет lot.json")
	}
	var p LotPackage
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("lot.json: %w", err)
	}
	st, err := stagePackageFromDir(p, tempDir, zipPath)
	if err != nil {
		return nil, err
	}
	ok = true
	return st, nil
}

func stagePackageFromDir(p LotPackage, tempDir, sourcePath string) (*StagedLot, error) {
	if p.Version == 0 {
		p.Version = 1
	}
	if p.LotID <= 0 && p.NodeID <= 0 && strings.TrimSpace(p.CategoryPath) == "" && strings.TrimSpace(p.CategoryURL) == "" {
		return nil, errors.New("укажи node_id, category_path или category_url")
	}
	if p.LotID <= 0 && strings.TrimSpace(p.TitleRU) == "" && strings.TrimSpace(p.TitleEN) == "" {
		return nil, errors.New("для нового лота нужно хотя бы одно название")
	}
	if p.LotID <= 0 && p.Price <= 0 {
		return nil, errors.New("для нового лота price должен быть > 0")
	}
	var imgs []string
	if len(p.ImageFiles) > 0 {
		for _, n := range p.ImageFiles {
			clean := filepath.Clean(strings.ReplaceAll(n, "\\", "/"))
			if clean == "." || strings.HasPrefix(clean, "../") || filepath.IsAbs(clean) {
				return nil, fmt.Errorf("опасный путь картинки: %s", n)
			}
			q := filepath.Join(tempDir, clean)
			if !strings.HasPrefix(filepath.Clean(q), filepath.Clean(tempDir)+string(os.PathSeparator)) {
				return nil, fmt.Errorf("опасный путь картинки: %s", n)
			}
			if _, err := os.Stat(q); err != nil {
				return nil, fmt.Errorf("картинка %s не найдена", n)
			}
			imgs = append(imgs, q)
		}
	} else {
		candidates := []string{"cover.png", "cover.jpg", "cover.jpeg", "cover.webp", "cover.gif", "image.png", "image.jpg", "image.jpeg", "image.webp", "image.gif"}
		for _, n := range candidates {
			q := filepath.Join(tempDir, n)
			if _, err := os.Stat(q); err == nil {
				imgs = append(imgs, q)
				break
			}
		}
	}
	if len(imgs) == 0 && p.LotID <= 0 {
		return nil, errors.New("не найдена картинка (cover.png / cover.jpg / cover.webp или image_files в lot.json)")
	}
	return &StagedLot{Token: randomToken(8), TempDir: tempDir, ZipPath: sourcePath, Package: p, ImagePaths: imgs, CreatedAt: time.Now()}, nil
}

func persistentMenuKB() map[string]any {
	return map[string]any{
		"keyboard": [][]map[string]string{
			{{"text": "🏠 Меню"}, {"text": "📦 Импорт"}},
			{{"text": "🛒 Лоты"}, {"text": "🔔 Заказы"}},
			{{"text": "📊 Статистика"}, {"text": "💰 Цены"}},
			{{"text": "⚙️ Настройки"}},
		},
		"resize_keyboard":         true,
		"is_persistent":           true,
		"input_field_placeholder": "Выбери действие или отправь ZIP",
	}
}

func mainMenuKB() map[string]any {
	return map[string]any{"inline_keyboard": [][]map[string]string{
		{{"text": "📦 Импорт", "callback_data": "menu:import"}, {"text": "🛒 Мои лоты", "callback_data": "menu:lots"}},
		{{"text": "🔔 Заказы", "callback_data": "menu:orders"}, {"text": "📊 Статистика", "callback_data": "menu:stats"}},
		{{"text": "💰 Цены", "callback_data": "menu:prices"}, {"text": "🔎 Категории", "callback_data": "menu:categories"}},
		{{"text": "🧰 Инструменты", "callback_data": "menu:tools"}, {"text": "⚙️ Настройки", "callback_data": "menu:settings"}},
	}}
}

func backHomeKB() map[string]any {
	return map[string]any{"inline_keyboard": [][]map[string]string{{{"text": "⬅️ Главное меню", "callback_data": "menu:home"}}}}
}

func toolsKB() map[string]any {
	return map[string]any{"inline_keyboard": [][]map[string]string{
		{{"text": "↩️ Откат последнего", "callback_data": "menu:rollback"}},
		{{"text": "📄 Формат lot.json", "callback_data": "menu:format"}, {"text": "🔎 Категории", "callback_data": "menu:categories"}},
		{{"text": "⚙️ Статус", "callback_data": "menu:status"}},
		{{"text": "⬅️ Главное меню", "callback_data": "menu:home"}},
	}}
}

func settingsKB(cfg Config) map[string]any {
	return map[string]any{"inline_keyboard": [][]map[string]string{
		{{"text": "🔔 Заказы: " + onOff(cfg.NotifyOrders), "callback_data": "setting:notify:toggle"}},
		{{"text": "🖥 Автозапуск: " + onOff(cfg.AutoStart), "callback_data": "setting:autostart:toggle"}},
		{{"text": "⚙️ Полный статус", "callback_data": "menu:status"}},
		{{"text": "⬅️ Главное меню", "callback_data": "menu:home"}},
	}}
}

func onOff(v bool) string {
	if v {
		return "ВКЛ ✅"
	}
	return "ВЫКЛ ❌"
}

func welcomeText() string {
	return `🛍 <b>ArtBay Publisher V4</b>

Готов к работе. Теперь у бота есть постоянное нижнее меню — не нужно помнить команды.

📦 Отправляй ZIP или JSON с лотом либо пачкой. Большая очередь сохраняет прогресс и умеет продолжаться после паузы.
🛒 Лотами, ценами, заказами и настройками управляй кнопками ниже.`
}

func (a *App) sendMainMenu(chatID int64) {
	a.mu.RLock()
	cfg := a.cfg
	a.mu.RUnlock()
	fpState := "не настроен ❌"
	if cfg.GoldenKey != "" {
		fpState = "готов ✅"
	}
	text := fmt.Sprintf("🏠 <b>ARTBAY CONTROL CENTER</b>\n\nFunPay: <b>%s</b>\nЗаказы: <b>%s</b>\nВерсия: <code>%s</code>\n\nВыбирай раздел 👇", fpState, onOff(cfg.NotifyOrders), appVersion)
	_ = a.tgSendMessage(chatID, text, mainMenuKB())
}

func (a *App) sendImportMenu(chatID int64) {
	text := `📦 <b>ИМПОРТ ЛОТОВ</b>

Отправь боту ZIP:
• один <code>lot.json</code> + карточка;
• общий ZIP с несколькими лотами;
• ZIP с картинками вида <code>ID.png</code> для массовой замены карточек.

Перед публикацией бот всё проверит и покажет предпросмотр.`
	kb := map[string]any{"inline_keyboard": [][]map[string]string{
		{{"text": "📄 Формат lot.json", "callback_data": "menu:format"}, {"text": "🔎 Категории", "callback_data": "menu:categories"}},
		{{"text": "⬅️ Главное меню", "callback_data": "menu:home"}},
	}}
	_ = a.tgSendMessage(chatID, text, kb)
}

func (a *App) sendPricesMenu(chatID int64) {
	text := `💰 <b>УПРАВЛЕНИЕ ЦЕНАМИ</b>

Для одного лота:
<code>/price ID 199</code>

Для всех известных лотов:
<code>/prices -15%</code>

Только для раздела:
<code>/prices NODE_ID -15%</code>

Массовое изменение сначала покажет предпросмотр и потребует подтверждение.`
	_ = a.tgSendMessage(chatID, text, backHomeKB())
}

func (a *App) sendToolsMenu(chatID int64) {
	_ = a.tgSendMessage(chatID, "🧰 <b>ИНСТРУМЕНТЫ</b>\n\nBackup создаётся перед опасными изменениями. Последнее изменение можно откатить.", toolsKB())
}

func (a *App) sendSettings(chatID int64) {
	a.mu.RLock()
	cfg := a.cfg
	a.mu.RUnlock()
	text := fmt.Sprintf("⚙️ <b>НАСТРОЙКИ</b>\n\n🔔 Уведомления о заказах: <b>%s</b>\n🖥 Автозапуск Windows: <b>%s</b>\n🔐 Telegram привязан: <b>%s</b>\n\nНажми кнопку, чтобы переключить настройку.", onOff(cfg.NotifyOrders), onOff(cfg.AutoStart), onOff(cfg.AdminUserID != 0))
	_ = a.tgSendMessage(chatID, text, settingsKB(cfg))
}

func actionConfirmKB(token string) map[string]any {
	return map[string]any{"inline_keyboard": [][]map[string]string{{
		{"text": "✅ ПРИМЕНИТЬ", "callback_data": "act:" + token},
		{"text": "❌ Отмена", "callback_data": "actcancel:" + token},
	}}}
}

func helpText() string {
	return `🛍 <b>ArtBay Publisher V4</b>

Главное управление теперь через кнопки.

<b>Полезные команды:</b>
<code>/lots</code> — мои лоты
<code>/orders</code> — последние заказы
<code>/stats</code> — статистика
<code>/price ID 199</code> — изменить цену
<code>/prices -15%</code> — массово изменить цены
<code>/on ID</code> / <code>/off ID</code>
<code>/clone ID</code> — клонировать
<code>/export ID</code> — выгрузить ZIP для правки
<code>/delete ID</code> — удалить с backup
<code>/rollback</code> — откат
<code>/categories Roblox</code> — найти раздел
<code>/stop</code> — поставить очередь на паузу
<code>/resume</code> — продолжить очередь
<code>/cancel</code> — отменить и удалить очередь
<code>/lot_format</code> — формат пакета`
}

func lotFormatText() string {
	return `📄 <b>lot.json V2</b>

Для нового лота можно больше не знать node_id:

<code>{
  "version": 2,
  "category_path": "Roblox Studio > Услуги",
  "title_ru": "...",
  "title_en": "...",
  "description_ru": "...",
  "description_en": "...",
  "payment_msg_ru": "...",
  "payment_msg_en": "...",
  "price": 199,
  "active": true,
  "image_files": ["cover.png"]
}</code>

Вместо category_path также можно использовать:
<code>"category_url": "https://funpay.com/lots/1858/"</code>
или старый <code>node_id</code>.

Для обновления существующего лота добавь:
<code>"lot_id": 12345678</code>

Большой ZIP может содержать несколько папок с lot.json, несколько готовых ZIP-пакетов или общий <code>lots.json</code> / <code>artbay-batch.json</code> с массивом лотов.

Поддерживаемые изображения: PNG, JPG, WEBP и GIF. JSON без ZIP подходит для обновлений без новых изображений.`
}

func (a *App) sendStatus(chatID int64) {
	a.mu.RLock()
	cfg := a.cfg
	a.mu.RUnlock()
	fp := "❌"
	if cfg.GoldenKey != "" {
		fp = "✅"
	}
	_ = a.tgSendMessage(chatID, fmt.Sprintf(
		"⚙️ <b>ArtBay Publisher %s</b>\n\nTelegram: ✅\nFunPay key: %s\nИзвестных разделов: <b>%d</b>\nУведомления о заказах: <b>%s</b>",
		appVersion, fp, len(cfg.KnownNodes), onOff(cfg.NotifyOrders)), backHomeKB())
}

func (a *App) stageIncomingZip(zipPath string) ([]*StagedLot, *PendingAction, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, nil, err
	}
	defer zr.Close()
	outer, err := os.MkdirTemp(a.dataDir, "incoming-")
	if err != nil {
		return nil, nil, err
	}
	cleanupOuter := true
	defer func() {
		if cleanupOuter {
			_ = os.RemoveAll(outer)
			_ = os.Remove(zipPath)
		}
	}()
	if err := extractZipReader(zr, outer, 600<<20); err != nil {
		return nil, nil, err
	}

	var lotJSONs []string
	var manifestJSONs []string
	var nestedZips []string
	imageMap := map[int]string{}
	err = filepath.Walk(outer, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return err
		}
		name := strings.ToLower(info.Name())
		switch {
		case name == "lot.json":
			lotJSONs = append(lotJSONs, path)
		case name == "lots.json" || name == "artbay-batch.json":
			manifestJSONs = append(manifestJSONs, path)
		case strings.HasSuffix(name, ".zip"):
			nestedZips = append(nestedZips, path)
		default:
			ext := strings.ToLower(filepath.Ext(name))
			if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" || ext == ".gif" {
				base := strings.TrimSuffix(filepath.Base(name), ext)
				base = strings.TrimPrefix(strings.ToLower(base), "lot_")
				if id, e := strconv.Atoi(base); e == nil && id > 0 {
					imageMap[id] = path
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	var lots []*StagedLot
	usesOuter := false
	for _, manifestPath := range manifestJSONs {
		packages, err := readLotManifest(manifestPath)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", filepath.Base(manifestPath), err)
		}
		baseDir := filepath.Dir(manifestPath)
		for i, p := range packages {
			st, err := stagePackageFromDir(p, baseDir, zipPath)
			if err != nil {
				return nil, nil, fmt.Errorf("%s, лот %d: %w", filepath.Base(manifestPath), i+1, err)
			}
			st.TempDir = outer
			lots = append(lots, st)
			usesOuter = true
		}
	}
	seenDirs := map[string]bool{}
	for _, j := range lotJSONs {
		dir := filepath.Dir(j)
		if seenDirs[dir] {
			continue
		}
		seenDirs[dir] = true
		tmpZip := filepath.Join(a.dataDir, "dirlot-"+randomToken(4)+".zip")
		if err := zipDirectory(dir, tmpZip); err != nil {
			return nil, nil, err
		}
		st, err := a.stageLot(tmpZip)
		if err != nil {
			_ = os.Remove(tmpZip)
			return nil, nil, fmt.Errorf("%s: %w", filepath.Base(dir), err)
		}
		lots = append(lots, st)
	}
	// Вложенные ZIP распаковываем параллельно: это локальный I/O и безопасно
	// ускоряет большие пакеты на десятки/сотни лотов.
	if len(nestedZips) > 0 {
		results := make([]*StagedLot, len(nestedZips))
		sem := make(chan struct{}, 8)
		var wg sync.WaitGroup
		for i, nz := range nestedZips {
			i, nz := i, nz
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				st, err := a.stageLot(nz)
				if err == nil {
					results[i] = st
				}
			}()
		}
		wg.Wait()
		for _, st := range results {
			if st != nil {
				lots = append(lots, st)
			}
		}
	}
	if len(lots) > 0 {
		if usesOuter {
			cleanupOuter = false
		}
		return lots, nil, nil
	}
	if len(imageMap) > 0 {
		act := &PendingAction{
			Token: randomToken(8), Kind: "images", ImagePaths: imageMap, TempDir: outer,
			Description: fmt.Sprintf("Замена карточек у %d лотов", len(imageMap)), CreatedAt: time.Now(),
		}
		cleanupOuter = false
		_ = os.Remove(zipPath)
		return nil, act, nil
	}
	return nil, nil, errors.New("не найдено lot.json, вложенных пакетов лотов или картинок вида LOT_ID.png")
}

func readLotManifest(path string) ([]LotPackage, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lots []LotPackage
	if err := json.Unmarshal(b, &lots); err == nil {
		if len(lots) == 0 {
			return nil, errors.New("манифест не содержит лотов")
		}
		return lots, nil
	}
	var single LotPackage
	if err := json.Unmarshal(b, &single); err == nil && (single.LotID > 0 || single.NodeID > 0 || single.CategoryPath != "" || single.CategoryURL != "") {
		return []LotPackage{single}, nil
	}
	var wrapper struct {
		Version int          `json:"version"`
		Lots    []LotPackage `json:"lots"`
	}
	if err := json.Unmarshal(b, &wrapper); err != nil {
		return nil, fmt.Errorf("неверный JSON: %w", err)
	}
	if len(wrapper.Lots) == 0 {
		return nil, errors.New("поле lots пустое")
	}
	return wrapper.Lots, nil
}

func (a *App) stageIncomingJSON(path string) ([]*StagedLot, error) {
	tempDir, err := os.MkdirTemp(a.dataDir, "json-import-")
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(tempDir)
			_ = os.Remove(path)
		}
	}()
	dest := filepath.Join(tempDir, filepath.Base(path))
	if err := os.Rename(path, dest); err != nil {
		return nil, err
	}
	packages, err := readLotManifest(dest)
	if err != nil {
		return nil, err
	}
	lots := make([]*StagedLot, 0, len(packages))
	for i, p := range packages {
		st, err := stagePackageFromDir(p, tempDir, "")
		if err != nil {
			return nil, fmt.Errorf("лот %d: %w", i+1, err)
		}
		st.TempDir = tempDir
		lots = append(lots, st)
	}
	ok = true
	return lots, nil
}

func extractZipReader(zr *zip.ReadCloser, destRoot string, limit int64) error {
	var total int64
	for _, f := range zr.File {
		n := filepath.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		if n == "." || strings.HasPrefix(n, "../") || filepath.IsAbs(n) {
			return errors.New("опасный путь внутри ZIP")
		}
		dest := filepath.Join(destRoot, n)
		if !strings.HasPrefix(filepath.Clean(dest), filepath.Clean(destRoot)+string(os.PathSeparator)) && filepath.Clean(dest) != filepath.Clean(destRoot) {
			return errors.New("опасный путь внутри ZIP")
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0700); err != nil {
				return err
			}
			continue
		}
		if f.UncompressedSize64 > 30<<20 {
			return fmt.Errorf("слишком большой файл внутри ZIP: %s", f.Name)
		}
		total += int64(f.UncompressedSize64)
		if total > limit {
			return errors.New("ZIP слишком большой после распаковки")
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(dest)
		if err != nil {
			rc.Close()
			return err
		}
		_, cpErr := io.Copy(out, io.LimitReader(rc, 31<<20))
		out.Close()
		rc.Close()
		if cpErr != nil {
			return cpErr
		}
	}
	return nil
}

func zipDirectory(srcDir, dst string) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, io.LimitReader(f, 30<<20))
		f.Close()
		return err
	})
	cerr := zw.Close()
	oerr := out.Close()
	if err != nil {
		return err
	}
	if cerr != nil {
		return cerr
	}
	return oerr
}

func (a *App) prepareStagedLot(st *StagedLot) error {
	if st.Package.LotID > 0 {
		_, err := a.fp.GetLotFields(st.Package.LotID)
		if err != nil {
			return fmt.Errorf("лот %d не найден или недоступен: %w", st.Package.LotID, err)
		}
		st.Warnings = append(st.Warnings, "✏️ Режим обновления существующего лота")
		return nil
	}
	node, cat, err := a.fp.ResolveNode(st.Package)
	if err != nil {
		return err
	}
	st.Package.NodeID = node
	a.addKnownNode(node)
	if cat != "" {
		st.Warnings = append(st.Warnings, "📂 "+cat)
	}
	warnings, duplicate, err := a.fp.PreflightLot(st.Package)
	if err != nil {
		return err
	}
	st.Warnings = append(st.Warnings, warnings...)
	st.Duplicate = duplicate
	if duplicate {
		st.Warnings = append(st.Warnings, "⚠️ На аккаунте уже есть лот с очень похожим названием")
	}
	return nil
}

func (a *App) prepareBulkStagedLots(lots []*StagedLot) {
	// Только локальная подготовка. Важная цель V3.4 — не делать 100–500
	// последовательных сетевых preflight-проверок до начала публикации.
	resolved := map[string]struct {
		node int
		cat  string
		err  error
	}{}
	nodes := map[int]bool{}
	seenTitles := map[string]bool{}

	for _, st := range lots {
		if st == nil {
			continue
		}
		if st.Package.LotID > 0 {
			st.Warnings = append(st.Warnings, "✏️ Режим обновления существующего лота")
			continue
		}

		if st.Package.NodeID <= 0 {
			key := strings.TrimSpace(st.Package.CategoryURL) + "|" + strings.TrimSpace(st.Package.CategoryPath)
			r, ok := resolved[key]
			if !ok {
				node, cat, err := a.fp.ResolveNode(st.Package)
				r = struct {
					node int
					cat  string
					err  error
				}{node: node, cat: cat, err: err}
				resolved[key] = r
			}
			if r.err != nil {
				st.Warnings = append(st.Warnings, "❌ "+r.err.Error())
				continue
			}
			st.Package.NodeID = r.node
			if r.cat != "" {
				st.Warnings = append(st.Warnings, "📂 "+r.cat)
			}
		}
		if st.Package.NodeID <= 0 {
			st.Warnings = append(st.Warnings, "❌ node_id не определён")
			continue
		}
		nodes[st.Package.NodeID] = true

		// Мгновенная защита от двух одинаковых заголовков внутри одного ZIP.
		title := normalizeText(st.Package.TitleRU)
		if title == "" {
			title = normalizeText(st.Package.TitleEN)
		}
		dupKey := fmt.Sprintf("%d|%s", st.Package.NodeID, title)
		if title != "" && seenTitles[dupKey] {
			st.Duplicate = true
			st.Warnings = append(st.Warnings, "⚠️ Дубликат названия внутри этого пакета")
		} else if title != "" {
			seenTitles[dupKey] = true
		}
	}
	a.addKnownNodesBulk(nodes)
}

func (a *App) addKnownNodesBulk(nodes map[int]bool) {
	if len(nodes) == 0 {
		return
	}
	changed := false
	a.mu.Lock()
	existing := make(map[int]bool, len(a.cfg.KnownNodes)+len(nodes))
	for _, n := range a.cfg.KnownNodes {
		existing[n] = true
	}
	for n := range nodes {
		if n > 0 && !existing[n] {
			a.cfg.KnownNodes = append(a.cfg.KnownNodes, n)
			existing[n] = true
			changed = true
		}
	}
	if changed {
		sort.Ints(a.cfg.KnownNodes)
	}
	a.mu.Unlock()
	if changed {
		_ = a.saveConfig()
	}
}

func (a *App) addKnownNode(node int) {
	if node <= 0 {
		return
	}
	a.mu.Lock()
	for _, n := range a.cfg.KnownNodes {
		if n == node {
			a.mu.Unlock()
			return
		}
	}
	a.cfg.KnownNodes = append(a.cfg.KnownNodes, node)
	sort.Ints(a.cfg.KnownNodes)
	a.mu.Unlock()
	_ = a.saveConfig()
}

func (a *App) sendSinglePreview(chatID int64, st *StagedLot) {
	p := st.Package
	active := true
	if p.Active != nil {
		active = *p.Active
	}
	mode := "НОВЫЙ ЛОТ"
	if p.LotID > 0 {
		mode = fmt.Sprintf("ОБНОВЛЕНИЕ ЛОТА #%d", p.LotID)
	}
	var ws string
	if len(st.Warnings) > 0 {
		ws = "\n\n" + escapeTG(strings.Join(st.Warnings, "\n"))
	}
	preview := fmt.Sprintf(
		"🛒 <b>%s</b>\n\n<b>Node:</b> <code>%d</code>\n<b>Цена:</b> %.2f\n<b>Активен:</b> %v\n<b>Картинок:</b> %d\n\n🇷🇺 <b>%s</b>\n\n🇬🇧 <b>%s</b>%s",
		mode, p.NodeID, p.Price, active, len(st.ImagePaths), escapeTG(cut(p.TitleRU, 180)), escapeTG(cut(p.TitleEN, 180)), ws)
	pubText := "🚀 ОПУБЛИКОВАТЬ"
	if p.LotID > 0 {
		pubText = "💾 ОБНОВИТЬ"
	} else if st.Duplicate {
		pubText = "⚠️ ВСЁ РАВНО ОПУБЛИКОВАТЬ"
	}
	kb := map[string]any{"inline_keyboard": [][]map[string]string{{{"text": pubText, "callback_data": "pub:" + st.Token}, {"text": "❌ Отмена", "callback_data": "cancel:" + st.Token}}}}
	_ = a.tgSendMessage(chatID, preview, kb)
}

func shouldAutoPublishBatch(count int) bool {
	return count > autoPublishBatchThreshold
}

func stageHasFatalWarning(st *StagedLot) bool {
	for _, w := range st.Warnings {
		if strings.HasPrefix(w, "❌") {
			return true
		}
	}
	return false
}

func (a *App) sendBatchPreview(chatID int64, messageID int64, batch *BatchStage, edit bool) {
	selected := 0
	var lines []string
	var kbRows [][]map[string]string
	for i, st := range batch.Lots {
		on := batch.Selected[i]
		if on {
			selected++
		}
		icon := "✅"
		if !on {
			icon = "⬜"
		}
		title := st.Package.TitleRU
		if strings.TrimSpace(title) == "" {
			title = st.Package.TitleEN
		}
		status := ""
		if st.Duplicate {
			status = " ⚠️"
		}
		if len(st.Warnings) > 0 {
			for _, w := range st.Warnings {
				if strings.HasPrefix(w, "❌") {
					status = " ❌"
					break
				}
			}
		}
		lines = append(lines, fmt.Sprintf("%s <b>%d.</b> %.2f ₽ — %s%s", icon, i+1, st.Package.Price, escapeTG(cut(title, 55)), status))
		kbRows = append(kbRows, []map[string]string{{"text": fmt.Sprintf("%s %d. %s", icon, i+1, cut(title, 32)), "callback_data": fmt.Sprintf("bt:%s:%d", batch.Token, i)}})
	}
	kbRows = append(kbRows, []map[string]string{
		{"text": fmt.Sprintf("🚀 Опубликовать выбранные (%d)", selected), "callback_data": "bpub:" + batch.Token},
		{"text": "❌ Отмена", "callback_data": "bcancel:" + batch.Token},
	})
	text := fmt.Sprintf("📦 <b>ПАКЕТ ЛОТОВ</b>\n\nНайдено: <b>%d</b>\nВыбрано: <b>%d</b>\n\n%s\n\nНажимай на строки, чтобы включать/исключать лоты.", len(batch.Lots), selected, strings.Join(lines, "\n"))
	kb := map[string]any{"inline_keyboard": kbRows}
	if edit && messageID != 0 {
		if err := a.tgEditMessage(chatID, messageID, text, kb); err == nil {
			return
		}
	}
	_ = a.tgSendMessage(chatID, text, kb)
}

func (a *App) publishSingle(chatID int64, st *StagedLot) {
	_ = a.tgSendMessage(chatID, "⏳ Отправляю данные на FunPay...", nil)
	if st.Package.LotID > 0 {
		if err := a.backupLot(st.Package.LotID, "update_from_package"); err != nil {
			a.logger.Printf("backup update %d: %v", st.Package.LotID, err)
		}
	}
	resp, err := a.fp.ApplyPackage(st.Package, st.ImagePaths)
	if err != nil {
		a.mu.Lock()
		st.Publishing = false
		a.mu.Unlock()
		a.logger.Printf("Publish/update failed: %v", err)
		_ = a.tgSendMessage(chatID, "❌ FunPay отклонил операцию:\n<code>"+escapeTG(err.Error())+"</code>", nil)
		return
	}
	if st.Package.NodeID > 0 {
		a.addKnownNode(st.Package.NodeID)
	}
	a.removeStage(st.Token)
	msg := "✅ <b>Готово.</b>"
	if st.Package.LotID > 0 {
		msg = "✅ <b>Лот обновлён.</b>"
	} else {
		msg = "✅ <b>Лот опубликован на FunPay.</b>"
	}
	if strings.TrimSpace(resp) != "" {
		msg += "\n\n<code>" + escapeTG(cut(resp, 500)) + "</code>"
	}
	_ = a.tgSendMessage(chatID, msg, mainMenuKB())
}

func classifyBulkSkipError(err error) (kind string, skip bool, blockNode bool) {
	if err == nil {
		return "", false, false
	}
	low := strings.ToLower(strings.TrimSpace(err.Error()))

	// FunPay may reject a whole category after the account reaches the allowed
	// number of active offers in that section. This is not a transport failure:
	// skip the current offer, remember the node and continue other categories.
	fullMarkers := []string{
		"много предложений",
		"слишком много предложений",
		"удалите ненужные",
		"достигнут лимит предложений",
		"лимит предложений",
		"too many offers",
		"too many listings",
		"maximum number of offers",
		"maximum offers",
		"offer limit",
		"listing limit",
	}
	for _, m := range fullMarkers {
		if strings.Contains(low, m) {
			return "category_full", true, true
		}
	}

	// Duplicate/same-offer validation means this specific lot is unnecessary.
	// It should never increase the consecutive-failure protection counter.
	duplicateMarkers := []string{
		"предложение уже существует",
		"такое предложение уже существует",
		"уже существует такое предложение",
		"повторяющееся предложение",
		"одинаковое предложение",
		"дублирует",
		"дубликат",
		"duplicate",
		"already exists",
		"already have an offer",
		"already have a listing",
		"похожее предложение уже есть",
	}
	for _, m := range duplicateMarkers {
		if strings.Contains(low, m) {
			return "duplicate", true, false
		}
	}
	return "", false, false
}

func (a *App) resumeBulk(chatID int64) {
	a.mu.RLock()
	if a.bulkRunning {
		a.mu.RUnlock()
		_ = a.tgSendMessage(chatID, "⏳ Массовая публикация уже выполняется.", nil)
		return
	}
	var batch *BatchStage
	for _, b := range a.batches {
		if b.Status == "paused" || b.NextIndex > 0 {
			batch = b
			break
		}
	}
	a.mu.RUnlock()
	if batch == nil {
		_ = a.tgSendMessage(chatID, "ℹ️ Сохранённой пачки для продолжения нет.", nil)
		return
	}
	a.publishBatch(chatID, batch)
}

func isTemporaryCapacityError(err error) bool {
	if err == nil {
		return false
	}
	low := strings.ToLower(err.Error())
	markers := []string{"http 429", "too many requests", "превышено максимальное количество загрузок", "upload limit", "rate limit"}
	for _, marker := range markers {
		if strings.Contains(low, marker) {
			return true
		}
	}
	return false
}

func bulkSkipSummary(batch *BatchStage) string {
	parts := make([]string, 0, 3)
	if batch.LimitSkippedCount > 0 {
		parts = append(parts, fmt.Sprintf("лимит раздела %d", batch.LimitSkippedCount))
	}
	if batch.DuplicateSkippedCount > 0 {
		parts = append(parts, fmt.Sprintf("дубли %d", batch.DuplicateSkippedCount))
	}
	other := batch.SkippedCount - batch.LimitSkippedCount - batch.DuplicateSkippedCount
	if other > 0 {
		parts = append(parts, fmt.Sprintf("ранее/прочее %d", other))
	}
	if len(parts) == 0 {
		return "нет"
	}
	return strings.Join(parts, ", ")
}

func (a *App) publishBatch(chatID int64, batch *BatchStage) {
	a.mu.Lock()
	if _, ok := a.batches[batch.Token]; !ok {
		a.mu.Unlock()
		return
	}
	if a.bulkRunning {
		a.mu.Unlock()
		_ = a.tgSendMessage(chatID, "⏳ Другая массовая публикация уже выполняется. Останови её командой /stop или дождись завершения.", nil)
		return
	}
	delete(a.batches, batch.Token)
	a.bulkRunning = true
	a.bulkCancel = false
	a.bulkDiscard = false
	a.bulkActiveToken = batch.Token
	batch.Status = "running"
	if batch.BlockedNodes == nil {
		batch.BlockedNodes = map[int]bool{}
	}
	a.mu.Unlock()
	if err := a.saveBulkManifest(batch); err != nil {
		a.logger.Printf("bulk checkpoint manifest: %v", err)
	}
	defer func() {
		a.mu.Lock()
		a.bulkRunning = false
		a.bulkCancel = false
		a.bulkDiscard = false
		a.bulkActiveToken = ""
		a.mu.Unlock()
	}()

	total := 0
	for i := range batch.Lots {
		if batch.Selected[i] {
			total++
		}
	}
	if total == 0 {
		a.removeBatchData(batch)
		a.clearBulkCheckpoint(batch.Token)
		_ = a.tgSendMessage(chatID, "⚠️ Не выбран ни один лот.", nil)
		return
	}
	_ = a.tgSendMessage(chatID, fmt.Sprintf("🚀 Массовая публикация: <b>%d</b> лотов.\nПродолжаю с позиции <b>%d</b>.\n\n/stop — поставить на паузу, /resume — продолжить, /cancel — отменить и удалить очередь.", total, batch.NextIndex+1), nil)

	cancelled := false
	pausedByLimit := false
	progressEvery := 10
	if total > 100 {
		progressEvery = 25
	}

	for i, st := range batch.Lots {
		if i < batch.NextIndex || !batch.Selected[i] {
			continue
		}
		a.mu.RLock()
		stop := a.bulkCancel
		a.mu.RUnlock()
		if stop {
			cancelled = true
			break
		}

		// Если FunPay уже сообщил, что раздел переполнен, остальные новые лоты
		// этого node_id пропускаем локально и продолжаем следующую категорию.
		if st.Package.LotID == 0 && st.Package.NodeID > 0 && batch.BlockedNodes[st.Package.NodeID] {
			batch.SkippedCount++
			batch.LimitSkippedCount++
			batch.NextIndex = i + 1
			_ = a.saveBulkProgress(batch)
			continue
		}

		if st.Package.LotID > 0 {
			_ = a.backupLot(st.Package.LotID, "batch_update")
		}
		_, err := a.fp.ApplyPackage(st.Package, st.ImagePaths)
		if err != nil {
			if isTemporaryCapacityError(err) {
				pausedByLimit = true
				batch.Status = "paused"
				a.logger.Printf("bulk paused by FunPay limit item=%d node=%d: %v", i+1, st.Package.NodeID, err)
				break
			}
			skipKind, skip, blockNode := classifyBulkSkipError(err)
			if skip {
				batch.SkippedCount++
				if skipKind == "category_full" {
					batch.LimitSkippedCount++
				} else if skipKind == "duplicate" {
					batch.DuplicateSkippedCount++
				}
				if blockNode && st.Package.NodeID > 0 {
					batch.BlockedNodes[st.Package.NodeID] = true
				}
				a.logger.Printf("bulk skip (%s) node=%d: %v", skipKind, st.Package.NodeID, err)
			} else {
				// Обычная ошибка относится только к текущему лоту.
				// относится только к текущему лоту: записываем её в лог и идём дальше.
				batch.FailCount++
				title := st.Package.TitleRU
				if title == "" {
					title = st.Package.TitleEN
				}
				a.logger.Printf("bulk error item=%d node=%d title=%q: %v", i+1, st.Package.NodeID, cut(title, 80), err)
				// Небольшая техническая пауза, чтобы при мгновенной ошибке не делать
				// сотни запросов в одну миллисекунду. Она не является защитным stop.
				time.Sleep(100 * time.Millisecond)
			}
		} else {
			batch.OKCount++
			if st.Package.NodeID > 0 {
				a.addKnownNode(st.Package.NodeID)
			}
		}
		batch.NextIndex = i + 1
		_ = a.saveBulkProgress(batch)
		processed := batch.OKCount + batch.FailCount + batch.SkippedCount
		if processed%progressEvery == 0 && processed < total {
			_ = a.tgSendMessage(chatID, fmt.Sprintf("⚡ Прогресс: <b>%d/%d</b> • успешно %d • пропущено %d (%s) • ошибок %d", processed, total, batch.OKCount, batch.SkippedCount, bulkSkipSummary(batch), batch.FailCount), nil)
		}
	}

	processed := batch.OKCount + batch.FailCount + batch.SkippedCount
	unprocessed := total - processed
	a.mu.RLock()
	discarded := cancelled && a.bulkDiscard
	a.mu.RUnlock()
	if discarded {
		batch.Status = "cancelled"
		a.removeBatchData(batch)
		a.clearBulkCheckpoint(batch.Token)
		_ = a.tgSendMessage(chatID, fmt.Sprintf("❌ <b>Массовая публикация отменена.</b>\n\nОбработано: <b>%d/%d</b>. Сохранённая очередь удалена — можно отправлять новый ZIP.", processed, total), mainMenuKB())
		return
	}
	if cancelled || pausedByLimit {
		batch.Status = "paused"
		_ = a.saveBulkProgress(batch)
		a.mu.Lock()
		a.batches[batch.Token] = batch
		a.mu.Unlock()
		if pausedByLimit {
			_ = a.tgSendMessage(chatID, fmt.Sprintf("⏸ <b>FunPay временно ограничил загрузки.</b>\n\nПрогресс сохранён: <b>%d/%d</b>. Ничего не потеряно. Подожди и отправь /resume. Для окончательной отмены: /cancel.\n\n✅ %d · ⏭ %d (%s) · ❌ %d", processed, total, batch.OKCount, batch.SkippedCount, bulkSkipSummary(batch), batch.FailCount), mainMenuKB())
		} else {
			_ = a.tgSendMessage(chatID, fmt.Sprintf("⏸ <b>Публикация приостановлена.</b>\n\n✅ %d · ⏭ %d (%s) · ❌ %d · осталось %d\n\nПродолжить: /resume · отменить и удалить: /cancel", batch.OKCount, batch.SkippedCount, bulkSkipSummary(batch), batch.FailCount, unprocessed), mainMenuKB())
		}
		return
	}
	batch.Status = "complete"
	_ = a.saveBulkProgress(batch)
	a.removeBatchData(batch)
	a.clearBulkCheckpoint(batch.Token)
	_ = a.tgSendMessage(chatID, fmt.Sprintf("✅ <b>Пачка обработана до конца.</b>\n\n✅ Успешно: <b>%d</b>\n⏭ Пропущено: <b>%d</b> (%s)\n❌ Ошибок отдельных лотов: <b>%d</b>", batch.OKCount, batch.SkippedCount, bulkSkipSummary(batch), batch.FailCount), mainMenuKB())
}

func (a *App) removeBatch(token string) {
	a.mu.Lock()
	b := a.batches[token]
	delete(a.batches, token)
	a.mu.Unlock()
	if b != nil {
		a.removeBatchData(b)
		a.clearBulkCheckpoint(token)
	}
}

func (a *App) discardBulk() string {
	a.mu.Lock()
	if a.bulkRunning {
		a.bulkCancel = true
		a.bulkDiscard = true
		a.mu.Unlock()
		return "stopping"
	}
	var token string
	for candidate, batch := range a.batches {
		if batch != nil && (batch.Status == "paused" || batch.NextIndex > 0) {
			token = candidate
			break
		}
	}
	a.mu.Unlock()
	if token != "" {
		a.removeBatch(token)
		return "discarded"
	}

	// A previous version could leave only the on-disk checkpoint behind.
	// Explicit /cancel must clear that stale state as well.
	b, err := os.ReadFile(a.bulkJobPath)
	if err != nil {
		if _, progressErr := os.Stat(a.bulkProgressPath); progressErr == nil {
			_ = os.Remove(a.bulkProgressPath)
			_ = os.Remove(a.bulkJobPath)
			return "discarded"
		}
		return "none"
	}
	var batch BatchStage
	if json.Unmarshal(b, &batch) == nil {
		a.removeBatchData(&batch)
	}
	_ = os.Remove(a.bulkProgressPath)
	_ = os.Remove(a.bulkJobPath)
	return "discarded"
}

func (a *App) removeBatchData(b *BatchStage) {
	for _, st := range b.Lots {
		_ = os.RemoveAll(st.TempDir)
		if st.ZipPath != "" {
			_ = os.Remove(st.ZipPath)
		}
	}
}

func (a *App) removeAction(token string) {
	a.mu.Lock()
	act := a.actions[token]
	delete(a.actions, token)
	a.mu.Unlock()
	if act != nil && act.TempDir != "" {
		_ = os.RemoveAll(act.TempDir)
	}
}

func (a *App) sendLots(chatID int64) {
	_ = a.tgSendMessage(chatID, "⏳ Получаю список лотов...", nil)
	a.mu.RLock()
	nodes := append([]int(nil), a.cfg.KnownNodes...)
	a.mu.RUnlock()
	lots, err := a.fp.GetMyLots(nodes)
	if err != nil {
		_ = a.tgSendMessage(chatID, "❌ Не удалось получить лоты:\n<code>"+escapeTG(err.Error())+"</code>", nil)
		return
	}
	if len(lots) == 0 {
		_ = a.tgSendMessage(chatID, "🛒 Лотов не найдено.", nil)
		return
	}
	sort.Slice(lots, func(i, j int) bool {
		if lots[i].NodeID != lots[j].NodeID {
			return lots[i].NodeID < lots[j].NodeID
		}
		return lots[i].ID < lots[j].ID
	})
	var rows [][]map[string]string
	limit := len(lots)
	if limit > 30 {
		limit = 30
	}
	for _, lot := range lots[:limit] {
		icon := "🟢"
		if !lot.Active {
			icon = "🔴"
		}
		rows = append(rows, []map[string]string{{"text": fmt.Sprintf("%s %s — %.2f", icon, cut(lot.Title, 34), lot.Price), "callback_data": fmt.Sprintf("lot:%d", lot.ID)}})
	}
	text := fmt.Sprintf("🛒 <b>МОИ ЛОТЫ</b>\n\nНайдено: <b>%d</b>", len(lots))
	if len(lots) > limit {
		text += fmt.Sprintf("\nПоказываю первые %d.", limit)
	}
	_ = a.tgSendMessage(chatID, text, map[string]any{"inline_keyboard": rows})
}

func (a *App) sendLotDetails(chatID int64, lotID int) {
	fields, err := a.fp.GetLotFields(lotID)
	if err != nil {
		_ = a.tgSendMessage(chatID, "❌ Не удалось открыть лот:\n<code>"+escapeTG(err.Error())+"</code>", nil)
		return
	}
	title := fields["fields[summary][ru]"]
	if title == "" {
		title = fields["fields[summary][en]"]
	}
	price := fields["price"]
	active := fields["active"] == "on"
	icon := "🟢"
	toggleText := "🔴 Выключить"
	toggleVal := "0"
	if !active {
		icon = "🔴"
		toggleText = "🟢 Включить"
		toggleVal = "1"
	}
	text := fmt.Sprintf("%s <b>%s</b>\n\nID: <code>%d</code>\nЦена: <b>%s</b>\nАктивен: <b>%v</b>\n\nДля точной цены: <code>/price %d 199</code>", icon, escapeTG(cut(title, 180)), lotID, escapeTG(price), active, lotID)
	kb := map[string]any{"inline_keyboard": [][]map[string]string{
		{{"text": toggleText, "callback_data": fmt.Sprintf("toggle:%d:%s", lotID, toggleVal)}, {"text": "📋 Клон", "callback_data": fmt.Sprintf("clone:%d", lotID)}},
		{{"text": "✏️ Экспорт для правки", "callback_data": fmt.Sprintf("export:%d", lotID)}},
		{{"text": "🗑 Удалить", "callback_data": fmt.Sprintf("delask:%d", lotID)}, {"text": "⬅️ К списку", "callback_data": "menu:lots"}},
	}}
	_ = a.tgSendMessage(chatID, text, kb)
}

func (a *App) commandPrice(chatID int64, txt string) {
	p := strings.Fields(txt)
	if len(p) != 3 {
		_ = a.tgSendMessage(chatID, "Формат: <code>/price ID 199</code>", nil)
		return
	}
	id, e1 := strconv.Atoi(p[1])
	price, e2 := strconv.ParseFloat(strings.ReplaceAll(p[2], ",", "."), 64)
	if e1 != nil || e2 != nil || id <= 0 || price <= 0 {
		_ = a.tgSendMessage(chatID, "❌ Неверный ID или цена.", nil)
		return
	}
	if err := a.backupLot(id, "price"); err != nil {
		_ = a.tgSendMessage(chatID, "❌ Не удалось создать backup: "+escapeTG(err.Error()), nil)
		return
	}
	if err := a.fp.SetLotPrice(id, price); err != nil {
		_ = a.tgSendMessage(chatID, "❌ Цена не изменена:\n<code>"+escapeTG(err.Error())+"</code>", nil)
		return
	}
	_ = a.tgSendMessage(chatID, fmt.Sprintf("✅ Цена лота <code>%d</code> → <b>%.2f</b>", id, price), nil)
}

func (a *App) commandBulkPrices(chatID int64, txt string) {
	args := strings.Fields(strings.TrimSpace(strings.TrimPrefix(txt, "/prices")))
	if len(args) < 1 || len(args) > 2 {
		_ = a.tgSendMessage(chatID, "Формат:\n<code>/prices -15%</code>\nили <code>/prices 1858 -15%</code>", nil)
		return
	}
	node := 0
	change := args[0]
	if len(args) == 2 {
		n, err := strconv.Atoi(args[0])
		if err != nil || n <= 0 {
			_ = a.tgSendMessage(chatID, "❌ Неверный node_id.", nil)
			return
		}
		node = n
		change = args[1]
	}
	a.mu.RLock()
	nodes := append([]int(nil), a.cfg.KnownNodes...)
	a.mu.RUnlock()
	lots, err := a.fp.GetMyLots(nodes)
	if err != nil {
		_ = a.tgSendMessage(chatID, "❌ Не удалось получить лоты: "+escapeTG(err.Error()), nil)
		return
	}
	newPrices := map[int]float64{}
	for _, lot := range lots {
		if node > 0 && lot.NodeID != node {
			continue
		}
		np, ok := calculatePriceChange(lot.Price, change)
		if ok && np > 0 {
			newPrices[lot.ID] = np
		}
	}
	if len(newPrices) == 0 {
		_ = a.tgSendMessage(chatID, "⚠️ Нет подходящих лотов или неверный формат изменения.", nil)
		return
	}
	act := &PendingAction{Token: randomToken(8), Kind: "prices", NewPrices: newPrices, Description: fmt.Sprintf("Изменить цены у %d лотов: %s", len(newPrices), change), CreatedAt: time.Now()}
	a.mu.Lock()
	a.actions[act.Token] = act
	a.mu.Unlock()

	var sample []string
	for _, lot := range lots {
		if np, ok := newPrices[lot.ID]; ok {
			sample = append(sample, fmt.Sprintf("#%d %.2f → %.2f", lot.ID, lot.Price, np))
			if len(sample) >= 8 {
				break
			}
		}
	}
	_ = a.tgSendMessage(chatID, fmt.Sprintf("💰 <b>МАССОВОЕ ИЗМЕНЕНИЕ ЦЕН</b>\n\nЛотов: <b>%d</b>\n%s\n\n<code>%s</code>", len(newPrices), escapeTG(change), escapeTG(strings.Join(sample, "\n"))), actionConfirmKB(act.Token))
}

func calculatePriceChange(old float64, expr string) (float64, bool) {
	expr = strings.TrimSpace(strings.ReplaceAll(expr, ",", "."))
	if strings.HasSuffix(expr, "%") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(expr, "%"), 64)
		if err != nil {
			return 0, false
		}
		return math.Round((old*(1+v/100))*100) / 100, true
	}
	if strings.HasPrefix(expr, "=") {
		expr = strings.TrimPrefix(expr, "=")
	}
	v, err := strconv.ParseFloat(expr, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return math.Round(v*100) / 100, true
}

func (a *App) commandToggle(chatID int64, txt string, on bool) {
	p := strings.Fields(txt)
	if len(p) != 2 {
		_ = a.tgSendMessage(chatID, "Формат: <code>/on ID</code> или <code>/off ID</code>", nil)
		return
	}
	id, err := strconv.Atoi(p[1])
	if err != nil || id <= 0 {
		_ = a.tgSendMessage(chatID, "❌ Неверный ID.", nil)
		return
	}
	a.toggleLot(chatID, id, on)
}

func (a *App) toggleLot(chatID int64, id int, on bool) {
	if err := a.backupLot(id, "toggle"); err != nil {
		_ = a.tgSendMessage(chatID, "❌ Backup не создан: "+escapeTG(err.Error()), nil)
		return
	}
	if err := a.fp.SetLotActive(id, on); err != nil {
		_ = a.tgSendMessage(chatID, "❌ Не удалось изменить состояние:\n<code>"+escapeTG(err.Error())+"</code>", nil)
		return
	}
	state := "🔴 выключен"
	if on {
		state = "🟢 включён"
	}
	_ = a.tgSendMessage(chatID, fmt.Sprintf("✅ Лот <code>%d</code> %s.", id, state), nil)
}

func (a *App) commandClone(chatID int64, txt string) {
	p := strings.Fields(txt)
	if len(p) != 2 {
		_ = a.tgSendMessage(chatID, "Формат: <code>/clone ID</code>", nil)
		return
	}
	id, err := strconv.Atoi(p[1])
	if err != nil || id <= 0 {
		return
	}
	a.cloneLot(chatID, id)
}

func (a *App) cloneLot(chatID int64, id int) {
	resp, err := a.fp.CloneLot(id)
	if err != nil {
		_ = a.tgSendMessage(chatID, "❌ Не удалось клонировать:\n<code>"+escapeTG(err.Error())+"</code>", nil)
		return
	}
	_ = a.tgSendMessage(chatID, "✅ Клон лота создан.\n<code>"+escapeTG(cut(resp, 500))+"</code>", nil)
}

func (a *App) commandDelete(chatID int64, txt string) {
	p := strings.Fields(txt)
	if len(p) != 2 {
		_ = a.tgSendMessage(chatID, "Формат: <code>/delete ID</code>", nil)
		return
	}
	id, err := strconv.Atoi(p[1])
	if err != nil || id <= 0 {
		return
	}
	kb := map[string]any{"inline_keyboard": [][]map[string]string{{{"text": "🗑 Да, удалить", "callback_data": fmt.Sprintf("del:%d", id)}, {"text": "❌ Отмена", "callback_data": "menu:lots"}}}}
	_ = a.tgSendMessage(chatID, fmt.Sprintf("⚠️ Точно удалить лот <code>%d</code>?", id), kb)
}

func (a *App) deleteLot(chatID int64, id int) {
	if err := a.backupLot(id, "delete"); err != nil {
		_ = a.tgSendMessage(chatID, "❌ Backup не создан: "+escapeTG(err.Error()), nil)
		return
	}
	if err := a.fp.DeleteLot(id); err != nil {
		_ = a.tgSendMessage(chatID, "❌ Не удалось удалить:\n<code>"+escapeTG(err.Error())+"</code>", nil)
		return
	}
	_ = a.tgSendMessage(chatID, fmt.Sprintf("🗑 Лот <code>%d</code> удалён. Backup сохранён — можно использовать /rollback.", id), nil)
}

func (a *App) commandExport(chatID int64, txt string) {
	p := strings.Fields(txt)
	if len(p) != 2 {
		_ = a.tgSendMessage(chatID, "Формат: <code>/export ID</code>", nil)
		return
	}
	id, err := strconv.Atoi(p[1])
	if err != nil || id <= 0 {
		_ = a.tgSendMessage(chatID, "❌ Неверный ID.", nil)
		return
	}
	a.exportLot(chatID, id)
}

func (a *App) exportLot(chatID int64, id int) {
	fields, err := a.fp.GetLotFields(id)
	if err != nil {
		_ = a.tgSendMessage(chatID, "❌ Не удалось прочитать лот:\n<code>"+escapeTG(err.Error())+"</code>", nil)
		return
	}
	price, _ := strconv.ParseFloat(strings.ReplaceAll(fields["price"], ",", "."), 64)
	active := fields["active"] == "on"
	pkg := LotPackage{
		Version:       2,
		LotID:         id,
		TitleRU:       fields["fields[summary][ru]"],
		TitleEN:       fields["fields[summary][en]"],
		DescriptionRU: fields["fields[desc][ru]"],
		DescriptionEN: fields["fields[desc][en]"],
		PaymentMsgRU:  fields["fields[payment_msg][ru]"],
		PaymentMsgEN:  fields["fields[payment_msg][en]"],
		Price:         price,
		Active:        &active,
		RawFields:     map[string]string{},
	}
	if av := strings.TrimSpace(fields["amount"]); av != "" {
		if n, e := strconv.Atoi(av); e == nil {
			pkg.Amount = &n
		}
	}
	if _, ok := fields["deactivate_after_sale"]; ok {
		v := fields["deactivate_after_sale"] == "on"
		pkg.DeactivateAfterSale = &v
	}
	skip := map[string]bool{
		"csrf_token": true, "offer_id": true, "deleted": true, "price": true, "active": true, "amount": true,
		"fields[summary][ru]": true, "fields[summary][en]": true,
		"fields[desc][ru]": true, "fields[desc][en]": true,
		"fields[payment_msg][ru]": true, "fields[payment_msg][en]": true,
		"fields[images]": true, "secrets": true, "auto_delivery": true, "deactivate_after_sale": true,
	}
	for k, v := range fields {
		if !skip[k] {
			pkg.RawFields[k] = v
		}
	}
	if len(pkg.RawFields) == 0 {
		pkg.RawFields = nil
	}
	tmpDir, err := os.MkdirTemp(a.dataDir, "export-")
	if err != nil {
		_ = a.tgSendMessage(chatID, "❌ Не удалось подготовить экспорт.", nil)
		return
	}
	defer os.RemoveAll(tmpDir)
	jsonPath := filepath.Join(tmpDir, "lot.json")
	b, _ := json.MarshalIndent(pkg, "", "  ")
	if err := os.WriteFile(jsonPath, b, 0600); err != nil {
		_ = a.tgSendMessage(chatID, "❌ Не удалось записать lot.json.", nil)
		return
	}
	zipPath := filepath.Join(a.dataDir, fmt.Sprintf("lot_%d_edit_%s.zip", id, randomToken(3)))
	zf, err := os.Create(zipPath)
	if err != nil {
		_ = a.tgSendMessage(chatID, "❌ Не удалось создать ZIP.", nil)
		return
	}
	zw := zip.NewWriter(zf)
	w, _ := zw.Create("lot.json")
	_, _ = w.Write(b)
	_ = zw.Close()
	_ = zf.Close()
	defer os.Remove(zipPath)
	caption := fmt.Sprintf("✏️ Лот #%d выгружен для редактирования.\nИзмени lot.json и отправь ZIP обратно боту — V4 обновит этот лот, а не создаст новый.", id)
	if err := a.tgSendDocument(chatID, zipPath, caption); err != nil {
		_ = a.tgSendMessage(chatID, "❌ Не удалось отправить файл:\n<code>"+escapeTG(err.Error())+"</code>", nil)
	}
}

func (a *App) executeAction(chatID int64, act *PendingAction) {
	switch act.Kind {
	case "prices":
		_ = a.tgSendMessage(chatID, fmt.Sprintf("⏳ Меняю цены у %d лотов...", len(act.NewPrices)), nil)
		ok, fail := 0, 0
		var errs []string
		ids := make([]int, 0, len(act.NewPrices))
		for id := range act.NewPrices {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		for _, id := range ids {
			_ = a.backupLot(id, "bulk_price")
			if err := a.fp.SetLotPrice(id, act.NewPrices[id]); err != nil {
				fail++
				errs = append(errs, fmt.Sprintf("#%d: %s", id, cut(err.Error(), 100)))
			} else {
				ok++
			}
			time.Sleep(500 * time.Millisecond)
		}
		a.removeAction(act.Token)
		text := fmt.Sprintf("✅ Изменено: <b>%d</b>\n❌ Ошибок: <b>%d</b>", ok, fail)
		if len(errs) > 0 {
			text += "\n\n<code>" + escapeTG(strings.Join(errs, "\n")) + "</code>"
		}
		_ = a.tgSendMessage(chatID, text, nil)
	case "images":
		_ = a.tgSendMessage(chatID, fmt.Sprintf("⏳ Заменяю карточки у %d лотов...", len(act.ImagePaths)), nil)
		ok, fail := 0, 0
		var errs []string
		ids := make([]int, 0, len(act.ImagePaths))
		for id := range act.ImagePaths {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		for _, id := range ids {
			_ = a.backupLot(id, "replace_images")
			if err := a.fp.ReplaceLotImages(id, []string{act.ImagePaths[id]}); err != nil {
				fail++
				errs = append(errs, fmt.Sprintf("#%d: %s", id, cut(err.Error(), 100)))
			} else {
				ok++
			}
			time.Sleep(700 * time.Millisecond)
		}
		a.removeAction(act.Token)
		text := fmt.Sprintf("🖼 Карточки заменены: <b>%d</b>\n❌ Ошибок: <b>%d</b>", ok, fail)
		if len(errs) > 0 {
			text += "\n\n<code>" + escapeTG(strings.Join(errs, "\n")) + "</code>"
		}
		_ = a.tgSendMessage(chatID, text, nil)
	}
}

func (a *App) sendCategories(chatID int64, filter string) {
	cats, err := a.fp.GetCategories()
	if err != nil {
		_ = a.tgSendMessage(chatID, "❌ Не удалось прочитать категории: "+escapeTG(err.Error()), nil)
		return
	}
	f := normalizeText(filter)
	var lines []string
	for _, c := range cats {
		path := c.Game + " > " + c.Name
		if f != "" && !strings.Contains(normalizeText(path), f) {
			continue
		}
		lines = append(lines, fmt.Sprintf("<code>%d</code> — %s", c.ID, escapeTG(path)))
		if len(lines) >= 30 {
			break
		}
	}
	if len(lines) == 0 {
		_ = a.tgSendMessage(chatID, "Ничего не найдено.", nil)
		return
	}
	_ = a.tgSendMessage(chatID, "📂 <b>Категории</b>\n\n"+strings.Join(lines, "\n"), nil)
}

func (a *App) backupLot(lotID int, action string) error {
	fields, err := a.fp.GetLotFields(lotID)
	if err != nil {
		return err
	}
	rec := BackupRecord{LotID: lotID, Action: action, CreatedAt: time.Now(), Fields: cloneMap(fields)}
	name := fmt.Sprintf("%s_%d_%s.json", time.Now().Format("20060102_150405.000"), lotID, sanitizeFileName(action))
	b, _ := json.MarshalIndent(rec, "", "  ")
	return os.WriteFile(filepath.Join(a.backupDir, name), b, 0600)
}

func (a *App) askRollback(chatID int64) {
	rec, _, err := a.latestBackup()
	if err != nil {
		_ = a.tgSendMessage(chatID, "↩️ Backup пока нет.", nil)
		return
	}
	kb := map[string]any{"inline_keyboard": [][]map[string]string{{{"text": "↩️ Откатить", "callback_data": "rollback:yes"}, {"text": "Отмена", "callback_data": "menu:status"}}}}
	_ = a.tgSendMessage(chatID, fmt.Sprintf("↩️ Последний backup:\nЛот: <code>%d</code>\nДействие: <b>%s</b>\nВремя: %s\n\nОткатить?", rec.LotID, escapeTG(rec.Action), rec.CreatedAt.Format("02.01 15:04:05")), kb)
}

func (a *App) rollbackLast(chatID int64) {
	rec, path, err := a.latestBackup()
	if err != nil {
		_ = a.tgSendMessage(chatID, "↩️ Нет backup для отката.", nil)
		return
	}
	err = a.fp.RestoreLotFields(rec.LotID, rec.Fields)
	restoredAsClone := false
	if err != nil && rec.Action == "delete" {
		// Удалённый offer_id FunPay иногда уже не принимает обратно. В таком случае восстанавливаем содержимое новым лотом.
		if _, cloneErr := a.fp.SaveLotFields(0, cloneMap(rec.Fields)); cloneErr == nil {
			err = nil
			restoredAsClone = true
		}
	}
	if err != nil {
		_ = a.tgSendMessage(chatID, "❌ Откат не выполнен:\n<code>"+escapeTG(err.Error())+"</code>", nil)
		return
	}
	_ = os.Rename(path, path+".restored")
	if restoredAsClone {
		_ = a.tgSendMessage(chatID, fmt.Sprintf("✅ Содержимое удалённого лота <code>%d</code> восстановлено новым лотом.", rec.LotID), nil)
	} else {
		_ = a.tgSendMessage(chatID, fmt.Sprintf("✅ Лот <code>%d</code> восстановлен из backup.", rec.LotID), nil)
	}
}

func (a *App) latestBackup() (*BackupRecord, string, error) {
	ents, err := os.ReadDir(a.backupDir)
	if err != nil {
		return nil, "", err
	}
	var files []os.DirEntry
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			files = append(files, e)
		}
	}
	if len(files) == 0 {
		return nil, "", errors.New("no backups")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() > files[j].Name() })
	path := filepath.Join(a.backupDir, files[0].Name())
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var rec BackupRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		return nil, "", err
	}
	return &rec, path, nil
}

func (a *App) sendRecentOrders(chatID int64) {
	orders, err := a.fp.GetRecentOrders()
	if err != nil {
		_ = a.tgSendMessage(chatID, "❌ Не удалось получить заказы:\n<code>"+escapeTG(err.Error())+"</code>", backHomeKB())
		return
	}
	if len(orders) == 0 {
		_ = a.tgSendMessage(chatID, "🔔 Заказов на текущей странице пока нет.", backHomeKB())
		return
	}
	limit := len(orders)
	if limit > 10 {
		limit = 10
	}
	var b strings.Builder
	b.WriteString("🔔 <b>ПОСЛЕДНИЕ ЗАКАЗЫ</b>\n\n")
	for i, o := range orders[:limit] {
		icon := "🟡"
		switch o.Status {
		case "paid":
			icon = "💳"
		case "closed":
			icon = "✅"
		case "refunded":
			icon = "↩️"
		}
		fmt.Fprintf(&b, "%s <b>%s</b> • %.2f %s\n%s\n<code>%s</code>\n", icon, escapeTG(o.Status), o.Price, escapeTG(o.Currency), escapeTG(cut(o.Description, 90)), escapeTG(o.ID))
		if i != limit-1 {
			b.WriteString("\n")
		}
	}
	kb := map[string]any{"inline_keyboard": [][]map[string]string{
		{{"text": "🔄 Обновить", "callback_data": "menu:orders"}, {"text": "📊 Статистика", "callback_data": "menu:stats"}},
		{{"text": "⬅️ Главное меню", "callback_data": "menu:home"}},
	}}
	_ = a.tgSendMessage(chatID, b.String(), kb)
}

func (a *App) sendStats(chatID int64) {
	orders, err := a.fp.GetRecentOrders()
	if err != nil {
		_ = a.tgSendMessage(chatID, "❌ Не удалось получить продажи:\n<code>"+escapeTG(err.Error())+"</code>", nil)
		return
	}
	a.mu.RLock()
	nodes := append([]int(nil), a.cfg.KnownNodes...)
	a.mu.RUnlock()
	lots, _ := a.fp.GetMyLots(nodes)
	paid, closed, refunded, today := 0, 0, 0, 0
	var revenue float64
	for _, o := range orders {
		switch o.Status {
		case "paid":
			paid++
			revenue += o.Price
		case "closed":
			closed++
			revenue += o.Price
		case "refunded":
			refunded++
		}
		d := strings.ToLower(o.DateText)
		if strings.Contains(d, "сегодня") || strings.Contains(d, "today") || strings.Contains(d, "сьогодні") {
			today++
		}
	}
	text := fmt.Sprintf(
		"📊 <b>БЫСТРАЯ СТАТИСТИКА</b>\n\nАктивных/известных лотов: <b>%d</b>\nЗаказов на текущей странице продаж: <b>%d</b>\nСегодня: <b>%d</b>\nОплачено: <b>%d</b>\nЗакрыто: <b>%d</b>\nВозвраты: <b>%d</b>\nСумма видимых оплаченных/закрытых: <b>%.2f</b>\n\n<i>Это быстрая статистика по последним заказам, которые FunPay отдаёт на первой странице.</i>",
		len(lots), len(orders), today, paid, closed, refunded, revenue)
	_ = a.tgSendMessage(chatID, text, nil)
}

func (a *App) orderWatchLoop() {
	for {
		time.Sleep(60 * time.Second)
		a.mu.RLock()
		cfg := a.cfg
		bulk := a.bulkRunning
		a.mu.RUnlock()
		if bulk {
			continue
		}
		if !cfg.NotifyOrders || cfg.AdminChatID == 0 || cfg.GoldenKey == "" || cfg.TelegramToken == "" {
			continue
		}
		orders, err := a.fp.GetRecentOrders()
		if err != nil {
			a.logger.Printf("order watch: %v", err)
			continue
		}
		a.mu.Lock()
		if !a.ordersReady {
			for _, o := range orders {
				a.seenOrders[o.ID] = true
			}
			a.ordersReady = true
			a.mu.Unlock()
			continue
		}
		var fresh []OrderBrief
		for _, o := range orders {
			if !a.seenOrders[o.ID] {
				a.seenOrders[o.ID] = true
				fresh = append(fresh, o)
			}
		}
		a.mu.Unlock()
		for _, o := range fresh {
			icon := "🛒"
			if o.Status == "refunded" {
				icon = "↩️"
			}
			_ = a.tgSendMessage(cfg.AdminChatID, fmt.Sprintf("%s <b>Новый заказ FunPay</b>\n\nID: <code>%s</code>\n%s\nЦена: <b>%.2f %s</b>\nСтатус: <b>%s</b>", icon, escapeTG(o.ID), escapeTG(cut(o.Description, 180)), o.Price, escapeTG(o.Currency), escapeTG(o.Status)), nil)
		}
	}
}

func normalizeText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "ё", "е")
	re := regexp.MustCompile(`[^a-zа-я0-9]+`)
	return strings.TrimSpace(re.ReplaceAllString(s, " "))
}

func sanitizeFileName(s string) string {
	re := regexp.MustCompile(`[^A-Za-z0-9_-]+`)
	s = re.ReplaceAllString(s, "_")
	if s == "" {
		return "backup"
	}
	return s
}

func cloneMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (a *App) tgSendMessage(chatID int64, text string, replyMarkup any) error {
	a.mu.RLock()
	token := a.cfg.TelegramToken
	a.mu.RUnlock()
	if token == "" {
		return errors.New("Telegram token не задан")
	}
	vals := url.Values{"chat_id": {strconv.FormatInt(chatID, 10)}, "text": {text}, "parse_mode": {"HTML"}, "disable_web_page_preview": {"true"}}
	if replyMarkup != nil {
		b, _ := json.Marshal(replyMarkup)
		vals.Set("reply_markup", string(b))
	}
	_, err := tgPost(token, "sendMessage", vals)
	return err
}

func (a *App) tgEditMessage(chatID int64, messageID int64, text string, replyMarkup any) error {
	a.mu.RLock()
	token := a.cfg.TelegramToken
	a.mu.RUnlock()
	if token == "" {
		return errors.New("Telegram token не задан")
	}
	vals := url.Values{
		"chat_id":                  {strconv.FormatInt(chatID, 10)},
		"message_id":               {strconv.FormatInt(messageID, 10)},
		"text":                     {text},
		"parse_mode":               {"HTML"},
		"disable_web_page_preview": {"true"},
	}
	if replyMarkup != nil {
		b, _ := json.Marshal(replyMarkup)
		vals.Set("reply_markup", string(b))
	}
	_, err := tgPost(token, "editMessageText", vals)
	return err
}

func (a *App) tgSendDocument(chatID int64, filePath, caption string) error {
	a.mu.RLock()
	token := a.cfg.TelegramToken
	a.mu.RUnlock()
	if token == "" {
		return errors.New("Telegram token не задан")
	}
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("chat_id", strconv.FormatInt(chatID, 10))
	_ = mw.WriteField("caption", caption)
	_ = mw.WriteField("parse_mode", "HTML")
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="document"; filename="%s"`, filepath.Base(filePath)))
	h.Set("Content-Type", "application/zip")
	part, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
		return err
	}
	_ = mw.Close()
	req, _ := http.NewRequest("POST", fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", token), &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := (&http.Client{Timeout: 45 * time.Second}).Do(req)
	if err != nil {
		return sanitizeSecretError(err, token)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var x struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal(b, &x)
	if !x.OK {
		return errors.New(x.Description)
	}
	return nil
}

func (a *App) tgAnswerCallback(id, text string) error {
	a.mu.RLock()
	token := a.cfg.TelegramToken
	a.mu.RUnlock()
	_, err := tgPost(token, "answerCallbackQuery", url.Values{"callback_query_id": {id}, "text": {text}})
	return err
}

func tgPost(token, method string, vals url.Values) ([]byte, error) {
	req, _ := http.NewRequest("POST", fmt.Sprintf("https://api.telegram.org/bot%s/%s", token, method), strings.NewReader(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cl := &http.Client{Timeout: 30 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, sanitizeSecretError(err, token)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var x struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal(b, &x)
	if !x.OK {
		return b, errors.New(x.Description)
	}
	return b, nil
}

func tgGetBot(token string) (*TGBotUser, error) {
	b, err := tgPost(token, "getMe", url.Values{})
	if err != nil {
		return nil, err
	}
	var r TGResponse[TGBotUser]
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	if !r.OK || r.Result.ID == 0 || strings.TrimSpace(r.Result.Username) == "" {
		return nil, errors.New("Telegram не вернул данные бота")
	}
	return &r.Result, nil
}

func (a *App) tgDownloadDocument(fileID, fileName string) (string, error) {
	a.mu.RLock()
	token := a.cfg.TelegramToken
	a.mu.RUnlock()
	b, err := tgPost(token, "getFile", url.Values{"file_id": {fileID}})
	if err != nil {
		return "", err
	}
	var r TGResponse[TGFile]
	if err := json.Unmarshal(b, &r); err != nil {
		return "", err
	}
	if r.Result.FilePath == "" {
		return "", errors.New("Telegram не вернул file_path")
	}
	dl := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", token, r.Result.FilePath)
	resp, err := http.Get(dl)
	if err != nil {
		return "", sanitizeSecretError(err, token)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download HTTP %d", resp.StatusCode)
	}
	safe := filepath.Base(fileName)
	if safe == "." || safe == "" {
		safe = "lot.zip"
	}
	path := filepath.Join(a.dataDir, fmt.Sprintf("upload-%s-%s", randomToken(4), safe))
	out, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer out.Close()
	written, err := io.Copy(out, io.LimitReader(resp.Body, (200<<20)+1))
	if err != nil {
		return "", err
	}
	if written > 200<<20 {
		_ = out.Close()
		_ = os.Remove(path)
		return "", errors.New("файл больше 200 МБ; разбей пакет на несколько архивов")
	}
	return path, nil
}

func NewFunPayClient(goldenKey, userAgent string) *FunPayClient {
	jar, _ := cookiejar.New(nil)
	cl := &http.Client{Jar: jar, Timeout: 25 * time.Second}
	return &FunPayClient{
		goldenKey: goldenKey, userAgent: userAgent, client: cl,
		createFieldsCache: make(map[int]cachedCreateFields),
		nodeLotsCache:     make(map[int]cachedNodeLots),
		imageCache:        make(map[string]int),
	}
}

func (f *FunPayClient) Init() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.TrimSpace(f.goldenKey) == "" {
		return "", errors.New("golden_key не задан")
	}
	// Массовый режим: не проверяем главную FunPay перед каждой операцией.
	// Сессия повторно валидируется раз в 5 минут.
	if f.username != "" && !f.initAt.IsZero() && time.Since(f.initAt) < 5*time.Minute {
		return f.username, nil
	}
	root, _ := url.Parse("https://funpay.com/")
	f.client.Jar.SetCookies(root, []*http.Cookie{{Name: "golden_key", Value: f.goldenKey, Path: "/"}, {Name: "cookie_prefs", Value: "1", Path: "/"}})
	req, _ := http.NewRequest("GET", "https://funpay.com/", nil)
	req.Header.Set("User-Agent", f.userAgent)
	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("FunPay HTTP %d", resp.StatusCode)
	}
	if strings.Contains(resp.Request.URL.Path, "account/login") {
		return "", errors.New("golden_key недействителен")
	}
	s := string(b)
	data := firstSubmatch(reAppData, s)
	if data == "" {
		return "", errors.New("не удалось прочитать data-app-data — возможно, golden_key недействителен")
	}
	data = html.UnescapeString(data)
	var app map[string]any
	if err := json.Unmarshal([]byte(data), &app); err != nil {
		return "", fmt.Errorf("app-data: %w", err)
	}
	if x, ok := app["csrf-token"].(string); ok {
		f.pageCSRFToken = x
	}
	switch v := app["userId"].(type) {
	case float64:
		f.userID = int(v)
	case string:
		f.userID, _ = strconv.Atoi(v)
	}
	uname := stripTags(firstSubmatch(reUsername, s))
	if strings.TrimSpace(uname) == "" {
		return "", errors.New("FunPay не вернул имя пользователя — авторизация не подтверждена")
	}
	f.username = html.UnescapeString(strings.TrimSpace(uname))
	f.categories = parseCategoriesHTML(s)
	f.initAt = time.Now()
	return f.username, nil
}

func (f *FunPayClient) PublishLot(p LotPackage, imagePaths []string) (string, error) {
	if _, err := f.Init(); err != nil {
		return "", err
	}
	// Важно: csrf_token и form_created_at принадлежат конкретной форме offerEdit.
	// Их нельзя кэшировать и повторно использовать для десятков созданий подряд.
	f.mu.Lock()
	defer f.mu.Unlock()

	var imageIDs []string
	for _, path := range imagePaths {
		id, err := f.uploadImageLocked(path)
		if err != nil {
			return "", fmt.Errorf("картинка: %w", err)
		}
		imageIDs = append(imageIDs, strconv.Itoa(id))
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		fields, err := f.getLotFormLocked(p.NodeID) // свежая форма на КАЖДУЮ попытку
		if err != nil {
			lastErr = err
			if attempt < 3 {
				time.Sleep(time.Duration(attempt) * 700 * time.Millisecond)
				continue
			}
			break
		}
		if p.RawFields != nil {
			for k, v := range p.RawFields {
				fields[k] = v
			}
		}
		fields["offer_id"] = "0"
		fields["fields[summary][ru]"] = p.TitleRU
		fields["fields[summary][en]"] = p.TitleEN
		fields["fields[desc][ru]"] = p.DescriptionRU
		fields["fields[desc][en]"] = p.DescriptionEN
		fields["fields[payment_msg][ru]"] = p.PaymentMsgRU
		fields["fields[payment_msg][en]"] = p.PaymentMsgEN
		fields["price"] = trimFloat(p.Price)
		fields["fields[images]"] = strings.Join(imageIDs, ",")
		active := true
		if p.Active != nil {
			active = *p.Active
		}
		if active {
			fields["active"] = "on"
		} else {
			fields["active"] = ""
		}
		if p.Amount != nil {
			fields["amount"] = strconv.Itoa(*p.Amount)
		}
		if p.DeactivateAfterSale != nil {
			if *p.DeactivateAfterSale {
				fields["deactivate_after_sale"] = "on"
			} else {
				fields["deactivate_after_sale"] = ""
			}
		}

		vals := url.Values{}
		for k, v := range fields {
			vals.Set(k, v)
		}
		if strings.TrimSpace(vals.Get("csrf_token")) == "" {
			lastErr = errors.New("свежая форма FunPay не содержит csrf_token")
			continue
		}
		req, _ := http.NewRequest("POST", "https://funpay.com/lots/offerSave", strings.NewReader(vals.Encode()))
		req.Header.Set("User-Agent", f.userAgent)
		req.Header.Set("Accept", "*/*")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
		if f.pageCSRFToken != "" {
			req.Header.Set("X-CSRF-Token", f.pageCSRFToken)
		}

		f.waitForRequest(350 * time.Millisecond)
		resp, err := f.client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < 3 {
				time.Sleep(time.Duration(attempt) * time.Second)
				continue
			}
			break
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		body := strings.TrimSpace(string(b))
		if resp.StatusCode != 200 {
			lastErr = fmt.Errorf("offerSave HTTP %d: %s", resp.StatusCode, cut(body, 600))
			if attempt < 3 && (resp.StatusCode == 408 || resp.StatusCode == 429 || resp.StatusCode >= 500) {
				delay := time.Duration(attempt) * 2 * time.Second
				if resp.StatusCode == 429 {
					delay = 5 * time.Second
				}
				time.Sleep(delay)
				continue
			}
			break
		}
		var j map[string]any
		if json.Unmarshal(b, &j) == nil {
			if e := extractFunPayError(j); e != "" {
				lastErr = errors.New(e)
				low := strings.ToLower(e)
				retryFreshForm := strings.Contains(low, "обновите страницу") || strings.Contains(low, "повторите попытку") || strings.Contains(low, "csrf") || strings.Contains(low, "form_created_at")
				if attempt < 3 && retryFreshForm {
					time.Sleep(time.Duration(attempt) * 650 * time.Millisecond)
					continue
				}
				break
			}
		}
		delete(f.nodeLotsCache, p.NodeID)
		return body, nil
	}
	if lastErr == nil {
		lastErr = errors.New("FunPay не подтвердил создание лота")
	}
	return "", lastErr
}

func (f *FunPayClient) getLotFormLocked(nodeID int) (map[string]string, error) {
	u := fmt.Sprintf("https://funpay.com/lots/offerEdit?node=%d", nodeID)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", f.userAgent)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("offerEdit HTTP %d", resp.StatusCode)
	}
	s := string(b)
	form := firstSubmatch(reOfferForm, s)
	if form == "" {
		lead := stripTags(firstSubmatch(reLead, s))
		if lead != "" {
			return nil, errors.New(strings.TrimSpace(html.UnescapeString(lead)))
		}
		return nil, errors.New("не найдена форма создания лота для node_id")
	}
	fields := parseFormFields(form)
	if strings.TrimSpace(fields["csrf_token"]) == "" {
		return nil, errors.New("не найден form csrf_token")
	}
	return fields, nil
}

func (f *FunPayClient) waitForRequest(minInterval time.Duration) {
	if !f.lastRequestAt.IsZero() {
		if wait := minInterval - time.Since(f.lastRequestAt); wait > 0 {
			time.Sleep(wait)
		}
	}
	f.lastRequestAt = time.Now()
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (f *FunPayClient) uploadImageLocked(path string) (int, error) {
	hash, err := fileSHA256(path)
	if err != nil {
		return 0, err
	}
	if id := f.imageCache[hash]; id > 0 {
		return id, nil
	}
	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		f.waitForRequest(900 * time.Millisecond)
		id, err := f.uploadImageOnceLocked(path)
		if err == nil {
			f.imageCache[hash] = id
			return id, nil
		}
		lastErr = err
		low := strings.ToLower(err.Error())
		retryable := strings.Contains(low, "http 429") || strings.Contains(low, "http 408") || strings.Contains(low, "http 500") || strings.Contains(low, "http 502") || strings.Contains(low, "http 503") || strings.Contains(low, "http 504")
		if !retryable || attempt == 5 {
			break
		}
		delay := time.Duration(1<<uint(attempt-1)) * 2 * time.Second
		time.Sleep(delay)
	}
	return 0, lastErr
}

func (f *FunPayClient) uploadImageOnceLocked(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	ct := "image/png"
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".jpg" || ext == ".jpeg" {
		ct = "image/jpeg"
	} else if ext == ".webp" {
		ct = "image/webp"
	} else if ext == ".gif" {
		ct = "image/gif"
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filepath.Base(path)))
	h.Set("Content-Type", ct)
	part, err := mw.CreatePart(h)
	if err != nil {
		return 0, err
	}
	_, err = io.Copy(part, file)
	if err != nil {
		return 0, err
	}
	_ = mw.WriteField("file_id", "0")
	mw.Close()
	req, _ := http.NewRequest("POST", "https://funpay.com/file/addOfferImage", &body)
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := f.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("upload HTTP %d: %s", resp.StatusCode, cut(string(b), 400))
	}
	var j struct {
		FileID any    `json:"fileId"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return 0, err
	}
	switch v := j.FileID.(type) {
	case float64:
		return int(v), nil
	case string:
		n, _ := strconv.Atoi(v)
		if n > 0 {
			return n, nil
		}
	}
	if j.Msg != "" {
		return 0, errors.New(j.Msg)
	}
	return 0, errors.New("FunPay не вернул fileId")
}

func (f *FunPayClient) ApplyPackage(p LotPackage, imagePaths []string) (string, error) {
	if p.LotID <= 0 {
		return f.PublishLot(p, imagePaths)
	}
	fields, err := f.GetLotFields(p.LotID)
	if err != nil {
		return "", err
	}
	if p.TitleRU != "" {
		fields["fields[summary][ru]"] = p.TitleRU
	}
	if p.TitleEN != "" {
		fields["fields[summary][en]"] = p.TitleEN
	}
	if p.DescriptionRU != "" {
		fields["fields[desc][ru]"] = p.DescriptionRU
	}
	if p.DescriptionEN != "" {
		fields["fields[desc][en]"] = p.DescriptionEN
	}
	if p.PaymentMsgRU != "" {
		fields["fields[payment_msg][ru]"] = p.PaymentMsgRU
	}
	if p.PaymentMsgEN != "" {
		fields["fields[payment_msg][en]"] = p.PaymentMsgEN
	}
	if p.Price > 0 {
		fields["price"] = trimFloat(p.Price)
	}
	if p.Active != nil {
		if *p.Active {
			fields["active"] = "on"
		} else {
			fields["active"] = ""
		}
	}
	if p.Amount != nil {
		fields["amount"] = strconv.Itoa(*p.Amount)
	}
	if p.DeactivateAfterSale != nil {
		if *p.DeactivateAfterSale {
			fields["deactivate_after_sale"] = "on"
		} else {
			fields["deactivate_after_sale"] = ""
		}
	}
	for k, v := range p.RawFields {
		fields[k] = v
	}
	if len(imagePaths) > 0 {
		ids, err := f.UploadImages(imagePaths)
		if err != nil {
			return "", err
		}
		fields["fields[images]"] = strings.Join(ids, ",")
	}
	return f.SaveLotFields(p.LotID, fields)
}

func (f *FunPayClient) GetCategories() ([]CategoryInfo, error) {
	if _, err := f.Init(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]CategoryInfo(nil), f.categories...)
	return out, nil
}

func (f *FunPayClient) ResolveNode(p LotPackage) (int, string, error) {
	if p.NodeID > 0 {
		cats, _ := f.GetCategories()
		for _, c := range cats {
			if c.ID == p.NodeID {
				return c.ID, c.Game + " > " + c.Name, nil
			}
		}
		return p.NodeID, fmt.Sprintf("node %d", p.NodeID), nil
	}
	if u := strings.TrimSpace(p.CategoryURL); u != "" {
		re := regexp.MustCompile(`(?i)/(?:lots|chips)/(\d+)/?`)
		m := re.FindStringSubmatch(u)
		if len(m) > 1 {
			id, _ := strconv.Atoi(m[1])
			if id > 0 {
				cats, _ := f.GetCategories()
				for _, c := range cats {
					if c.ID == id {
						return id, c.Game + " > " + c.Name, nil
					}
				}
				return id, fmt.Sprintf("node %d", id), nil
			}
		}
		return 0, "", errors.New("category_url не содержит ID раздела FunPay")
	}
	q := normalizeText(p.CategoryPath)
	if q == "" {
		return 0, "", errors.New("не указана категория")
	}
	cats, err := f.GetCategories()
	if err != nil {
		return 0, "", err
	}
	var exact, fuzzy []CategoryInfo
	for _, c := range cats {
		path := normalizeText(c.Game + " > " + c.Name)
		name := normalizeText(c.Name)
		if q == path || q == name {
			exact = append(exact, c)
		} else if strings.Contains(path, q) || strings.Contains(q, path) {
			fuzzy = append(fuzzy, c)
		} else {
			parts := strings.Fields(q)
			ok := true
			for _, part := range parts {
				if !strings.Contains(path, part) {
					ok = false
					break
				}
			}
			if ok {
				fuzzy = append(fuzzy, c)
			}
		}
	}
	pick := exact
	if len(pick) == 0 {
		pick = fuzzy
	}
	if len(pick) == 1 {
		c := pick[0]
		return c.ID, c.Game + " > " + c.Name, nil
	}
	if len(pick) > 1 {
		var names []string
		for i, c := range pick {
			if i >= 8 {
				break
			}
			names = append(names, fmt.Sprintf("%d — %s > %s", c.ID, c.Game, c.Name))
		}
		return 0, "", fmt.Errorf("категория неоднозначна. Варианты: %s", strings.Join(names, "; "))
	}
	return 0, "", fmt.Errorf("категория %q не найдена. Используй /categories часть_названия", p.CategoryPath)
}

func (f *FunPayClient) PreflightLot(p LotPackage) ([]string, bool, error) {
	if p.NodeID <= 0 {
		return nil, false, errors.New("node_id не определён")
	}
	fields, err := f.GetCreateLotFields(p.NodeID)
	if err != nil {
		return nil, false, fmt.Errorf("форма FunPay: %w", err)
	}
	var warnings []string
	for k := range p.RawFields {
		if _, ok := fields[k]; !ok {
			warnings = append(warnings, "⚠️ Поле "+k+" сейчас отсутствует в форме FunPay")
		}
	}
	// Проверяем, что основные поля формы существуют прямо сейчас.
	for _, k := range []string{"fields[summary][ru]", "fields[desc][ru]", "price"} {
		if _, ok := fields[k]; !ok {
			warnings = append(warnings, "⚠️ FunPay не вернул ожидаемое поле "+k)
		}
	}
	lots, err := f.GetMyLotsByNode(p.NodeID)
	if err != nil {
		// Предпросмотр не должен ломаться только из-за невозможности проверить дубликаты.
		warnings = append(warnings, "⚠️ Проверка дубликатов недоступна: "+cut(err.Error(), 100))
		return warnings, false, nil
	}
	target := normalizeText(p.TitleRU)
	if target == "" {
		target = normalizeText(p.TitleEN)
	}
	duplicate := false
	for _, lot := range lots {
		if target != "" && normalizeText(lot.Title) == target {
			duplicate = true
			break
		}
	}
	return warnings, duplicate, nil
}

func (f *FunPayClient) GetCreateLotFields(nodeID int) (map[string]string, error) {
	if _, err := f.Init(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.createFieldsCache[nodeID]; ok && time.Since(c.at) < 3*time.Minute {
		return cloneMap(c.fields), nil
	}
	fields, err := f.getLotFormLocked(nodeID)
	if err != nil {
		return nil, err
	}
	f.createFieldsCache[nodeID] = cachedCreateFields{at: time.Now(), fields: cloneMap(fields)}
	return cloneMap(fields), nil
}

func (f *FunPayClient) GetLotFields(lotID int) (map[string]string, error) {
	if lotID <= 0 {
		return nil, errors.New("lot id <= 0")
	}
	if _, err := f.Init(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	u := fmt.Sprintf("https://funpay.com/lots/offerEdit?offer=%d", lotID)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", f.userAgent)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("offerEdit HTTP %d", resp.StatusCode)
	}
	form := firstSubmatch(reOfferForm, string(b))
	if form == "" {
		lead := stripTags(firstSubmatch(reLead, string(b)))
		if lead != "" {
			return nil, errors.New(strings.TrimSpace(html.UnescapeString(lead)))
		}
		return nil, errors.New("форма лота не найдена")
	}
	fields := parseFormFields(form)
	return fields, nil
}

func (f *FunPayClient) SaveLotFields(lotID int, fields map[string]string) (string, error) {
	if _, err := f.Init(); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saveLotFieldsLocked(lotID, fields)
}

func (f *FunPayClient) saveLotFieldsLocked(lotID int, fields map[string]string) (string, error) {
	vals := url.Values{}
	for k, v := range fields {
		vals.Set(k, v)
	}
	vals.Set("offer_id", strconv.Itoa(lotID))
	if strings.TrimSpace(vals.Get("csrf_token")) == "" {
		return "", errors.New("форма FunPay не содержит csrf_token")
	}
	req, _ := http.NewRequest("POST", "https://funpay.com/lots/offerSave", strings.NewReader(vals.Encode()))
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	if f.pageCSRFToken != "" {
		req.Header.Set("X-CSRF-Token", f.pageCSRFToken)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("offerSave HTTP %d: %s", resp.StatusCode, cut(string(b), 600))
	}
	var j map[string]any
	if json.Unmarshal(b, &j) == nil {
		if e := extractFunPayError(j); e != "" {
			return "", errors.New(e)
		}
	}
	return strings.TrimSpace(string(b)), nil
}

func (f *FunPayClient) SetLotPrice(lotID int, price float64) error {
	fields, err := f.GetLotFields(lotID)
	if err != nil {
		return err
	}
	fields["price"] = trimFloat(price)
	_, err = f.SaveLotFields(lotID, fields)
	return err
}

func (f *FunPayClient) SetLotActive(lotID int, on bool) error {
	fields, err := f.GetLotFields(lotID)
	if err != nil {
		return err
	}
	if on {
		fields["active"] = "on"
	} else {
		fields["active"] = ""
	}
	_, err = f.SaveLotFields(lotID, fields)
	return err
}

func (f *FunPayClient) DeleteLot(lotID int) error {
	fields, err := f.GetLotFields(lotID)
	if err != nil {
		return err
	}
	fields["deleted"] = "1"
	_, err = f.SaveLotFields(lotID, fields)
	return err
}

func (f *FunPayClient) CloneLot(lotID int) (string, error) {
	fields, err := f.GetLotFields(lotID)
	if err != nil {
		return "", err
	}
	delete(fields, "deleted")
	fields["active"] = "on"
	return f.SaveLotFields(0, fields)
}

func (f *FunPayClient) RestoreLotFields(lotID int, fields map[string]string) error {
	_, err := f.SaveLotFields(lotID, cloneMap(fields))
	return err
}

func (f *FunPayClient) UploadImages(paths []string) ([]string, error) {
	if _, err := f.Init(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []string
	for _, path := range paths {
		id, err := f.uploadImageLocked(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		ids = append(ids, strconv.Itoa(id))
	}
	return ids, nil
}

func (f *FunPayClient) ReplaceLotImages(lotID int, paths []string) error {
	fields, err := f.GetLotFields(lotID)
	if err != nil {
		return err
	}
	ids, err := f.UploadImages(paths)
	if err != nil {
		return err
	}
	fields["fields[images]"] = strings.Join(ids, ",")
	_, err = f.SaveLotFields(lotID, fields)
	return err
}

func (f *FunPayClient) GetMyLots(knownNodes []int) ([]ExistingLot, error) {
	if _, err := f.Init(); err != nil {
		return nil, err
	}
	nodes := map[int]bool{}
	for _, n := range knownNodes {
		if n > 0 {
			nodes[n] = true
		}
	}
	profileNodes, err := f.getProfileNodes()
	if err == nil {
		for _, n := range profileNodes {
			nodes[n] = true
		}
	}
	if len(nodes) == 0 {
		return nil, nil
	}
	var ids []int
	for n := range nodes {
		ids = append(ids, n)
	}
	sort.Ints(ids)
	if len(ids) > 60 {
		ids = ids[:60]
	}
	seen := map[int]bool{}
	var result []ExistingLot
	for _, node := range ids {
		lots, err := f.GetMyLotsByNode(node)
		if err != nil {
			continue
		}
		for _, lot := range lots {
			if !seen[lot.ID] {
				seen[lot.ID] = true
				result = append(result, lot)
			}
		}
		time.Sleep(120 * time.Millisecond)
	}
	return result, nil
}

func (f *FunPayClient) getProfileNodes() ([]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.userID <= 0 {
		return nil, errors.New("FunPay user id неизвестен")
	}
	u := fmt.Sprintf("https://funpay.com/users/%d/", f.userID)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", f.userAgent)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("profile HTTP %d", resp.StatusCode)
	}
	re := regexp.MustCompile(`(?is)offer-list-title-container.{0,1000}?href\s*=\s*["'][^"']*/lots/(\d+)/`)
	ms := re.FindAllStringSubmatch(string(b), -1)
	set := map[int]bool{}
	for _, m := range ms {
		if len(m) > 1 {
			id, _ := strconv.Atoi(m[1])
			if id > 0 {
				set[id] = true
			}
		}
	}
	var nodes []int
	for n := range set {
		nodes = append(nodes, n)
	}
	sort.Ints(nodes)
	return nodes, nil
}

func (f *FunPayClient) GetMyLotsByNode(nodeID int) ([]ExistingLot, error) {
	if nodeID <= 0 {
		return nil, errors.New("node id <= 0")
	}
	if _, err := f.Init(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.nodeLotsCache[nodeID]; ok && time.Since(c.at) < 20*time.Second {
		return append([]ExistingLot(nil), c.lots...), nil
	}
	u := fmt.Sprintf("https://funpay.com/lots/%d/trade", nodeID)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", f.userAgent)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("trade HTTP %d", resp.StatusCode)
	}
	s := string(b)
	if !strings.Contains(s, "user-link-name") {
		return nil, errors.New("FunPay не подтвердил авторизацию")
	}
	var out []ExistingLot
	for _, m := range reTCItem.FindAllStringSubmatch(s, -1) {
		attrs := parseAttrs(m[1])
		body := m[2]
		id, _ := strconv.Atoi(attrs["data-offer"])
		if id <= 0 {
			href := attrs["href"]
			x := regexp.MustCompile(`id=(\d+)`).FindStringSubmatch(href)
			if len(x) > 1 {
				id, _ = strconv.Atoi(x[1])
			}
		}
		if id <= 0 {
			continue
		}
		title := html.UnescapeString(stripTags(firstSubmatch(reTCDesc, body)))
		price := 0.0
		pm := reTCPrice.FindStringSubmatch(body)
		currency := ""
		if len(pm) > 1 {
			pa := parseAttrs(pm[1])
			price, _ = strconv.ParseFloat(strings.ReplaceAll(pa["data-s"], " ", ""), 64)
			if len(pm) > 2 {
				currency = html.UnescapeString(strings.TrimSpace(stripTags(firstSubmatch(reUnit, pm[2]))))
			}
		}
		var amount *int
		am := firstSubmatch(reTCAmount, body)
		if am != "" {
			v, err := strconv.Atoi(strings.ReplaceAll(strings.TrimSpace(stripTags(am)), " ", ""))
			if err == nil {
				amount = &v
			}
		}
		class := strings.ToLower(attrs["class"])
		active := !strings.Contains(class, "warning")
		out = append(out, ExistingLot{ID: id, NodeID: nodeID, Title: title, Price: price, Amount: amount, Active: active, Currency: currency})
	}
	f.nodeLotsCache[nodeID] = cachedNodeLots{at: time.Now(), lots: append([]ExistingLot(nil), out...)}
	return append([]ExistingLot(nil), out...), nil
}

func (f *FunPayClient) GetRecentOrders() ([]OrderBrief, error) {
	if _, err := f.Init(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	req, _ := http.NewRequest("GET", "https://funpay.com/orders/trade", nil)
	req.Header.Set("User-Agent", f.userAgent)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("orders HTTP %d", resp.StatusCode)
	}
	s := string(b)
	if !strings.Contains(strings.ToLower(s), "tc-order") {
		return nil, nil
	}
	var result []OrderBrief
	for _, m := range reTCItem.FindAllStringSubmatch(s, -1) {
		attrs := parseAttrs(m[1])
		body := m[2]
		id := strings.TrimPrefix(strings.TrimSpace(stripTags(firstSubmatch(reTCOrder, body))), "#")
		if id == "" {
			continue
		}
		desc := strings.TrimSpace(stripTags(firstSubmatch(reOrderDescFirst, body)))
		priceText := strings.ReplaceAll(strings.TrimSpace(stripTags(firstSubmatch(reTCPriceText, body))), "\u00a0", " ")
		price, currency := 0.0, ""
		if priceText != "" {
			parts := strings.Fields(priceText)
			if len(parts) >= 2 {
				currency = parts[len(parts)-1]
				v := strings.Join(parts[:len(parts)-1], "")
				price, _ = strconv.ParseFloat(strings.ReplaceAll(v, ",", "."), 64)
			}
		}
		dateText := strings.TrimSpace(stripTags(firstSubmatch(reTCDateTime, body)))
		buyer := strings.TrimSpace(stripTags(firstSubmatch(reTCUserName, body)))
		class := strings.ToLower(attrs["class"])
		status := "closed"
		if strings.Contains(class, "warning") {
			status = "refunded"
		} else if strings.Contains(class, "info") {
			status = "paid"
		}
		result = append(result, OrderBrief{ID: id, Description: desc, Price: price, Currency: currency, Status: status, DateText: dateText, Buyer: buyer})
	}
	return result, nil
}

func parseCategoriesHTML(s string) []CategoryInfo {
	starts := reGameItemStart.FindAllStringIndex(s, -1)
	if len(starts) == 0 {
		return nil
	}
	var out []CategoryInfo
	seen := map[int]bool{}
	for i, ix := range starts {
		end := len(s)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		block := s[ix[0]:end]
		game := ""
		if gm := reGameTitleBlock.FindStringSubmatch(block); len(gm) > 1 {
			game = html.UnescapeString(strings.TrimSpace(stripTags(gm[1])))
		}
		if game == "" {
			if am := reAnchorText.FindStringSubmatch(block); len(am) > 1 {
				game = html.UnescapeString(strings.TrimSpace(stripTags(am[1])))
			}
		}
		for _, m := range reCategoryAnchor.FindAllStringSubmatch(block, -1) {
			if len(m) < 4 || strings.ToLower(m[1]) != "lots" {
				continue
			}
			id, _ := strconv.Atoi(m[2])
			if id <= 0 || seen[id] {
				continue
			}
			name := html.UnescapeString(strings.TrimSpace(stripTags(m[3])))
			if name == "" {
				continue
			}
			seen[id] = true
			out = append(out, CategoryInfo{ID: id, Game: game, Name: name, URL: fmt.Sprintf("https://funpay.com/lots/%d/", id)})
		}
	}
	return out
}

var (
	reAppData        = regexp.MustCompile(`(?is)data-app-data\s*=\s*["']([^"']+)["']`)
	reUsername       = regexp.MustCompile(`(?is)<div[^>]*class\s*=\s*["'][^"']*user-link-name[^"']*["'][^>]*>(.*?)</div>`)
	reOfferForm      = regexp.MustCompile(`(?is)<form[^>]*class\s*=\s*["'][^"']*form-offer-editor[^"']*["'][^>]*>(.*?)</form>`)
	reLead           = regexp.MustCompile(`(?is)<p[^>]*class\s*=\s*["'][^"']*lead[^"']*["'][^>]*>(.*?)</p>`)
	reInput          = regexp.MustCompile(`(?is)<input\b([^>]*)>`)
	reTextarea       = regexp.MustCompile(`(?is)<textarea\b([^>]*)>(.*?)</textarea>`)
	reSelect         = regexp.MustCompile(`(?is)<select\b([^>]*)>(.*?)</select>`)
	reOption         = regexp.MustCompile(`(?is)<option\b([^>]*)>(.*?)</option>`)
	reAttr           = regexp.MustCompile(`(?is)([A-Za-z0-9_:\-\[\]]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	reTag            = regexp.MustCompile(`(?is)<[^>]+>`)
	reTCItem         = regexp.MustCompile(`(?is)<a\b([^>]*\bclass\s*=\s*["'][^"']*tc-item[^"']*["'][^>]*)>(.*?)</a>`)
	reTCDesc         = regexp.MustCompile(`(?is)<div[^>]*class\s*=\s*["'][^"']*tc-desc-text[^"']*["'][^>]*>(.*?)</div>`)
	reTCPrice        = regexp.MustCompile(`(?is)<div\b([^>]*class\s*=\s*["'][^"']*tc-price[^"']*["'][^>]*)>(.*?)</div>`)
	reUnit           = regexp.MustCompile(`(?is)<span[^>]*class\s*=\s*["'][^"']*unit[^"']*["'][^>]*>(.*?)</span>`)
	reTCAmount       = regexp.MustCompile(`(?is)<div[^>]*class\s*=\s*["'][^"']*tc-amount[^"']*["'][^>]*>(.*?)</div>`)
	reTCOrder        = regexp.MustCompile(`(?is)<div[^>]*class\s*=\s*["'][^"']*tc-order[^"']*["'][^>]*>(.*?)</div>`)
	reOrderDescFirst = regexp.MustCompile(`(?is)<div[^>]*class\s*=\s*["'][^"']*order-desc[^"']*["'][^>]*>\s*<div[^>]*>(.*?)</div>`)
	reTCPriceText    = regexp.MustCompile(`(?is)<div[^>]*class\s*=\s*["'][^"']*tc-price[^"']*["'][^>]*>(.*?)</div>`)
	reTCDateTime     = regexp.MustCompile(`(?is)<div[^>]*class\s*=\s*["'][^"']*tc-date-time[^"']*["'][^>]*>(.*?)</div>`)
	reTCUserName     = regexp.MustCompile(`(?is)<div[^>]*class\s*=\s*["'][^"']*media-user-name[^"']*["'][^>]*>(.*?)</div>`)
	reGameItemStart  = regexp.MustCompile(`(?is)<div[^>]*class\s*=\s*["'][^"']*promo-game-item[^"']*["'][^>]*>`)
	reGameTitleBlock = regexp.MustCompile(`(?is)<div[^>]*class\s*=\s*["'][^"']*game-title[^"']*["'][^>]*>(.*?)</div>`)
	reAnchorText     = regexp.MustCompile(`(?is)<a\b[^>]*>(.*?)</a>`)
	reCategoryAnchor = regexp.MustCompile(`(?is)<a\b[^>]*href\s*=\s*["'][^"']*/(lots|chips)/(\d+)/?[^"']*["'][^>]*>(.*?)</a>`)
)

func parseFormFields(form string) map[string]string {
	out := map[string]string{}
	for _, m := range reInput.FindAllStringSubmatch(form, -1) {
		attrs := parseAttrs(m[1])
		name := attrs["name"]
		if name == "" {
			continue
		}
		typ := strings.ToLower(attrs["type"])
		val := attrs["value"]
		if typ == "checkbox" {
			if hasBoolAttr(m[1], "checked") {
				val = "on"
			}
		}
		out[name] = html.UnescapeString(val)
	}
	for _, m := range reTextarea.FindAllStringSubmatch(form, -1) {
		attrs := parseAttrs(m[1])
		if name := attrs["name"]; name != "" {
			out[name] = html.UnescapeString(stripTags(m[2]))
		}
	}
	for _, m := range reSelect.FindAllStringSubmatch(form, -1) {
		attrs := parseAttrs(m[1])
		name := attrs["name"]
		if name == "" {
			continue
		}
		selected := ""
		first := ""
		for _, o := range reOption.FindAllStringSubmatch(m[2], -1) {
			oa := parseAttrs(o[1])
			v := oa["value"]
			if first == "" {
				first = v
			}
			if hasBoolAttr(o[1], "selected") {
				selected = v
				break
			}
		}
		if selected == "" {
			selected = first
		}
		out[name] = html.UnescapeString(selected)
	}
	return out
}

func parseAttrs(s string) map[string]string {
	m := map[string]string{}
	for _, x := range reAttr.FindAllStringSubmatch(s, -1) {
		v := x[2]
		if v == "" {
			v = x[3]
		}
		if v == "" {
			v = x[4]
		}
		m[strings.ToLower(x[1])] = v
	}
	return m
}
func hasBoolAttr(s, name string) bool {
	re := regexp.MustCompile(`(?i)(?:^|\s)` + regexp.QuoteMeta(name) + `(?:\s|=|$)`)
	return re.FindStringIndex(s) != nil
}
func firstSubmatch(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}
func stripTags(s string) string { return strings.TrimSpace(reTag.ReplaceAllString(s, "")) }
func extractFunPayError(j map[string]any) string {
	if e, ok := j["error"].(string); ok && e != "" {
		return e
	}
	if es, ok := j["errors"].([]any); ok && len(es) > 0 {
		var parts []string
		for _, it := range es {
			if a, ok := it.([]any); ok && len(a) >= 2 {
				parts = append(parts, fmt.Sprintf("%v: %v", a[0], a[1]))
			} else {
				parts = append(parts, fmt.Sprint(it))
			}
		}
		return strings.Join(parts, "; ")
	}
	return ""
}
func trimFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}
func randomDigits(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	var sb strings.Builder
	for _, x := range b {
		sb.WriteByte('0' + x%10)
	}
	return sb.String()
}
func hasArg(s string) bool {
	for _, a := range os.Args[1:] {
		if a == s {
			return true
		}
	}
	return false
}
func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
func escapeTG(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
