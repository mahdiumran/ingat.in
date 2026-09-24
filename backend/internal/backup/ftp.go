// Package backup menyediakan pembuatan, pemulihan, dan pengarsipan cadangan
// database Ingat.in (F34).
//
// Desain:
//   - Cadangan dibuat dengan `pg_dump -Fc` (format custom) sehingga dapat
//     dipulihkan lebih fleksibel dan mendukung pemeriksaan integritas.
//   - Berkas disimpan di <DataDir>/backups dengan nama ber-timestamp.
//   - Pengunggahan ke FTP memakai klien FTP minimal (stdlib, tanpa dependensi
//     pihak ketiga) yang cukup untuk MKD + STOR pada server FTP standar.
//   - Seluruh konfigurasi (jadwal, retensi, FTP) disimpan di tabel settings
//     sehingga dapat diubah operator dari panel tanpa deploy ulang.
package backup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// FTPConfig menampung parameter koneksi FTP.
type FTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	// Dir adalah direktori tujuan (dibuat bila belum ada). Boleh kosong.
	Dir string
	// TLS memakai FTPS eksplisit (AUTH TLS) bila true.
	TLS bool
	// Passive memakai mode PASV (default true bila tidak diset).
	Passive bool
}

// ftpError adalah kesalahan protokol FTP dengan kode balasan.
type ftpError struct {
	code int
	msg  string
}

func (e *ftpError) Error() string {
	return fmt.Sprintf("FTP %d: %s", e.code, e.msg)
}

// ftpClient adalah klien FTP minimal.
type ftpClient struct {
	conn net.Conn
	r    *bufio.Reader
}

// dialFTP membuka koneksi kontrol dan melakukan login.
func dialFTP(cfg FTPConfig) (*ftpClient, error) {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return nil, errors.New("host FTP kosong")
	}
	port := cfg.Port
	if port <= 0 {
		port = 21
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	conn, err := net.DialTimeout("tcp", addr, 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("koneksi FTP: %w", err)
	}
	// Timeout keseluruhan operasi agar tidak menggantung selamanya.
	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))

	c := &ftpClient{conn: conn, r: bufio.NewReader(conn)}

	if _, _, err := c.readResponse(); err != nil {
		c.Close()
		return nil, err
	}
	if cfg.TLS {
		// FTPS eksplisit tidak didukung tanpa TLS wrapper; beri pesan jelas.
		c.Close()
		return nil, errors.New("FTPS (AUTH TLS) belum didukung; gunakan FTP biasa atau SFTP")
	}

	user := cfg.Username
	if user == "" {
		user = "anonymous"
	}
	pass := cfg.Password
	if pass == "" && user == "anonymous" {
		pass = "ingatin@localhost"
	}
	if err := c.command("USER "+user, 331, 230); err != nil {
		c.Close()
		return nil, err
	}
	if err := c.command("PASS "+pass, 230, 202); err != nil {
		c.Close()
		return nil, err
	}
	// TYPE I = binary (penting untuk dump).
	if err := c.command("TYPE I", 200); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// Close menutup koneksi kontrol (QUIT diabaikan bila gagal).
func (c *ftpClient) Close() {
	if c.conn == nil {
		return
	}
	_ = c.conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = c.conn.Write([]byte("QUIT\r\n"))
	_ = c.conn.Close()
	c.conn = nil
}

// readResponse membaca satu balasan FTP (mendukung balasan multi-baris "230-...").
func (c *ftpClient) readResponse() (int, string, error) {
	var code int
	var sb strings.Builder
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return 0, "", fmt.Errorf("baca balasan FTP: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if len(line) >= 3 {
			if n, err := strconv.Atoi(line[:3]); err == nil {
				code = n
			}
		}
		sb.WriteString(line)
		// Baris terakhir balasan multi-baris: "230 <teks>" (spasi pada posisi 4).
		if len(line) >= 4 && line[3] == ' ' {
			break
		}
		if len(line) < 4 {
			break
		}
	}
	return code, sb.String(), nil
}

// command mengirim perintah lalu memastikan kode balasan termasuk wanted.
func (c *ftpClient) command(cmd string, wanted ...int) error {
	if _, err := c.conn.Write([]byte(cmd + "\r\n")); err != nil {
		return fmt.Errorf("kirim perintah FTP: %w", err)
	}
	code, msg, err := c.readResponse()
	if err != nil {
		return err
	}
	for _, w := range wanted {
		if code == w {
			return nil
		}
	}
	return &ftpError{code: code, msg: msg}
}

// ensureDir membuat direktori tujuan (MKD), mengabaikan bila sudah ada.
func (c *ftpClient) ensureDir(dir string) error {
	dir = strings.Trim(strings.TrimSpace(dir), "/")
	if dir == "" {
		return nil
	}
	parts := strings.Split(dir, "/")
	path := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if path == "" {
			path = p
		} else {
			path += "/" + p
		}
		// Coba MKD; 257 = dibuat, 550 = kemungkinan sudah ada (abaikan).
		if _, err := c.conn.Write([]byte("MKD " + path + "\r\n")); err != nil {
			return err
		}
		code, msg, err := c.readResponse()
		if err != nil {
			return err
		}
		if code != 257 && code != 550 {
			return &ftpError{code: code, msg: msg}
		}
	}
	return nil
}

// openData membuka koneksi data pasif (PASV) untuk transfer.
func (c *ftpClient) openData(passive bool) (net.Conn, error) {
	mode := "PASV"
	if !passive {
		return nil, errors.New("mode PORT (aktif) belum didukung; gunakan PASV")
	}
	if _, err := c.conn.Write([]byte(mode + "\r\n")); err != nil {
		return nil, err
	}
	code, msg, err := c.readResponse()
	if err != nil {
		return nil, err
	}
	if code != 227 {
		return nil, &ftpError{code: code, msg: msg}
	}
	// Format: 227 Entering Passive Mode (h1,h2,h3,h4,p1,p2).
	open := strings.Index(msg, "(")
	closeP := strings.Index(msg, ")")
	if open < 0 || closeP <= open {
		return nil, errors.New("balasan PASV tidak dikenali")
	}
	nums := strings.Split(msg[open+1:closeP], ",")
	if len(nums) != 6 {
		return nil, errors.New("balasan PASV tidak lengkap")
	}
	ip := strings.Join(nums[0:4], ".")
	p1, _ := strconv.Atoi(strings.TrimSpace(nums[4]))
	p2, _ := strconv.Atoi(strings.TrimSpace(nums[5]))
	port := p1*256 + p2

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(port)), 15*time.Second)
	if err != nil {
		// Sebagian server mengirim alamat privat; coba host kontrol.
		host, _, herr := net.SplitHostPort(c.conn.RemoteAddr().String())
		if herr == nil {
			conn, err = net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 15*time.Second)
		}
		if err != nil {
			return nil, fmt.Errorf("buka koneksi data FTP: %w", err)
		}
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Minute))
	return conn, nil
}

// Upload mengirim reader ke remotePath (nama berkas di direktori cfg.Dir).
func (c *ftpClient) Upload(cfg FTPConfig, remotePath string, r io.Reader) error {
	if err := c.ensureDir(cfg.Dir); err != nil {
		return err
	}
	passive := true
	if !cfg.Passive {
		// default tetap PASV; PORT tidak didukung.
		passive = true
	}
	data, err := c.openData(passive)
	if err != nil {
		return err
	}

	target := remotePath
	if dir := strings.Trim(strings.TrimSpace(cfg.Dir), "/"); dir != "" {
		target = dir + "/" + remotePath
	}

	if _, err := c.conn.Write([]byte("STOR " + target + "\r\n")); err != nil {
		data.Close()
		return err
	}
	code, msg, err := c.readResponse()
	if err != nil {
		data.Close()
		return err
	}
	if code != 150 && code != 125 {
		data.Close()
		return &ftpError{code: code, msg: msg}
	}

	if _, err := io.Copy(data, r); err != nil {
		data.Close()
		return fmt.Errorf("kirim data FTP: %w", err)
	}
	if err := data.Close(); err != nil {
		return fmt.Errorf("tutup data FTP: %w", err)
	}

	code, msg, err = c.readResponse()
	if err != nil {
		return err
	}
	if code != 226 && code != 250 {
		return &ftpError{code: code, msg: msg}
	}
	return nil
}

// TestConnection membuka lalu menutup sesi FTP untuk memverifikasi kredensial.
func TestConnection(cfg FTPConfig) error {
	c, err := dialFTP(cfg)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.ensureDir(cfg.Dir); err != nil {
		return err
	}
	// CWD ke direktori tujuan bila ada, untuk memverifikasi akses.
	if dir := strings.Trim(strings.TrimSpace(cfg.Dir), "/"); dir != "" {
		if err := c.command("CWD "+dir, 250); err != nil {
			return fmt.Errorf("akses direktori %q gagal: %w", dir, err)
		}
	}
	return nil
}

// UploadFile membuka koneksi baru lalu mengunggah satu berkas.
func UploadFile(cfg FTPConfig, localPath, remoteName string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	c, err := dialFTP(cfg)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Upload(cfg, remoteName, f)
}
