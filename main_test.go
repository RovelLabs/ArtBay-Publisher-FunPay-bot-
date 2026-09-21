package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseFormFields(t *testing.T) {
	form := `<input type="hidden" name="csrf_token" value="abc"><input name="price" value="123"><input type="checkbox" name="active" checked><textarea name="fields[desc][ru]">hello &amp; bye</textarea><select name="server"><option value="1">A</option><option value="2" selected>B</option></select>`
	f := parseFormFields(form)
	if f["csrf_token"] != "abc" || f["price"] != "123" || f["active"] != "on" || f["server"] != "2" || f["fields[desc][ru]"] != "hello & bye" {
		t.Fatalf("unexpected fields: %#v", f)
	}
}

func TestStageLot(t *testing.T) {
	base := t.TempDir()
	cfg := filepath.Join(base, "cfg.json")
	logp := filepath.Join(base, "app.log")
	lf, _ := os.Create(logp)
	a := &App{cfgPath: cfg, dataDir: base, logger: nil, logPath: logp, staged: map[string]*StagedLot{}}
	_ = lf.Close()
	zp := filepath.Join(base, "lot.zip")
	zf, _ := os.Create(zp)
	zw := zip.NewWriter(zf)
	w, _ := zw.Create("lot.json")
	_, _ = w.Write([]byte(`{"version":1,"node_id":123,"title_ru":"Test","price":99}`))
	w, _ = zw.Create("cover.png")
	_, _ = w.Write([]byte("fakepng"))
	_ = zw.Close()
	_ = zf.Close()
	st, err := a.stageLot(zp)
	if err != nil {
		t.Fatal(err)
	}
	if st.Package.NodeID != 123 || len(st.ImagePaths) != 1 {
		t.Fatalf("bad stage: %#v", st)
	}
}

func TestCalculatePriceChange(t *testing.T) {
	if v, ok := calculatePriceChange(200, "-15%"); !ok || v != 170 {
		t.Fatalf("unexpected percent result: %v %v", v, ok)
	}
	if v, ok := calculatePriceChange(200, "=149"); !ok || v != 149 {
		t.Fatalf("unexpected fixed result: %v %v", v, ok)
	}
}

func TestParseCategoriesHTML(t *testing.T) {
	html := `<div class="promo-game-item"><div class="game-title" data-id="1"><a>Roblox Studio</a></div><ul class="list-inline" data-id="1"><li><a href="/lots/1858/">Услуги</a></li></ul></div>`
	cats := parseCategoriesHTML(html)
	if len(cats) != 1 || cats[0].ID != 1858 || cats[0].Game != "Roblox Studio" || cats[0].Name != "Услуги" {
		t.Fatalf("unexpected categories: %#v", cats)
	}
}

func TestStageLotWithCategoryPath(t *testing.T) {
	base := t.TempDir()
	a := &App{dataDir: base, staged: map[string]*StagedLot{}, batches: map[string]*BatchStage{}, actions: map[string]*PendingAction{}}
	zp := filepath.Join(base, "lot2.zip")
	zf, _ := os.Create(zp)
	zw := zip.NewWriter(zf)
	w, _ := zw.Create("lot.json")
	_, _ = w.Write([]byte(`{"version":2,"category_path":"Roblox Studio > Услуги","title_ru":"Test","price":199}`))
	w, _ = zw.Create("cover.png")
	_, _ = w.Write([]byte("fakepng"))
	_ = zw.Close()
	_ = zf.Close()
	st, err := a.stageLot(zp)
	if err != nil {
		t.Fatal(err)
	}
	if st.Package.CategoryPath == "" || st.Package.NodeID != 0 {
		t.Fatalf("bad stage: %#v", st.Package)
	}
}

func makeLotZipBytes(t *testing.T, title string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("lot.json")
	_, _ = w.Write([]byte(`{"version":2,"node_id":123,"title_ru":"` + title + `","price":99}`))
	w, _ = zw.Create("cover.png")
	_, _ = w.Write([]byte("img"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestStageIncomingNestedBatch(t *testing.T) {
	base := t.TempDir()
	a := &App{dataDir: base, staged: map[string]*StagedLot{}, batches: map[string]*BatchStage{}, actions: map[string]*PendingAction{}}
	outerPath := filepath.Join(base, "batch.zip")
	f, _ := os.Create(outerPath)
	zw := zip.NewWriter(f)
	for i, title := range []string{"One", "Two"} {
		w, _ := zw.Create(fmt.Sprintf("%02d.zip", i+1))
		_, _ = w.Write(makeLotZipBytes(t, title))
	}
	_ = zw.Close()
	_ = f.Close()
	lots, act, err := a.stageIncomingZip(outerPath)
	if err != nil {
		t.Fatal(err)
	}
	if act != nil || len(lots) != 2 {
		t.Fatalf("unexpected batch: lots=%d act=%#v", len(lots), act)
	}
}

func TestStageIncomingImagePack(t *testing.T) {
	base := t.TempDir()
	a := &App{dataDir: base, staged: map[string]*StagedLot{}, batches: map[string]*BatchStage{}, actions: map[string]*PendingAction{}}
	outerPath := filepath.Join(base, "images.zip")
	f, _ := os.Create(outerPath)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("12345.png")
	_, _ = w.Write([]byte("img"))
	w, _ = zw.Create("lot_67890.jpg")
	_, _ = w.Write([]byte("img"))
	_ = zw.Close()
	_ = f.Close()
	lots, act, err := a.stageIncomingZip(outerPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(lots) != 0 || act == nil || len(act.ImagePaths) != 2 {
		t.Fatalf("unexpected image action: lots=%d act=%#v", len(lots), act)
	}
	a.actions[act.Token] = act
	a.removeAction(act.Token)
}

func TestAutoPublishBatchThreshold(t *testing.T) {
	if shouldAutoPublishBatch(10) {
		t.Fatal("10 lots must still require confirmation")
	}
	if !shouldAutoPublishBatch(11) {
		t.Fatal("11 lots must auto-publish")
	}
	if !shouldAutoPublishBatch(100) {
		t.Fatal("100 lots must auto-publish")
	}
}

func TestStageHasFatalWarning(t *testing.T) {
	if stageHasFatalWarning(&StagedLot{Warnings: []string{"⚠️ warning"}}) {
		t.Fatal("warning must not be fatal")
	}
	if !stageHasFatalWarning(&StagedLot{Warnings: []string{"📂 category", "❌ bad form"}}) {
		t.Fatal("fatal marker must be detected")
	}
}

func TestBulkPrepareWithNodeIDNeedsNoFunPayClient(t *testing.T) {
	base := t.TempDir()
	a := &App{cfgPath: filepath.Join(base, "cfg.json"), dataDir: base}
	lots := []*StagedLot{
		{Package: LotPackage{NodeID: 1858, TitleRU: "Lot A", Price: 199}},
		{Package: LotPackage{NodeID: 1858, TitleRU: "Lot B", Price: 209}},
	}
	// fp intentionally nil: with explicit node_id, V3.4 bulk preparation must be local-only.
	a.prepareBulkStagedLots(lots)
	if stageHasFatalWarning(lots[0]) || stageHasFatalWarning(lots[1]) {
		t.Fatalf("unexpected fatal warnings: %#v %#v", lots[0].Warnings, lots[1].Warnings)
	}
	if len(a.cfg.KnownNodes) != 1 || a.cfg.KnownNodes[0] != 1858 {
		t.Fatalf("node was not remembered: %#v", a.cfg.KnownNodes)
	}
}

func TestBulkPrepareSkipsDuplicateTitlesInsidePackage(t *testing.T) {
	base := t.TempDir()
	a := &App{cfgPath: filepath.Join(base, "cfg.json"), dataDir: base}
	lots := []*StagedLot{
		{Package: LotPackage{NodeID: 1858, TitleRU: "Same Lot", Price: 199}},
		{Package: LotPackage{NodeID: 1858, TitleRU: "Same Lot", Price: 199}},
	}
	a.prepareBulkStagedLots(lots)
	if lots[0].Duplicate || !lots[1].Duplicate {
		t.Fatalf("duplicate detection failed: first=%v second=%v", lots[0].Duplicate, lots[1].Duplicate)
	}
}

func TestClassifyBulkSkipError(t *testing.T) {
	cases := []struct {
		msg       string
		kind      string
		skip      bool
		blockNode bool
	}{
		{"Много предложений в разделе «Studio», удалите ненужные (1).", "category_full", true, true},
		{"Такое предложение уже существует", "duplicate", true, false},
		{"duplicate listing", "duplicate", true, false},
		{"offerSave HTTP 500", "", false, false},
	}
	for _, tc := range cases {
		kind, skip, blockNode := classifyBulkSkipError(errors.New(tc.msg))
		if kind != tc.kind || skip != tc.skip || blockNode != tc.blockNode {
			t.Fatalf("%q => kind=%q skip=%v block=%v", tc.msg, kind, skip, blockNode)
		}
	}
}

func TestStageIncomingLotsManifest(t *testing.T) {
	base := t.TempDir()
	a := &App{dataDir: base, staged: map[string]*StagedLot{}, batches: map[string]*BatchStage{}, actions: map[string]*PendingAction{}}
	zp := filepath.Join(base, "manifest.zip")
	f, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("lots.json")
	_, _ = w.Write([]byte(`{"version":1,"lots":[{"version":2,"node_id":100,"title_ru":"One","price":100,"image_files":["one.webp"]},{"version":2,"node_id":101,"title_ru":"Two","price":200,"image_files":["two.png"]}]}`))
	w, _ = zw.Create("one.webp")
	_, _ = w.Write([]byte("webp"))
	w, _ = zw.Create("two.png")
	_, _ = w.Write([]byte("png"))
	_ = zw.Close()
	_ = f.Close()
	lots, action, err := a.stageIncomingZip(zp)
	if err != nil {
		t.Fatal(err)
	}
	if action != nil || len(lots) != 2 {
		t.Fatalf("unexpected manifest result: lots=%d action=%#v", len(lots), action)
	}
	if lots[0].Package.TitleRU != "One" || filepath.Ext(lots[0].ImagePaths[0]) != ".webp" {
		t.Fatalf("unexpected first lot: %#v", lots[0])
	}
	a.removeBatchData(&BatchStage{Lots: lots})
}

func TestStageIncomingFiveThousandManifest(t *testing.T) {
	base := t.TempDir()
	a := &App{dataDir: base, staged: map[string]*StagedLot{}, batches: map[string]*BatchStage{}, actions: map[string]*PendingAction{}}
	packages := make([]LotPackage, 5000)
	for i := range packages {
		packages[i] = LotPackage{Version: 2, NodeID: 100 + i%20, TitleRU: fmt.Sprintf("Lot %04d", i+1), Price: 100, ImageFiles: []string{"shared.webp"}}
	}
	manifest, err := json.Marshal(map[string]any{"version": 1, "lots": packages})
	if err != nil {
		t.Fatal(err)
	}
	zp := filepath.Join(base, "five-thousand.zip")
	f, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("lots.json")
	_, _ = w.Write(manifest)
	w, _ = zw.Create("shared.webp")
	_, _ = w.Write([]byte("shared image"))
	_ = zw.Close()
	_ = f.Close()
	lots, action, err := a.stageIncomingZip(zp)
	if err != nil {
		t.Fatal(err)
	}
	if action != nil || len(lots) != 5000 {
		t.Fatalf("unexpected large manifest result: lots=%d action=%#v", len(lots), action)
	}
	a.removeBatchData(&BatchStage{Lots: lots})
}

func TestStagePackageRejectsEscapingImagePath(t *testing.T) {
	_, err := stagePackageFromDir(LotPackage{NodeID: 1, TitleRU: "Unsafe", Price: 1, ImageFiles: []string{"../secret.png"}}, t.TempDir(), "")
	if err == nil || !strings.Contains(err.Error(), "опасный путь") {
		t.Fatalf("expected unsafe path error, got %v", err)
	}
}

func TestSanitizeSecretError(t *testing.T) {
	secret := "123:very-secret"
	err := sanitizeSecretError(fmt.Errorf("POST https://api.telegram.org/bot%s/getUpdates failed", secret), secret)
	if strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "[secret]") {
		t.Fatalf("secret was not redacted: %v", err)
	}
}

func TestTemporaryCapacityError(t *testing.T) {
	for _, msg := range []string{"upload HTTP 429", "Превышено максимальное количество загрузок", "rate limit exceeded"} {
		if !isTemporaryCapacityError(errors.New(msg)) {
			t.Fatalf("must pause for %q", msg)
		}
	}
	if isTemporaryCapacityError(errors.New("offerSave HTTP 400: invalid title")) {
		t.Fatal("ordinary validation error must not pause the whole queue")
	}
}

func TestBulkSkipSummary(t *testing.T) {
	b := &BatchStage{SkippedCount: 19, LimitSkippedCount: 15, DuplicateSkippedCount: 2}
	got := bulkSkipSummary(b)
	for _, want := range []string{"лимит раздела 15", "дубли 2", "ранее/прочее 2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary %q does not contain %q", got, want)
		}
	}
}

func TestBulkCheckpointRecovery(t *testing.T) {
	base := t.TempDir()
	manifest := filepath.Join(base, "bulk-job.json")
	progress := filepath.Join(base, "bulk-progress.json")
	batch := &BatchStage{Token: "job1", Lots: []*StagedLot{{Package: LotPackage{NodeID: 1, TitleRU: "One", Price: 10}}}, Selected: map[int]bool{0: true}, BlockedNodes: map[int]bool{42: true}, CreatedAt: time.Now(), NextIndex: 1, OKCount: 1, SkippedCount: 1, LimitSkippedCount: 1, Status: "paused"}
	a := &App{bulkJobPath: manifest, bulkProgressPath: progress, batches: map[string]*BatchStage{}, logger: nil}
	if err := a.saveBulkManifest(batch); err != nil {
		t.Fatal(err)
	}
	batch.NextIndex = 2
	batch.OKCount = 2
	if err := a.saveBulkProgress(batch); err != nil {
		t.Fatal(err)
	}
	batches := map[string]*BatchStage{}
	b := &App{bulkJobPath: manifest, bulkProgressPath: progress, batches: batches, logger: testLogger(t)}
	b.loadBulkJob()
	restored := b.batches["job1"]
	if restored == nil || restored.NextIndex != 2 || restored.OKCount != 2 || restored.Status != "paused" || restored.LimitSkippedCount != 1 || !restored.BlockedNodes[42] {
		t.Fatalf("unexpected restored batch: %#v", restored)
	}
}

func TestDiscardBulkRemovesPausedJobAndCheckpoint(t *testing.T) {
	base := t.TempDir()
	manifest := filepath.Join(base, "bulk-job.json")
	progress := filepath.Join(base, "bulk-progress.json")
	lotDir := filepath.Join(base, "lot-stage")
	if err := os.MkdirAll(lotDir, 0700); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(base, "upload.zip")
	if err := os.WriteFile(zipPath, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	batch := &BatchStage{Token: "job1", Lots: []*StagedLot{{TempDir: lotDir, ZipPath: zipPath}}, Selected: map[int]bool{0: true}, Status: "paused"}
	a := &App{bulkJobPath: manifest, bulkProgressPath: progress, batches: map[string]*BatchStage{"job1": batch}}
	if err := a.saveBulkManifest(batch); err != nil {
		t.Fatal(err)
	}
	if got := a.discardBulk(); got != "discarded" {
		t.Fatalf("discardBulk() = %q, want discarded", got)
	}
	for _, path := range []string{manifest, progress, lotDir, zipPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after discard", path)
		}
	}
	if len(a.batches) != 0 {
		t.Fatalf("paused batch still registered: %#v", a.batches)
	}
}

func TestDiscardBulkMarksRunningJobForPermanentCancellation(t *testing.T) {
	a := &App{batches: map[string]*BatchStage{}, bulkRunning: true, bulkActiveToken: "job1"}
	if got := a.discardBulk(); got != "stopping" {
		t.Fatalf("discardBulk() = %q, want stopping", got)
	}
	if !a.bulkCancel || !a.bulkDiscard {
		t.Fatalf("running job was not marked for discard: cancel=%v discard=%v", a.bulkCancel, a.bulkDiscard)
	}
}

func testLogger(t *testing.T) *log.Logger {
	t.Helper()
	return log.New(io.Discard, "", 0)
}

func TestDashboardDoesNotRenderSecrets(t *testing.T) {
	secretToken, secretKey := "telegram-secret", "funpay-secret"
	a := &App{cfg: Config{DashboardKey: "dashboard", TelegramToken: secretToken, TelegramBot: "sample_bot", GoldenKey: secretKey, UserAgent: "UA"}, logger: testLogger(t)}
	req := httptest.NewRequest("GET", "/?k=dashboard", nil)
	w := httptest.NewRecorder()
	a.handleDashboard(w, req)
	if w.Code != 200 {
		t.Fatalf("unexpected status: %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, secretToken) || strings.Contains(body, secretKey) {
		t.Fatal("dashboard rendered a stored secret")
	}
	if !strings.Contains(body, "@sample_bot") || !strings.Contains(body, "Подключи один раз") {
		t.Fatalf("dashboard missing expected content: %s", body)
	}
}
