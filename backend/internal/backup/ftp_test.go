package backup

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubFTPServer adalah server FTP minimal untuk menguji klien FTP:
// mendukung USER/PASS/TYPE/MKD/PASV/STOR/QUIT dan menyimpan berkas di memori.
type stubFTPServer struct {
	ln    net.Listener
	mu    sync.Mutex
	files map[string][]byte
	dirs  map[string]bool
}

func newStubFTP(t *testing.T) *stubFTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &stubFTPServer{ln: ln, files: map[string][]byte{}, dirs: map[string]bool{}}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *stubFTPServer) addr() (string, int) {
	host, port, _ := net.SplitHostPort(s.ln.Addr().String())
	p, _ := strconv.Atoi(port)
	return host, p
}

func (s *stubFTPServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *stubFTPServer) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	write := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
	write("220 stub ready")

	var dataLn net.Listener
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		parts := strings.SplitN(line, " ", 2)
		cmd := strings.ToUpper(parts[0])
		arg := ""
		if len(parts) > 1 {
			arg = parts[1]
		}
		switch cmd {
		case "USER":
			write("331 need password")
		case "PASS":
			write("230 logged in")
		case "TYPE":
			write("200 type set")
		case "MKD":
			s.mu.Lock()
			s.dirs[arg] = true
			s.mu.Unlock()
			write(fmt.Sprintf("257 %q created", arg))
		case "CWD":
			s.mu.Lock()
			ok := s.dirs[arg]
			s.mu.Unlock()
			if ok {
				write("250 cwd ok")
			} else {
				write("550 no such dir")
			}
		case "PASV":
			dl, derr := net.Listen("tcp", "127.0.0.1:0")
			if derr != nil {
				write("425 cannot open data")
				continue
			}
			dataLn = dl
			host, port, _ := net.SplitHostPort(dl.Addr().String())
			p, _ := strconv.Atoi(port)
			ip := strings.ReplaceAll(host, ".", ",")
			write(fmt.Sprintf("227 Entering Passive Mode (%s,%d,%d)", ip, p/256, p%256))
		case "STOR":
			if dataLn == nil {
				write("425 no data connection")
				continue
			}
			write("150 ok to send")
			dc, aerr := dataLn.Accept()
			if aerr != nil {
				write("426 data error")
				continue
			}
			var buf bytes.Buffer
			_, _ = buf.ReadFrom(dc)
			_ = dc.Close()
			_ = dataLn.Close()
			dataLn = nil
			s.mu.Lock()
			s.files[arg] = buf.Bytes()
			s.mu.Unlock()
			write("226 transfer complete")
		case "QUIT":
			write("221 bye")
			return
		default:
			write("502 not implemented")
		}
	}
}

func (s *stubFTPServer) get(name string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.files[name]
	return b, ok
}

func TestFTPUploadStoresFile(t *testing.T) {
	srv := newStubFTP(t)
	host, port := srv.addr()

	// Beri sedikit waktu agar listener siap.
	time.Sleep(50 * time.Millisecond)

	cfg := FTPConfig{Host: host, Port: port, Username: "u", Password: "p", Dir: "ingatin/backups", Passive: true}

	// Uji koneksi & pembuatan direktori.
	if err := TestConnection(cfg); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}

	// Unggah berkas dari disk.
	dir := t.TempDir()
	localPath := dir + "/sample.dump"
	content := []byte("PGDMP-fake-content")
	if err := os.WriteFile(localPath, content, 0o600); err != nil {
		t.Fatalf("tulis berkas: %v", err)
	}

	if err := UploadFile(cfg, localPath, "sample.dump"); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	got, ok := srv.get("ingatin/backups/sample.dump")
	if !ok {
		t.Fatalf("berkas tidak tersimpan di server (files=%v)", srv.files)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("isi berkas tidak sama: %q", got)
	}
}
