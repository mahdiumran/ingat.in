package sheets

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// valueInputOptionRaw memakai nilai apa adanya (string), bukan diurai sebagai
// rumus/angka — penting agar judul seperti "=abc" tidak dieksekusi Sheets.
const valueInputOptionRaw = "RAW"

// Client membungkus Google Sheets API v4 untuk spreadsheet target.
type Client struct {
	svc           *sheets.Service
	spreadsheetID string
	sheetName     string
}

// NewClient membuat klien dari JSON service account (tidak terenkripsi).
func NewClient(ctx context.Context, serviceAccountJSON, spreadsheetID, sheetName string) (*Client, error) {
	return newClient(ctx, serviceAccountJSON, spreadsheetID, sheetName, nil)
}

// newClient membuat klien dengan opsi tambahan (dipakai pengujian untuk
// mengarahkan SDK ke server tiruan).
func newClient(ctx context.Context, serviceAccountJSON, spreadsheetID, sheetName string, extra []option.ClientOption) (*Client, error) {
	if strings.TrimSpace(spreadsheetID) == "" {
		return nil, fmt.Errorf("%w: spreadsheet_id kosong", ErrNotConfigured)
	}
	if err := ValidateSheetName(sheetName); err != nil {
		return nil, err
	}

	opts := append([]option.ClientOption{option.WithCredentialsJSON([]byte(serviceAccountJSON))}, extra...)
	svc, err := sheets.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("inisialisasi Google Sheets: %w", err)
	}
	return &Client{svc: svc, spreadsheetID: spreadsheetID, sheetName: sheetName}, nil
}

// rangeA1 menyusun range A1 untuk sebuah kolom pada sheet aktif.
// Contoh: rangeA1("A1:L1") -> "'Todo'!A1:L1".
func (c *Client) rangeA1(a1 string) string {
	// Nama sheet perlu dikutip karena boleh memuat spasi.
	return fmt.Sprintf("'%s'!%s", strings.ReplaceAll(c.sheetName, "'", "''"), a1)
}

// SheetExists melaporkan apakah tab dengan nama ini ada di spreadsheet.
func (c *Client) SheetExists(ctx context.Context) (bool, error) {
	ss, err := c.svc.Spreadsheets.Get(c.spreadsheetID).
		Fields("sheets.properties.title").
		Context(ctx).Do()
	if err != nil {
		return false, wrapAPIError("buka spreadsheet", err)
	}
	for _, s := range ss.Sheets {
		if s.Properties != nil && s.Properties.Title == c.sheetName {
			return true, nil
		}
	}
	return false, nil
}

// CreateSheetTab membuat tab baru dengan nama yang dikonfigurasi.
//
// Mengembalikan nil bila tab sudah ada (idempoten).
func (c *Client) CreateSheetTab(ctx context.Context) error {
	exists, err := c.SheetExists(ctx)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	req := &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{
			{AddSheet: &sheets.AddSheetRequest{
				Properties: &sheets.SheetProperties{Title: c.sheetName},
			}},
		},
	}
	if _, err := c.svc.Spreadsheets.BatchUpdate(c.spreadsheetID, req).Context(ctx).Do(); err != nil {
		return wrapAPIError("buat sheet tab", err)
	}
	return nil
}

// EnsureHeader memastikan baris pertama berisi header yang benar.
//
// Header ditulis bila baris pertama kosong ATAU tidak sama dengan HeaderRow
// (mis. setelah penambahan kolom baru pada versi aplikasi berikutnya). Sifat
// self-healing ini menjaga urutan kolom tetap sinkron tanpa intervensi manual.
func (c *Client) EnsureHeader(ctx context.Context) error {
	rg := c.rangeA1("A1:" + LastColumn + "1")
	resp, err := c.svc.Spreadsheets.Values.Get(c.spreadsheetID, rg).Context(ctx).Do()
	if err != nil {
		return wrapAPIError("baca header", err)
	}
	if len(resp.Values) > 0 && headerMatches(resp.Values[0]) {
		return nil // header sudah benar
	}
	vr := &sheets.ValueRange{Values: [][]any{HeaderRow}}
	if _, err := c.svc.Spreadsheets.Values.Update(c.spreadsheetID, rg, vr).
		ValueInputOption(valueInputOptionRaw).Context(ctx).Do(); err != nil {
		return wrapAPIError("tulis header", err)
	}
	return nil
}

// headerMatches melaporkan apakah baris pertama sama persis dengan HeaderRow.
func headerMatches(existing []any) bool {
	if len(existing) != len(HeaderRow) {
		return false
	}
	for i, want := range HeaderRow {
		if strings.TrimSpace(fmt.Sprint(existing[i])) != fmt.Sprint(want) {
			return false
		}
	}
	return true
}

// AppendRow menambahkan satu baris data ke akhir tabel.
func (c *Client) AppendRow(ctx context.Context, values []any) error {
	rg := c.rangeA1("A1")
	vr := &sheets.ValueRange{Values: [][]any{values}}
	_, err := c.svc.Spreadsheets.Values.Append(c.spreadsheetID, rg, vr).
		ValueInputOption(valueInputOptionRaw).
		InsertDataOption("INSERT_ROWS").
		Context(ctx).Do()
	if err != nil {
		return wrapAPIError("append baris", err)
	}
	return nil
}

// FindRow mencari nomor baris (1-based) yang kolom Ref-nya sama dengan refNo.
// Mengembalikan 0 bila tidak ditemukan.
func (c *Client) FindRow(ctx context.Context, refNo string) (int, error) {
	rg := c.rangeA1("A2:A")
	resp, err := c.svc.Spreadsheets.Values.Get(c.spreadsheetID, rg).Context(ctx).Do()
	if err != nil {
		return 0, wrapAPIError("cari baris", err)
	}
	for i, row := range resp.Values {
		if len(row) == 0 {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(row[0])) == refNo {
			return i + 2, nil // +2: baris data mulai dari 2 (baris 1 = header)
		}
	}
	return 0, nil
}

// UpdateRow menimpa satu baris (1-based) dengan nilai baru.
func (c *Client) UpdateRow(ctx context.Context, rowIndex int, values []any) error {
	if rowIndex < 1 {
		return errors.New("nomor baris tidak valid")
	}
	rg := c.rangeA1(fmt.Sprintf("A%d:%s%d", rowIndex, LastColumn, rowIndex))
	vr := &sheets.ValueRange{Values: [][]any{values}}
	if _, err := c.svc.Spreadsheets.Values.Update(c.spreadsheetID, rg, vr).
		ValueInputOption(valueInputOptionRaw).Context(ctx).Do(); err != nil {
		return wrapAPIError("perbarui baris", err)
	}
	return nil
}

// wrapAPIError memberi pesan yang lebih ramah untuk kesalahan umum Google.
func wrapAPIError(action string, err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "403"):
		return fmt.Errorf("%s: akses ditolak (403) — pastikan spreadsheet sudah dibagikan ke client_email service account sebagai Editor", action)
	case strings.Contains(msg, "404"):
		return fmt.Errorf("%s: spreadsheet atau sheet tidak ditemukan (404) — periksa Spreadsheet ID dan nama sheet", action)
	case strings.Contains(msg, "429"):
		return fmt.Errorf("%s: kuota API terlampaui (429) — coba lagi nanti", action)
	case strings.Contains(msg, "400"):
		return fmt.Errorf("%s: permintaan ditolak Google (400): %v", action, err)
	default:
		return fmt.Errorf("%s: %w", action, err)
	}
}

// TestConnection memastikan tab ada (membuatnya bila perlu), menulis header,
// lalu menulis satu baris uji. Mengembalikan client_email untuk ditampilkan.
func (c *Client) TestConnection(ctx context.Context, clientEmail string) error {
	start := time.Now()
	if err := c.CreateSheetTab(ctx); err != nil {
		return err
	}
	if err := c.EnsureHeader(ctx); err != nil {
		return err
	}
	testRow := make([]any, len(HeaderRow))
	testRow[0] = fmt.Sprintf("TEST-%s", start.In(wib).Format("20060102-150405"))
	testRow[1] = "Baris uji dari Ingat.in"
	testRow[len(testRow)-1] = start.In(wib).Format("02 Jan 2006 15:04")
	if err := c.AppendRow(ctx, testRow); err != nil {
		return err
	}
	return nil
}
