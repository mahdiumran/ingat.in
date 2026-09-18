package notify

import (
	"strings"
	"text/template"
	"time"

	"ingatin/backend/internal/models"
)

// Payload adalah data yang tersedia untuk template notifikasi.
//
// Semua field berupa string agar template sederhana (tanpa perlu fungsi
// tambahan) dan aman dari panic akibat tipe tak terduga.
type Payload struct {
	RefNo     string
	Title     string
	ItemType  string
	Priority  string
	Status    string
	Stage     string
	Owner     string
	Requester string
	Team      string
	CreatedBy string

	DueAt     string
	ExpireAt  string
	StartAt   string
	Remaining string
	CreatedAt string

	OffsetLabel string
	Severity    string

	Description string
	Notes       string

	// RFS
	CustomerName   string
	ServiceID      string
	ServicePackage string
	Bandwidth      string
	PicNOC         string
	PicSales       string
	Site           string
	DeviceRef      string
	ServiceRef     string
	CustomerRef    string

	// Reminder
	SubjectName string
	Category    string

	// Pesan uji
	TargetName string
	Channel    string
}

// templateFuncs menyediakan helper kecil di dalam template.
var templateFuncs = template.FuncMap{
	// list menggabungkan nilai non-kosong dengan pemisah.
	"list": func(sep string, values ...string) string {
		out := make([]string, 0, len(values))
		for _, v := range values {
			if strings.TrimSpace(v) != "" {
				out = append(out, v)
			}
		}
		return strings.Join(out, sep)
	},
	// or mengembalikan nilai pertama yang tidak kosong.
	"or": func(values ...string) string {
		for _, v := range values {
			if strings.TrimSpace(v) != "" {
				return v
			}
		}
		return ""
	},
	// dash mengubah string kosong menjadi "—".
	"dash": func(v string) string {
		if strings.TrimSpace(v) == "" {
			return "—"
		}
		return v
	},
	// upper mengubah ke huruf besar.
	"upper": strings.ToUpper,
}

// ParsedTemplate adalah template yang sudah dikompilasi.
type ParsedTemplate struct {
	subject *template.Template
	body    *template.Template
	raw     *models.NotificationTemplate
}

// ParseTemplate mengompilasi template dari database.
//
// Subject opsional: bila kosong, hanya body yang dirender.
func ParseTemplate(rec *models.NotificationTemplate) (*ParsedTemplate, error) {
	pt := &ParsedTemplate{raw: rec}

	if strings.TrimSpace(rec.SubjectTpl) != "" {
		t, err := template.New("subject").Funcs(templateFuncs).Parse(rec.SubjectTpl)
		if err != nil {
			return nil, err
		}
		pt.subject = t
	}

	body, err := template.New("body").Funcs(templateFuncs).Parse(rec.BodyTpl)
	if err != nil {
		return nil, err
	}
	pt.body = body
	return pt, nil
}

// Render menghasilkan subject dan body final.
func (pt *ParsedTemplate) Render(p Payload) (string, string, error) {
	var subject string
	if pt.subject != nil {
		var sb strings.Builder
		if err := pt.subject.Execute(&sb, p); err != nil {
			return "", "", err
		}
		subject = strings.TrimSpace(sb.String())
	}

	var bb strings.Builder
	if err := pt.body.Execute(&bb, p); err != nil {
		return "", "", err
	}
	return subject, strings.TrimSpace(bb.String()), nil
}

// FallbackTemplate dipakai bila template untuk key tidak ditemukan di database,
// sehingga notifikasi tetap terkirim (dengan format generik) alih-alih gagal.
func FallbackTemplate(key string) *models.NotificationTemplate {
	switch key {
	case TemplateTodoCreated:
		return &models.NotificationTemplate{
			Key:      key,
			Severity: models.SeverityInfo,
			BodyTpl:  "✅ TODO BARU {{.RefNo}}\n{{.Title}}\n\nPrioritas: {{.Priority}}\nOwner: {{dash .Owner}}\nDue: {{dash .DueAt}}\n\nDeskripsi : {{or .Description \"—\"}}",
		}
	case TemplateDailyTaskCreated:
		return &models.NotificationTemplate{
			Key:      key,
			Severity: models.SeverityInfo,
			BodyTpl:  "📋 DAILY TASK BARU {{.RefNo}}\n{{.Title}}\n\nTanggal: {{dash .DueAt}}\nOwner: {{dash .Owner}}\n\nDeskripsi : {{or .Description \"—\"}}",
		}
	case TemplateReminderOffset:
		return &models.NotificationTemplate{
			Key:      key,
			Severity: models.SeverityWarning,
			BodyTpl:  "⏰ REMINDER {{.OffsetLabel}}\n{{.Title}}\n\nSubjek: {{dash .SubjectName}}\nExpire: {{dash .ExpireAt}}\nSisa: {{dash .Remaining}}\n\nDeskripsi : {{or .Description \"—\"}}",
		}
	case TemplateReminderDueToday:
		return &models.NotificationTemplate{
			Key:      key,
			Severity: models.SeverityCritical,
			BodyTpl:  "🔴 HARI INI EXPIRED\n{{.Title}}\n\nSubjek: {{dash .SubjectName}}\nExpire: {{dash .ExpireAt}}\n\nDeskripsi : {{or .Description \"—\"}}",
		}
	case TemplateReminderLate:
		return &models.NotificationTemplate{
			Key:      key,
			Severity: models.SeverityCritical,
			BodyTpl:  "🚨 TERLAMBAT — BELUM ADA AKTIVASI\n{{.Title}}\n\nSubjek: {{dash .SubjectName}}\nExpire: {{dash .ExpireAt}}\nPIC NOC: {{dash .PicNOC}}\n\nDeskripsi : {{or .Description \"—\"}}",
		}
	case TemplateRFSUpcoming:
		return &models.NotificationTemplate{
			Key:      key,
			Severity: models.SeverityWarning,
			BodyTpl:  "📅 RFS {{.OffsetLabel}}\nCustomer: {{dash .CustomerName}}\nPaket: {{.ServicePackage}} / {{.Bandwidth}}\nRFS: {{dash .ExpireAt}}\nPIC NOC: {{dash .PicNOC}}\n\nDeskripsi : {{or .Description \"—\"}}",
		}
	case TemplateRFSToday:
		return &models.NotificationTemplate{
			Key:      key,
			Severity: models.SeverityCritical,
			BodyTpl:  "🚨 RFS HARI INI\nCustomer: {{dash .CustomerName}}\nPaket: {{.ServicePackage}} / {{.Bandwidth}}\nRFS: {{dash .ExpireAt}}\nPIC NOC: {{dash .PicNOC}}\n\nDeskripsi : {{or .Description \"—\"}}",
		}
	case TemplateRFSLate:
		return &models.NotificationTemplate{
			Key:      key,
			Severity: models.SeverityCritical,
			BodyTpl:  "🚨 RFS TERLEWAT\nCustomer: {{dash .CustomerName}}\nRFS: {{dash .ExpireAt}}\nTerlewat: {{dash .Remaining}}\nPIC NOC: {{dash .PicNOC}}\n\nDeskripsi : {{or .Description \"—\"}}",
		}
	case TemplateTestMessage:
		return &models.NotificationTemplate{
			Key:      key,
			Severity: models.SeverityInfo,
			BodyTpl:  "✅ Ingat.in — pesan uji\n\nKanal notifikasi berfungsi.\nTarget: {{dash .TargetName}}\nChannel: {{dash .Channel}}\nWaktu: {{.CreatedAt}}",
		}
	default:
		return &models.NotificationTemplate{
			Key:      key,
			Severity: models.SeverityInfo,
			BodyTpl:  "{{.Title}}\n\n{{.Description}}",
		}
	}
}

// Kunci template yang dipakai aplikasi.
const (
	TemplateTodoCreated      = "TODO_CREATED"
	TemplateDailyTaskCreated = "DAILY_TASK_CREATED"
	TemplateReminderOffset   = "REMINDER_OFFSET"
	TemplateReminderDueToday = "REMINDER_DUE_TODAY"
	TemplateReminderLate     = "REMINDER_LATE"
	TemplateRFSUpcoming      = "RFS_UPCOMING"
	TemplateRFSToday         = "RFS_TODAY"
	TemplateRFSLate          = "RFS_LATE"
	TemplateTestMessage      = "TEST_MESSAGE"
	TemplateSLAWarning       = "SLA_WARNING"
	TemplateSLABreach        = "SLA_BREACH"
)

// FormatWIB memformat waktu ke WIB untuk ditampilkan pada pesan notifikasi.
//
// Pesan dikirim ke manusia, sehingga waktu lokal lebih berguna daripada UTC.
func FormatWIB(t *time.Time) string {
	if t == nil {
		return ""
	}
	wib := time.FixedZone("WIB", 7*3600)
	return t.In(wib).Format("02 Jan 2006 15:04")
}

// HumanRemaining mengubah durasi menjadi teks Indonesia yang mudah dibaca.
//
// Contoh: 50h -> "2 hari 2 jam"; 3h -> "3 jam"; -2h -> "2 jam lewat".
func HumanRemaining(until time.Time, now time.Time) string {
	d := until.Sub(now)

	late := d < 0
	if late {
		d = -d
	}

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60

	var b strings.Builder
	switch {
	case days > 0:
		b.WriteString(itoa(days))
		b.WriteString(" hari")
		if hours > 0 {
			b.WriteString(" ")
			b.WriteString(itoa(hours))
			b.WriteString(" jam")
		}
	case hours > 0:
		b.WriteString(itoa(hours))
		b.WriteString(" jam")
	default:
		b.WriteString(itoa(mins))
		b.WriteString(" menit")
	}

	if late {
		b.WriteString(" lewat")
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
