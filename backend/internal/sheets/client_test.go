package sheets

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"google.golang.org/api/option"
)

// fakeSheets adalah server tiruan Google Sheets API untuk menguji alur tulis.
//
// Menyimpan baris dalam slice [][]string agar dapat diverifikasi.
type fakeSheets struct {
	mu     sync.Mutex
	tabs   []string
	rows   [][]string
	server *httptest.Server
	calls  []string
}

func newFakeSheets() *fakeSheets {
	f := &fakeSheets{tabs: []string{"Sheet1"}}
	mux := http.NewServeMux()

	// Dapatkan spreadsheet (untuk SheetExists).
	mux.HandleFunc("/v4/spreadsheets/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.Contains(path, "/values/") && r.Method == http.MethodGet:
			f.handleGet(w, r)
		case strings.Contains(path, "/values/") && r.Method == http.MethodPut:
			f.handleUpdate(w, r)
		case strings.HasSuffix(path, ":append") && r.Method == http.MethodPost:
			f.handleAppend(w, r)
		case strings.HasSuffix(path, ":batchUpdate") && r.Method == http.MethodPost:
			f.handleBatchUpdate(w, r)
		default:
			if r.Method == http.MethodGet {
				f.handleSpreadsheetGet(w)
				return
			}
			http.Error(w, "not found", http.StatusNotFound)
		}
	})

	f.server = httptest.NewServer(mux)
	return f
}

func (f *fakeSheets) Close() { f.server.Close() }

func (f *fakeSheets) handleSpreadsheetGet(w http.ResponseWriter) {
	f.mu.Lock()
	defer f.mu.Unlock()
	type props struct {
		Title string `json:"title"`
	}
	type sheet struct {
		Properties props `json:"properties"`
	}
	out := struct {
		Sheets []sheet `json:"sheets"`
	}{}
	for _, t := range f.tabs {
		out.Sheets = append(out.Sheets, sheet{Properties: props{Title: t}})
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (f *fakeSheets) handleBatchUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Requests []struct {
			AddSheet *struct {
				Properties struct{ Title string } `json:"properties"`
			} `json:"addSheet"`
		} `json:"requests"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, req := range body.Requests {
		if req.AddSheet != nil && req.AddSheet.Properties.Title != "" {
			f.tabs = append(f.tabs, req.AddSheet.Properties.Title)
			f.calls = append(f.calls, "addSheet:"+req.AddSheet.Properties.Title)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{}`))
}

func (f *fakeSheets) handleGet(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "get")
	type vr struct {
		Values [][]any `json:"values"`
	}
	out := vr{}
	// Hormati baris awal pada range (mis. A2:A -> mulai dari baris 2).
	start := parseRowIndex(r.URL.Path)
	from := 0
	if start > 1 {
		from = start - 1
	}
	for i := from; i < len(f.rows); i++ {
		row := f.rows[i]
		conv := make([]any, len(row))
		for j, c := range row {
			conv[j] = c
		}
		out.Values = append(out.Values, conv)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (f *fakeSheets) handleAppend(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Values [][]any `json:"values"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "append")
	for _, row := range body.Values {
		f.rows = append(f.rows, toStrings(row))
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{}`))
}

func (f *fakeSheets) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Values [][]any `json:"values"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	// Range ada di URL (...,/values/'Sheet'!A2:L2).
	rowIdx := parseRowIndex(r.URL.Path)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf("update:%d", rowIdx))
	if len(body.Values) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
		return
	}
	row := toStrings(body.Values[0])
	target := rowIdx - 1 // index 0-based
	for len(f.rows) <= target {
		f.rows = append(f.rows, nil)
	}
	f.rows[target] = row
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{}`))
}

func (f *fakeSheets) snapshot() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]string, len(f.rows))
	for i, r := range f.rows {
		out[i] = append([]string{}, r...)
	}
	return out
}

func toStrings(row []any) []string {
	out := make([]string, len(row))
	for i, v := range row {
		out[i] = fmt.Sprint(v)
	}
	return out
}

func parseRowIndex(path string) int {
	// Ambil angka setelah huruf kolom pertama pada bagian range (mis. A2:L2 -> 2).
	idx := strings.LastIndex(path, "!")
	if idx < 0 {
		return 0
	}
	rg := path[idx+1:]
	i := 0
	for i < len(rg) && (rg[i] < '0' || rg[i] > '9') {
		i++
	}
	n := 0
	for i < len(rg) && rg[i] >= '0' && rg[i] <= '9' {
		n = n*10 + int(rg[i]-'0')
		i++
	}
	return n
}

// clientFor mengembalikan Client yang diarahkan ke server tiruan.
func clientFor(t *testing.T, f *fakeSheets, tab string) *Client {
	t.Helper()
	c, err := newClient(context.Background(), `{}`, "SHEET_ID", tab,
		[]option.ClientOption{
			option.WithEndpoint(f.server.URL),
			option.WithHTTPClient(f.server.Client()),
			option.WithoutAuthentication(),
		})
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	return c
}

func TestClientCreatesTabAndHeader(t *testing.T) {
	f := newFakeSheets()
	defer f.Close()
	c := clientFor(t, f, "Todo")

	if err := c.CreateSheetTab(context.Background()); err != nil {
		t.Fatalf("CreateSheetTab: %v", err)
	}
	if err := c.EnsureHeader(context.Background()); err != nil {
		t.Fatalf("EnsureHeader: %v", err)
	}

	rows := f.snapshot()
	if len(rows) != 1 {
		t.Fatalf("harus ada 1 baris header, got %d", len(rows))
	}
	if rows[0][0] != "Ref" || rows[0][5] != "Status" {
		t.Errorf("header tidak sesuai: %v", rows[0])
	}

	// EnsureHeader kedua kali tidak boleh menulis ulang.
	if err := c.EnsureHeader(context.Background()); err != nil {
		t.Fatalf("EnsureHeader kedua: %v", err)
	}
	if got := len(f.snapshot()); got != 1 {
		t.Errorf("EnsureHeader menulis ulang header, rows=%d", got)
	}
}

func TestClientAppendFindUpdate(t *testing.T) {
	f := newFakeSheets()
	defer f.Close()
	c := clientFor(t, f, "Todo")
	ctx := context.Background()

	if err := c.CreateSheetTab(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.EnsureHeader(ctx); err != nil {
		t.Fatal(err)
	}

	row := RowFromPayload(map[string]any{
		"ref_no": "TSK-2026-0001", "title": "Cek BGP", "status": "accepted",
	})
	if err := c.AppendRow(ctx, row); err != nil {
		t.Fatalf("AppendRow: %v", err)
	}

	idx, err := c.FindRow(ctx, "TSK-2026-0001")
	if err != nil {
		t.Fatalf("FindRow: %v", err)
	}
	if idx != 2 {
		t.Fatalf("FindRow = %d, want 2 (baris 1 = header)", idx)
	}

	// Baris tidak dikenal -> 0.
	if idx, _ := c.FindRow(ctx, "TSK-TIDAK-ADA"); idx != 0 {
		t.Errorf("FindRow item tak dikenal = %d, want 0", idx)
	}

	// Update status pada baris yang sama.
	row[5] = "On Progress"
	if err := c.UpdateRow(ctx, idx, row); err != nil {
		t.Fatalf("UpdateRow: %v", err)
	}

	rows := f.snapshot()
	if len(rows) != 2 {
		t.Fatalf("jumlah baris = %d, want 2 (header + 1 data)", len(rows))
	}
	if rows[1][5] != "On Progress" {
		t.Errorf("status tidak diperbarui: %v", rows[1][5])
	}
}

func TestClientSheetExists(t *testing.T) {
	f := newFakeSheets()
	defer f.Close()

	c := clientFor(t, f, "Todo")
	ok, err := c.SheetExists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("tab Todo belum dibuat, SheetExists harus false")
	}

	c2 := clientFor(t, f, "Sheet1")
	ok, err = c2.SheetExists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("tab Sheet1 ada, SheetExists harus true")
	}
}

func TestWrapAPIError(t *testing.T) {
	cases := map[string]string{
		"403": "akses ditolak",
		"404": "tidak ditemukan",
		"429": "kuota",
	}
	for code, want := range cases {
		err := wrapAPIError("uji", fmt.Errorf("googleapi: Error %s: nope", code))
		if !strings.Contains(err.Error(), want) {
			t.Errorf("wrapAPIError(%s) = %v, want memuat %q", code, err, want)
		}
	}
}

func TestEnsureHeaderRewritesStaleHeader(t *testing.T) {
	f := newFakeSheets()
	defer f.Close()
	c := clientFor(t, f, "Todo")
	ctx := context.Background()

	if err := c.CreateSheetTab(ctx); err != nil {
		t.Fatal(err)
	}
	// Tanam header lama (12 kolom, tanpa "Tipe") seperti pada spreadsheet lama.
	f.mu.Lock()
	f.rows = [][]string{{"Ref", "Judul", "Deskripsi", "Prioritas", "Status", "Owner",
		"Dibuat Oleh", "Device", "Tags", "Due (WIB)", "Dibuat (WIB)", "Diperbarui (WIB)"}}
	f.mu.Unlock()

	if err := c.EnsureHeader(ctx); err != nil {
		t.Fatalf("EnsureHeader: %v", err)
	}

	rows := f.snapshot()
	if len(rows[0]) != len(HeaderRow) {
		t.Fatalf("header tidak ditulis ulang: %v", rows[0])
	}
	if rows[0][1] != "Tipe" {
		t.Errorf("kolom B harus 'Tipe', got %v", rows[0][1])
	}
}

func TestEnsureHeaderIdempotent(t *testing.T) {
	f := newFakeSheets()
	defer f.Close()
	c := clientFor(t, f, "Todo")
	ctx := context.Background()
	if err := c.CreateSheetTab(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.EnsureHeader(ctx); err != nil {
		t.Fatal(err)
	}
	// Panggilan kedua tidak boleh menambah baris.
	if err := c.EnsureHeader(ctx); err != nil {
		t.Fatal(err)
	}
	if got := len(f.snapshot()); got != 1 {
		t.Errorf("EnsureHeader menulis ulang header, rows=%d", got)
	}
}
