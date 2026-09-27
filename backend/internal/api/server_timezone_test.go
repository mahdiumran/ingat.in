package api

import (
	"strings"
	"testing"
	"time"

	"ingatin/backend/internal/config"
)

// TestServerNowTextWIB memastikan pesan uji memakai zona operasional (WIB),
// bukan UTC. Sebelumnya label menulis "(UTC)" dan jam meleset 7 jam.
func TestServerNowTextWIB(t *testing.T) {
	s := &Server{cfg: &config.Config{Timezone: "Asia/Jakarta"}}

	got := s.nowText()
	if !strings.Contains(got, "WIB") {
		t.Errorf("nowText() = %q, harus memuat 'WIB'", got)
	}
	if strings.Contains(got, "UTC") {
		t.Errorf("nowText() = %q, tidak boleh memuat 'UTC'", got)
	}
}

// TestServerLocationFallback memastikan zona tak dikenal jatuh ke WIB (UTC+7),
// bukan UTC, agar pesan ke pengguna tetap masuk akal.
func TestServerLocationFallback(t *testing.T) {
	s := &Server{cfg: &config.Config{Timezone: "Bukan/Zona"}}

	_, offset := time.Now().In(s.location()).Zone()
	if offset != 7*3600 {
		t.Errorf("offset zona fallback = %d detik, ingin 25200 (UTC+7)", offset)
	}
}

// TestServerLocationNilCfg memastikan server tanpa cfg tidak panik.
func TestServerLocationNilCfg(t *testing.T) {
	s := &Server{}
	if s.location() == nil {
		t.Fatal("location() = nil, ingin zona non-nil")
	}
	if s.nowText() == "" {
		t.Error("nowText() kosong")
	}
}

// TestTestMessageBody memastikan pesan uji menampilkan waktu WIB (bukan UTC)
// dan menyertakan baris Channel hanya bila channel diberikan.
func TestTestMessageBody(t *testing.T) {
	s := &Server{cfg: &config.Config{Timezone: "Asia/Jakarta"}}

	withChannel := s.testMessageBody("telegram")
	if !strings.Contains(withChannel, "Channel: telegram") {
		t.Errorf("body = %q, harus memuat 'Channel: telegram'", withChannel)
	}
	if !strings.Contains(withChannel, "WIB") {
		t.Errorf("body = %q, harus memuat 'WIB'", withChannel)
	}
	if strings.Contains(withChannel, "UTC") {
		t.Errorf("body = %q, tidak boleh memuat 'UTC'", withChannel)
	}

	withoutChannel := s.testMessageBody("")
	if strings.Contains(withoutChannel, "Channel:") {
		t.Errorf("body tanpa channel = %q, tidak boleh memuat 'Channel:'", withoutChannel)
	}
}
