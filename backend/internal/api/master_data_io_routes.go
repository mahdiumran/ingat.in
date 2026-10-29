package api

/* ---------------------------------------------------------------------------
   F40 — Import & export master data customer.

   Import  : CSV (encoding/csv, tanpa dependensi parser tambahan).
   Export  : XLSX asli via github.com/xuri/excelize/v2.
   Template: CSV atau XLSX (berisi header + contoh + panduan jenis).

   Keduanya dijaga izin masterdata.write. Import bersifat UPSERT:
     - kolom `kode` diisi & sudah ada  -> perbarui entri itu
     - kolom `kode` diisi & belum ada  -> buat dengan kode tersebut
     - kolom `kode` kosong             -> buat dengan kode otomatis
   Baris yang gagal dicatat per baris; proses tidak dibatalkan seluruhnya.
   --------------------------------------------------------------------------- */

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/repository"
)

// Kolom CSV import/export customer (urutan baku).
var customerCSVColumns = []string{
	"nama", "jenis", "kode", "pic", "telepon", "email", "alamat", "kapasitas",
}

// importRowError adalah satu baris gagal pada laporan import.
type importRowError struct {
	Row   int    `json:"row"`
	Kode  string `json:"kode,omitempty"`
	Nama  string `json:"nama,omitempty"`
	Error string `json:"error"`
}

// handleImportCustomer mengimpor customer dari berkas CSV.
//
// POST /api/master-data/import  (multipart/form-data, field "file")
// Query: ?mode=upsert (default) | skip
func (s *Server) handleImportCustomer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(8 << 20); err != nil { // 8 MiB
		writeErr(w, http.StatusBadRequest, "berkas tidak dapat dibaca (maks 8 MB)")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "field 'file' wajib berisi berkas CSV")
		return
	}
	defer file.Close()

	if strings.HasSuffix(strings.ToLower(header.Filename), ".xlsx") {
		writeErr(w, http.StatusBadRequest,
			"unggah CSV; berkas XLSX hanya untuk isian lalu simpan sebagai CSV")
		return
	}

	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	if mode == "" {
		mode = "upsert"
	}
	if mode != "upsert" && mode != "skip" {
		writeErr(w, http.StatusBadRequest, "mode harus upsert atau skip")
		return
	}

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	head, err := reader.Read()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "CSV kosong atau header tidak terbaca")
		return
	}
	idx, err := mapCustomerHeader(head)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	actor := currentUsername(r)
	var (
		inserted, updated, skipped int
		failures                   []importRowError
	)

	rowNum := 1 // header = baris 1
	for {
		rec, err := reader.Read()
		if err == io.EOF {
			break
		}
		rowNum++
		if err != nil {
			failures = append(failures, importRowError{Row: rowNum, Error: "baris tidak valid: " + err.Error()})
			continue
		}

		get := func(col string) string {
			i, ok := idx[col]
			if !ok || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}

		nama := get("nama")
		if nama == "" {
			failures = append(failures, importRowError{Row: rowNum, Error: "kolom nama wajib diisi"})
			continue
		}
		jenis := strings.ToLower(get("jenis"))
		meta := map[string]any{
			"jenis":    jenis,
			"pic":      get("pic"),
			"phone":    get("telepon"),
			"email":    get("email"),
			"address":  get("alamat"),
			"capacity": get("kapasitas"),
		}
		if err := validateCustomerMeta(meta); err != nil {
			failures = append(failures, importRowError{Row: rowNum, Nama: nama, Error: err.Error()})
			continue
		}

		kode := get("kode")
		existing, findErr := s.findCustomerByCode(r, kode)
		if findErr != nil {
			failures = append(failures, importRowError{Row: rowNum, Nama: nama, Kode: kode, Error: findErr.Error()})
			continue
		}

		switch {
		case existing != nil && mode == "skip":
			skipped++
			continue
		case existing != nil:
			desc := customerDescription(jenis, get("kapasitas"))
			if _, err := s.store.UpdateMasterData(r.Context(), existing.ID, repository.UpdateMasterDataParams{
				Label:       &nama,
				Description: &desc,
				Meta:        meta,
			}); err != nil {
				failures = append(failures, importRowError{Row: rowNum, Nama: nama, Kode: kode, Error: err.Error()})
				continue
			}
			updated++
		default:
			code := kode
			if code == "" {
				next, err := s.store.NextMasterDataCode(r.Context(), "customer")
				if err != nil {
					failures = append(failures, importRowError{Row: rowNum, Nama: nama, Error: err.Error()})
					continue
				}
				code = next
			}
			if _, err := s.store.CreateMasterData(r.Context(), repository.CreateMasterDataParams{
				Kind:        models.KindCustomer,
				Code:        code,
				Label:       nama,
				Description: customerDescription(jenis, get("kapasitas")),
				Meta:        meta,
				IsActive:    true,
				CreatedBy:   actor,
			}); err != nil {
				failures = append(failures, importRowError{Row: rowNum, Nama: nama, Kode: code, Error: err.Error()})
				continue
			}
			inserted++
		}
	}

	s.audit(r, actor, "masterdata.customer.import", "master_data", models.KindCustomer, map[string]any{
		"inserted": inserted, "updated": updated, "skipped": skipped, "failed": len(failures),
	}, true)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       len(failures) == 0,
		"inserted": inserted,
		"updated":  updated,
		"skipped":  skipped,
		"failed":   len(failures),
		"errors":   failures,
	})
}

// findCustomerByCode mencari entri customer berdasarkan kode (case-insensitive).
func (s *Server) findCustomerByCode(r *http.Request, code string) (*models.MasterData, error) {
	if strings.TrimSpace(code) == "" {
		return nil, nil
	}
	list, err := s.store.ListMasterData(r.Context(), repository.MasterDataFilter{Kind: models.KindCustomer})
	if err != nil {
		return nil, err
	}
	for i := range list {
		if strings.EqualFold(strings.TrimSpace(list[i].Code), strings.TrimSpace(code)) {
			return &list[i], nil
		}
	}
	return nil, nil
}

// mapCustomerHeader memetakan nama kolom (huruf kecil) ke indeksnya.
func mapCustomerHeader(head []string) (map[string]int, error) {
	idx := map[string]int{}
	for i, raw := range head {
		name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff")))
		if name != "" {
			idx[name] = i
		}
	}
	if _, ok := idx["nama"]; !ok {
		return nil, fmt.Errorf("header CSV wajib memuat kolom 'nama' (kolom: %s)",
			strings.Join(customerCSVColumns, ", "))
	}
	if _, ok := idx["jenis"]; !ok {
		return nil, fmt.Errorf("header CSV wajib memuat kolom 'jenis' (personal/corporate/reseller/dedicated)")
	}
	return idx, nil
}

// customerDescription menyusun deskripsi ringkas dari jenis & kapasitas.
func customerDescription(jenis, capacity string) string {
	label := jenis
	if l, ok := map[string]string{
		models.CustomerPersonal:  "Personal",
		models.CustomerCorporate: "Corporate",
		models.CustomerReseller:  "Reseller",
		models.CustomerDedicated: "Dedicated",
	}[jenis]; ok {
		label = l
	} else if label != "" {
		label = strings.ToUpper(label[:1]) + label[1:]
	}
	if c := strings.TrimSpace(capacity); c != "" {
		return label + " · " + c
	}
	return label
}

// handleCustomerTemplate mengunduh template import (CSV atau XLSX).
//
// GET /api/master-data/import/template?format=csv|xlsx
func (s *Server) handleCustomerTemplate(w http.ResponseWriter, r *http.Request) {
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "csv"
	}
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="template-customer.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write(customerCSVColumns)
		_ = cw.Write([]string{"PT Contoh Jaya", "corporate", "", "Budi", "08123456789", "budi@contoh.id", "Jl. Contoh 1", "100 Mbps"})
		_ = cw.Write([]string{"Budi Santoso", "personal", "", "", "08120000000", "", "Jl. Mawar 2", ""})
		cw.Flush()
		return
	}
	if format != "xlsx" {
		writeErr(w, http.StatusBadRequest, "format harus csv atau xlsx")
		return
	}

	s.writeCustomerTemplateXLSX(w)
}

// handleCustomerExport mengunduh master data customer sebagai XLSX.
//
// GET /api/master-data/export?jenis=corporate|reseller|dedicated|personal|all
//
// jenis=all (default) menghasilkan satu sheet per jenis.
func (s *Server) handleCustomerExport(w http.ResponseWriter, r *http.Request) {
	jenis := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("jenis")))
	if jenis == "" {
		jenis = "all"
	}
	if jenis != "all" && !models.IsValidCustomerJenis(jenis) {
		writeErr(w, http.StatusBadRequest,
			"jenis harus all, personal, corporate, reseller, atau dedicated")
		return
	}

	list, err := s.store.ListMasterData(r.Context(), repository.MasterDataFilter{Kind: models.KindCustomer})
	if err != nil {
		writeInternalError(w, err)
		return
	}

	groups := map[string][]models.MasterData{}
	for _, e := range list {
		j := customerJenisOf(e)
		if jenis != "all" && j != jenis {
			continue
		}
		groups[j] = append(groups[j], e)
	}

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	defaultSheet := f.GetSheetName(0)
	created := 0
	for _, j := range models.CustomerJenisList {
		entries, ok := groups[j]
		if !ok || len(entries) == 0 {
			continue
		}
		name := sheetNameForJenis(j)
		if created == 0 {
			if err := f.SetSheetName(defaultSheet, name); err != nil {
				writeInternalError(w, err)
				return
			}
		} else if _, err := f.NewSheet(name); err != nil {
			writeInternalError(w, err)
			return
		}
		created++
		if err := fillCustomerSheet(f, name, entries); err != nil {
			writeInternalError(w, err)
			return
		}
	}
	if created == 0 {
		if err := f.SetSheetName(defaultSheet, "Customer"); err != nil {
			writeInternalError(w, err)
			return
		}
		if err := fillCustomerSheet(f, "Customer", nil); err != nil {
			writeInternalError(w, err)
			return
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		writeInternalError(w, err)
		return
	}

	filename := "master-data-customer.xlsx"
	if jenis != "all" {
		filename = "master-data-customer-" + jenis + ".xlsx"
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	_, _ = w.Write(buf.Bytes())
}

// writeCustomerTemplateXLSX mengirim template import berformat XLSX.
func (s *Server) writeCustomerTemplateXLSX(w http.ResponseWriter) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	const dataSheet = "Customer"
	_ = f.SetSheetName(f.GetSheetName(0), dataSheet)

	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#E8DEF8"}},
	})
	cols := make([]any, len(customerCSVColumns))
	for i, c := range customerCSVColumns {
		cols[i] = c
	}
	_ = f.SetSheetRow(dataSheet, "A1", &cols)
	_ = f.SetCellStyle(dataSheet, "A1", "H1", headerStyle)
	_ = f.SetColWidth(dataSheet, "A", "A", 28)
	_ = f.SetColWidth(dataSheet, "B", "B", 14)
	_ = f.SetColWidth(dataSheet, "C", "C", 18)
	_ = f.SetColWidth(dataSheet, "D", "H", 20)

	_ = f.SetSheetRow(dataSheet, "A2", &[]any{"PT Contoh Jaya", "corporate", "", "Budi", "08123456789", "budi@contoh.id", "Jl. Contoh 1", "100 Mbps"})
	_ = f.SetSheetRow(dataSheet, "A3", &[]any{"Budi Santoso", "personal", "", "", "08120000000", "", "Jl. Mawar 2", ""})

	_ = f.AddDataValidation(dataSheet, &excelize.DataValidation{
		Sqref:      "B2:B1000",
		AllowBlank: true,
		Type:       "list",
		Formula1:   `"personal,corporate,reseller,dedicated"`,
	})

	const guideSheet = "Panduan"
	_, _ = f.NewSheet(guideSheet)
	_ = f.SetColWidth(guideSheet, "A", "A", 18)
	_ = f.SetColWidth(guideSheet, "B", "B", 62)
	guide := [][]any{
		{"Kolom", "Keterangan"},
		{"nama", "Wajib. Nama pelanggan atau perusahaan."},
		{"jenis", "Wajib. Salah satu: personal, corporate, reseller, dedicated."},
		{"kode", "Opsional. Kosongkan agar sistem membuat CUST-<tahun>-<urut>."},
		{"pic", "Wajib untuk corporate/reseller/dedicated; boleh kosong untuk personal."},
		{"telepon", "Opsional. Nomor telepon/WhatsApp."},
		{"email", "Opsional. Surel PIC/pelanggan."},
		{"alamat", "Opsional. Alamat pemasangan/penagihan."},
		{"kapasitas", "Opsional. Kapasitas layanan, mis. 100 Mbps."},
		{"", ""},
		{"Catatan", "Simpan sheet Customer sebagai CSV sebelum diunggah; import menerima CSV."},
		{"Upsert", "Kode yang sudah ada akan diperbarui; kode baru akan dibuat."},
	}
	for i, row := range guide {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		rowCopy := row
		_ = f.SetSheetRow(guideSheet, cell, &rowCopy)
	}
	_ = f.SetCellStyle(guideSheet, "A1", "B1", headerStyle)

	buf, err := f.WriteToBuffer()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="template-customer.xlsx"`)
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	_, _ = w.Write(buf.Bytes())
}

// sheetNameForJenis mengembalikan nama sheet per jenis customer.
func sheetNameForJenis(jenis string) string {
	switch jenis {
	case models.CustomerCorporate:
		return "Corporate"
	case models.CustomerReseller:
		return "Reseller"
	case models.CustomerDedicated:
		return "Dedicated"
	case models.CustomerPersonal:
		return "Personal"
	default:
		return "Customer"
	}
}

// customerJenisOf mengambil jenis customer dari meta (default personal).
func customerJenisOf(e models.MasterData) string {
	j := metaString(e.Meta, "jenis")
	if models.IsValidCustomerJenis(j) {
		return j
	}
	if strings.TrimSpace(j) == "" {
		return models.CustomerPersonal
	}
	return strings.ToLower(strings.TrimSpace(j))
}

// fillCustomerSheet menulis header + baris customer ke satu sheet.
func fillCustomerSheet(f *excelize.File, sheet string, entries []models.MasterData) error {
	headerStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#E8DEF8"}},
	})
	if err != nil {
		return err
	}
	header := []any{"Kode", "Nama", "Jenis", "PIC", "Telepon", "Email", "Alamat", "Kapasitas", "Deskripsi", "Status", "Dibuat"}
	if err := f.SetSheetRow(sheet, "A1", &header); err != nil {
		return err
	}
	if err := f.SetCellStyle(sheet, "A1", "K1", headerStyle); err != nil {
		return err
	}
	widths := []struct {
		col string
		w   float64
	}{
		{"A", 18}, {"B", 28}, {"C", 14}, {"D", 20}, {"E", 16}, {"F", 24},
		{"G", 30}, {"H", 14}, {"I", 24}, {"J", 10}, {"K", 20},
	}
	for _, wd := range widths {
		if err := f.SetColWidth(sheet, wd.col, wd.col, wd.w); err != nil {
			return err
		}
	}

	for i, e := range entries {
		row := []any{
			e.Code,
			e.Label,
			sheetNameForJenis(customerJenisOf(e)),
			metaString(e.Meta, "pic"),
			metaString(e.Meta, "phone"),
			metaString(e.Meta, "email"),
			metaString(e.Meta, "address"),
			metaString(e.Meta, "capacity"),
			e.Description,
			statusLabel(e.IsActive),
			e.CreatedAt.Format("2006-01-02 15:04"),
		}
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		rowCopy := row
		if err := f.SetSheetRow(sheet, cell, &rowCopy); err != nil {
			return err
		}
	}
	return f.AutoFilter(sheet, fmt.Sprintf("A1:K%d", len(entries)+1), nil)
}

// statusLabel menerjemahkan is_active ke teks Indonesia.
func statusLabel(active bool) string {
	if active {
		return "Aktif"
	}
	return "Nonaktif"
}
