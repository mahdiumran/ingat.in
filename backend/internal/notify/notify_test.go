package notify

import (
	"strings"
	"testing"
	"time"

	"ingatin/backend/internal/models"
)

/* ---------------------------------------------------------------------------
   Template rendering
   --------------------------------------------------------------------------- */

func TestParseAndRenderTemplate(t *testing.T) {
	rec := &models.NotificationTemplate{
		Key:        "RFS_UPCOMING",
		Severity:   models.SeverityWarning,
		SubjectTpl: "RFS {{.OffsetLabel}}",
		BodyTpl:    "Customer: {{.CustomerName}}\nPaket: {{.ServicePackage}} / {{.Bandwidth}}\nRFS: {{.ExpireAt}}",
	}

	pt, err := ParseTemplate(rec)
	if err != nil {
		t.Fatalf("ParseTemplate: %v", err)
	}

	subject, body, err := pt.Render(Payload{
		OffsetLabel:    "H-3",
		CustomerName:   "PT Contoh Jaya",
		ServicePackage: "Dedicated",
		Bandwidth:      "100 Mbps",
		ExpireAt:       "25 Sep 2026 09:00",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if subject != "RFS H-3" {
		t.Errorf("subject = %q", subject)
	}
	for _, want := range []string{"PT Contoh Jaya", "Dedicated / 100 Mbps", "25 Sep 2026 09:00"} {
		if !strings.Contains(body, want) {
			t.Errorf("body tidak memuat %q:\n%s", want, body)
		}
	}
}

func TestTemplateWithoutSubject(t *testing.T) {
	rec := &models.NotificationTemplate{
		Key:      "X",
		Severity: models.SeverityInfo,
		BodyTpl:  "Halo {{.Title}}",
	}
	pt, err := ParseTemplate(rec)
	if err != nil {
		t.Fatal(err)
	}
	subject, body, err := pt.Render(Payload{Title: "Dunia"})
	if err != nil {
		t.Fatal(err)
	}
	if subject != "" {
		t.Errorf("subject harus kosong, dapat %q", subject)
	}
	if body != "Halo Dunia" {
		t.Errorf("body = %q", body)
	}
}

func TestTemplateHelpersDashAndList(t *testing.T) {
	rec := &models.NotificationTemplate{
		Key:      "X",
		Severity: models.SeverityInfo,
		BodyTpl:  "Owner: {{dash .Owner}} | {{list \", \" .PicNOC .PicSales}}",
	}
	pt, err := ParseTemplate(rec)
	if err != nil {
		t.Fatal(err)
	}

	// Nilai kosong -> "—" dan list menyaring kosong.
	_, body, err := pt.Render(Payload{Owner: "", PicNOC: "Budi", PicSales: ""})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "Owner: —") {
		t.Errorf("helper dash tidak bekerja: %q", body)
	}
	if !strings.Contains(body, "Budi") || strings.Contains(body, ",") {
		t.Errorf("helper list tidak menyaring nilai kosong: %q", body)
	}
}

func TestTemplateInvalidSyntax(t *testing.T) {
	rec := &models.NotificationTemplate{
		Key:      "X",
		Severity: models.SeverityInfo,
		BodyTpl:  "Hello {{.Title",
	}
	if _, err := ParseTemplate(rec); err == nil {
		t.Fatal("template rusak seharusnya gagal di-parse")
	}
}

func TestFallbackTemplatesRenderWithoutError(t *testing.T) {
	keys := []string{
		TemplateTodoCreated, TemplateReminderOffset, TemplateReminderDueToday,
		TemplateReminderLate, TemplateRFSUpcoming, TemplateRFSToday,
		TemplateRFSLate, TemplateTestMessage, TemplateSLAWarning, "UNKNOWN_KEY",
	}
	payload := Payload{
		RefNo: "RFS-2026-0001", Title: "Uji", Priority: "high",
		Owner: "noc", DueAt: "01 Jan 2026 08:00", ExpireAt: "02 Jan 2026 08:00",
		OffsetLabel: "H-1", Remaining: "24 jam", CustomerName: "PT X",
		ServicePackage: "Dedicated", Bandwidth: "100 Mbps", PicNOC: "Budi",
		SubjectName: "PT X", CreatedAt: "01 Jan 2026 00:00",
	}

	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			rec := FallbackTemplate(key)
			pt, err := ParseTemplate(rec)
			if err != nil {
				t.Fatalf("fallback %s tidak dapat di-parse: %v", key, err)
			}
			_, body, err := pt.Render(payload)
			if err != nil {
				t.Fatalf("fallback %s gagal dirender: %v", key, err)
			}
			if strings.TrimSpace(body) == "" {
				t.Errorf("fallback %s menghasilkan body kosong", key)
			}
		})
	}
}

/* ---------------------------------------------------------------------------
   Offset
   --------------------------------------------------------------------------- */

func TestOffsetTimeHoursBefore(t *testing.T) {
	expire := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	h24 := 24

	at, label := offsetTime(expire, models.EscalationOffset{Label: "H-1", HoursBefore: &h24})
	if label != "H-1" {
		t.Errorf("label = %q", label)
	}
	want := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	if !at.Equal(want) {
		t.Errorf("H-1 = %v, ingin %v", at, want)
	}
}

func TestOffsetTimeHoursAfter(t *testing.T) {
	expire := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	h1 := 1

	at, label := offsetTime(expire, models.EscalationOffset{Label: "LATE-1H", HoursAfter: &h1})
	if label != "LATE-1H" {
		t.Errorf("label = %q", label)
	}
	want := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	if !at.Equal(want) {
		t.Errorf("LATE-1H = %v, ingin %v", at, want)
	}
}

func TestOffsetTimeNoHours(t *testing.T) {
	_, label := offsetTime(time.Now(), models.EscalationOffset{Label: "X"})
	if label != "" {
		t.Errorf("offset tanpa hours seharusnya kosong, dapat %q", label)
	}
}

func TestTrial3DPolicyOffsetsAreCorrect(t *testing.T) {
	// Mencerminkan policy TRIAL-3D pada seed: H-2, H-1, H-0, LATE-1H.
	p := &models.EscalationPolicy{
		Name: "TRIAL-3D",
		Offsets: []models.EscalationOffset{
			{Label: "H-2", Severity: models.SeverityInfo, HoursBefore: intPtr(48)},
			{Label: "H-1", Severity: models.SeverityWarning, HoursBefore: intPtr(24)},
			{Label: "H-0", Severity: models.SeverityCritical, HoursBefore: intPtr(0)},
			{Label: "LATE-1H", Severity: models.SeverityCritical, HoursAfter: intPtr(1)},
		},
	}

	start := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC) // trial 3 hari
	expire := start.Add(72 * time.Hour)                   // 25 Sep 09:00

	// Verifikasi setiap offset relatif terhadap expire.
	expectations := []struct {
		label string
		want  time.Time
	}{
		{"H-2", time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)},
		{"H-1", time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)},
		{"H-0", time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)},
		{"LATE-1H", time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)},
	}

	for i, exp := range expectations {
		got, label := offsetTime(expire, p.Offsets[i])
		if label != exp.label {
			t.Errorf("offset[%d] label = %q, ingin %q", i, label, exp.label)
		}
		if !got.Equal(exp.want) {
			t.Errorf("offset %s = %v, ingin %v", exp.label, got, exp.want)
		}
	}
}

func TestAllBeforeOffsetsPassed(t *testing.T) {
	expire := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	offsets := []models.EscalationOffset{
		{Label: "H-2", HoursBefore: intPtr(48)},
		{Label: "H-0", HoursBefore: intPtr(0)},
		{Label: "LATE-1H", HoursAfter: intPtr(1)},
	}

	// Sebelum H-2: belum semua lewat.
	if allBeforeOffsetsPassed(expire, offsets, time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)) {
		t.Error("seharusnya belum semua offset H-* terlewat")
	}
	// Tepat setelah H-0: semua lewat.
	if !allBeforeOffsetsPassed(expire, offsets, expire.Add(time.Minute)) {
		t.Error("setelah H-0 semua offset H-* seharusnya terlewat")
	}
}

/* ---------------------------------------------------------------------------
   Pemilihan template per offset
   --------------------------------------------------------------------------- */

func TestReminderTemplateForOffset(t *testing.T) {
	cases := []struct {
		name      string
		offset    models.EscalationOffset
		wantKey   string
		wantSever string
	}{
		{
			name:      "H-2 info",
			offset:    models.EscalationOffset{Label: "H-2", Severity: models.SeverityInfo, HoursBefore: intPtr(48)},
			wantKey:   TemplateReminderOffset,
			wantSever: models.SeverityInfo,
		},
		{
			name:      "H-1 warning",
			offset:    models.EscalationOffset{Label: "H-1", Severity: models.SeverityWarning, HoursBefore: intPtr(24)},
			wantKey:   TemplateReminderOffset,
			wantSever: models.SeverityWarning,
		},
		{
			name:      "H-0 memakai template hari-H",
			offset:    models.EscalationOffset{Label: "H-0", Severity: models.SeverityCritical, HoursBefore: intPtr(0)},
			wantKey:   TemplateReminderDueToday,
			wantSever: models.SeverityCritical,
		},
		{
			name:      "LATE memakai template eskalasi",
			offset:    models.EscalationOffset{Label: "LATE-1H", Severity: models.SeverityCritical, HoursAfter: intPtr(1)},
			wantKey:   TemplateReminderLate,
			wantSever: models.SeverityCritical,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, sev := reminderTemplateFor(tc.offset)
			if key != tc.wantKey {
				t.Errorf("template = %q, ingin %q", key, tc.wantKey)
			}
			if sev != tc.wantSever {
				t.Errorf("severity = %q, ingin %q", sev, tc.wantSever)
			}
		})
	}
}

func TestRFSTemplateForOffset(t *testing.T) {
	if key, _ := rfsTemplateFor(models.EscalationOffset{Label: "H-7", HoursBefore: intPtr(168)}); key != TemplateRFSUpcoming {
		t.Errorf("H-7 = %q", key)
	}
	if key, sev := rfsTemplateFor(models.EscalationOffset{Label: "H-0", HoursBefore: intPtr(0)}); key != TemplateRFSToday || sev != models.SeverityCritical {
		t.Errorf("H-0 = %q/%q", key, sev)
	}
	if key, sev := rfsTemplateFor(models.EscalationOffset{Label: "LATE-4H", HoursAfter: intPtr(4)}); key != TemplateRFSLate || sev != models.SeverityCritical {
		t.Errorf("LATE = %q/%q", key, sev)
	}
}

/* ---------------------------------------------------------------------------
   Backoff
   --------------------------------------------------------------------------- */

func TestBackoffSchedule(t *testing.T) {
	cases := map[int]time.Duration{
		1: time.Minute,
		2: 2 * time.Minute,
		3: 5 * time.Minute,
		4: 15 * time.Minute,
		5: 60 * time.Minute,
		9: 60 * time.Minute, // di atas batas tetap 60 menit
	}
	for attempt, want := range cases {
		if got := backoffFor(attempt); got != want {
			t.Errorf("backoffFor(%d) = %v, ingin %v", attempt, got, want)
		}
	}
}

func TestBackoffIsIncreasing(t *testing.T) {
	prev := time.Duration(0)
	for i := 1; i <= 5; i++ {
		cur := backoffFor(i)
		if cur < prev {
			t.Errorf("backoff tidak monoton pada attempt %d: %v < %v", i, cur, prev)
		}
		prev = cur
	}
}

/* ---------------------------------------------------------------------------
   Waktu & sisa waktu
   --------------------------------------------------------------------------- */

func TestFormatWIB(t *testing.T) {
	// 2026-09-25 02:00 UTC = 09:00 WIB.
	utc := time.Date(2026, 9, 25, 2, 0, 0, 0, time.UTC)
	got := FormatWIB(&utc)
	if !strings.Contains(got, "09:00") {
		t.Errorf("FormatWIB = %q, seharusnya memuat 09:00", got)
	}
	if !strings.Contains(got, "25") {
		t.Errorf("FormatWIB = %q, seharusnya memuat tanggal 25", got)
	}
	if FormatWIB(nil) != "" {
		t.Error("FormatWIB(nil) harus kosong")
	}
}

func TestHumanRemaining(t *testing.T) {
	now := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		until time.Time
		want  string
	}{
		{"2 hari 2 jam", now.Add(50 * time.Hour), "2 hari 2 jam"},
		{"1 hari", now.Add(24 * time.Hour), "1 hari"},
		{"3 jam", now.Add(3 * time.Hour), "3 jam"},
		{"30 menit", now.Add(30 * time.Minute), "30 menit"},
		{"terlewat 2 jam", now.Add(-2 * time.Hour), "2 jam lewat"},
		{"terlewat 5 menit", now.Add(-5 * time.Minute), "5 menit lewat"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HumanRemaining(tc.until, now); got != tc.want {
				t.Errorf("HumanRemaining = %q, ingin %q", got, tc.want)
			}
		})
	}
}

/* ---------------------------------------------------------------------------
   Payload dari JSONB
   --------------------------------------------------------------------------- */

func TestPayloadFromJSON(t *testing.T) {
	m := map[string]any{
		"ref_no":        "RFS-2026-0007",
		"title":         "RFS PT Contoh",
		"priority":      "high",
		"customer_name": "PT Contoh",
		"bandwidth":     "100 Mbps",
		"offset_label":  "H-3",
		"remaining":     "3 hari",
		"device_ref":    "MX204-CGK1",
		"extra_unknown": "diabaikan",
		"numeric_field": float64(42),
	}

	p := payloadFromJSON(m)
	if p.RefNo != "RFS-2026-0007" {
		t.Errorf("RefNo = %q", p.RefNo)
	}
	if p.Title != "RFS PT Contoh" {
		t.Errorf("Title = %q", p.Title)
	}
	if p.CustomerName != "PT Contoh" {
		t.Errorf("CustomerName = %q", p.CustomerName)
	}
	if p.OffsetLabel != "H-3" {
		t.Errorf("OffsetLabel = %q", p.OffsetLabel)
	}
	if p.DeviceRef != "MX204-CGK1" {
		t.Errorf("DeviceRef = %q", p.DeviceRef)
	}
	// Field tidak dikenal diabaikan tanpa panic.
	if p.PicNOC != "" {
		t.Errorf("PicNOC harus kosong, dapat %q", p.PicNOC)
	}
}

func TestPayloadFromNilJSON(t *testing.T) {
	p := payloadFromJSON(nil)
	if p.RefNo != "" || p.Title != "" {
		t.Error("payload dari nil harus kosong")
	}
}

/* ---------------------------------------------------------------------------
   Normalisasi source type
   --------------------------------------------------------------------------- */

func TestNormalizeSourceType(t *testing.T) {
	valid := []string{"work_item", "todo", "reminder", "rfs", "ticket", "manual", "test", "system"}
	for _, v := range valid {
		if got := normalizeSourceType(v); got != v {
			t.Errorf("normalizeSourceType(%q) = %q", v, got)
		}
	}
	// Nilai tidak dikenal harus dipetakan ke 'system' agar tidak melanggar CHECK.
	if got := normalizeSourceType("sembarang"); got != "system" {
		t.Errorf("nilai tak dikenal = %q, ingin system", got)
	}
	if got := normalizeSourceType(""); got != "system" {
		t.Errorf("string kosong = %q, ingin system", got)
	}
}

/* ---------------------------------------------------------------------------
   Validasi placeholder vs struct Payload
   ---------------------------------------------------------------------------
   Regresi yang ditemukan saat uji F7: template seed memakai {{.PicNoc}}
   sedangkan struct memakai PicNOC (case-sensitive). Uji ini memastikan setiap
   placeholder pada template bawaan benar-benar ada di struct Payload.
   --------------------------------------------------------------------------- */

func TestFallbackTemplatePlaceholdersMatchPayload(t *testing.T) {
	keys := []string{
		TemplateTodoCreated, TemplateReminderOffset, TemplateReminderDueToday,
		TemplateReminderLate, TemplateRFSUpcoming, TemplateRFSToday,
		TemplateRFSLate, TemplateTestMessage, TemplateSLAWarning, TemplateSLABreach,
	}

	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			rec := FallbackTemplate(key)
			// Parsing gagal bila sintaks template salah.
			pt, err := ParseTemplate(rec)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			// Eksekusi dengan payload kosong: text/template akan error bila
			// field tidak ada di struct (inilah yang terjadi pada .PicNoc).
			if _, _, err := pt.Render(Payload{}); err != nil {
				t.Fatalf("render gagal — kemungkinan nama field tidak cocok dengan struct Payload: %v", err)
			}
		})
	}
}

func TestUnifiedPicNOCPlaceholder(t *testing.T) {
	// Nama field kanonik adalah PicNOC. Memastikan tidak ada yang memakai
	// varian camelCase yang akan gagal dirender.
	rec := &models.NotificationTemplate{
		Key:      "X",
		Severity: models.SeverityInfo,
		BodyTpl:  "PIC NOC: {{.PicNOC}} | Sales: {{.PicSales}}",
	}
	pt, err := ParseTemplate(rec)
	if err != nil {
		t.Fatal(err)
	}
	_, body, err := pt.Render(Payload{PicNOC: "Budi", PicSales: "Sari"})
	if err != nil {
		t.Fatalf("PicNOC/PicSales harus dapat dirender: %v", err)
	}
	if !strings.Contains(body, "Budi") || !strings.Contains(body, "Sari") {
		t.Errorf("body tidak memuat nilai yang diharapkan: %q", body)
	}

	// Varian camelCase yang salah harus MENGHASILKAN ERROR, sehingga bug
	// serupa tidak lolos diam-diam.
	bad := &models.NotificationTemplate{
		Key:      "Y",
		Severity: models.SeverityInfo,
		BodyTpl:  "{{.PicNoc}}",
	}
	badPt, err := ParseTemplate(bad)
	if err != nil {
		return // gagal parse juga dapat diterima
	}
	if _, _, err := badPt.Render(Payload{}); err == nil {
		t.Error("placeholder .PicNoc seharusnya gagal (field tidak ada di Payload)")
	}
}

func TestAllSeedTemplateKeysHaveRenderingPayload(t *testing.T) {
	// Semua kunci template yang dipakai aplikasi harus punya fallback yang
	// dapat dirender — mencegah kegagalan saat baris database belum ada.
	keys := []string{
		TemplateTodoCreated, TemplateDailyTaskCreated, TemplateReminderOffset,
		TemplateReminderDueToday, TemplateReminderLate, TemplateRFSUpcoming,
		TemplateRFSToday, TemplateRFSLate, TemplateTestMessage,
	}
	full := Payload{
		RefNo: "REM-2026-0001", Title: "T", ItemType: "reminder",
		Priority: "high", Status: "active", Owner: "noc", Requester: "sales",
		Team: "NOC", CreatedBy: "admin",
		DueAt: "01 Jan 2026 08:00", ExpireAt: "02 Jan 2026 08:00",
		StartAt: "01 Jan 2026 07:00", Remaining: "24 jam", CreatedAt: "01 Jan 2026 00:00",
		OffsetLabel: "H-1", Severity: "warning",
		Description: "d", Notes: "n",
		CustomerName: "PT X", ServiceID: "SVC-1", ServicePackage: "Dedicated",
		Bandwidth: "100 Mbps", PicNOC: "Budi", PicSales: "Sari", Site: "CGK-1",
		DeviceRef: "MX204", ServiceRef: "SR-1", CustomerRef: "CR-1",
		SubjectName: "PT X", Category: "trial", TargetName: "NOC-Team", Channel: "telegram",
	}

	for _, key := range keys {
		rec := FallbackTemplate(key)
		pt, err := ParseTemplate(rec)
		if err != nil {
			t.Errorf("%s: parse gagal: %v", key, err)
			continue
		}
		_, body, err := pt.Render(full)
		if err != nil {
			t.Errorf("%s: render dengan payload lengkap gagal: %v", key, err)
			continue
		}
		// Pastikan tidak ada placeholder yang tersisa sebagai literal.
		if strings.Contains(body, "{{") || strings.Contains(body, "<no value>") {
			t.Errorf("%s: output memuat placeholder yang tidak tergantikan: %q", key, body)
		}
	}
}
