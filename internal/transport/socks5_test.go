package transport

import (
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"soapweb/internal/model"
)

// startSOCKS5 runs a minimal SOCKS5 server (CONNECT only) that optionally
// requires username/password authentication. It proxies to the real target.
func startSOCKS5(t *testing.T, wantUser, wantPass string) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleSOCKS5(conn, wantUser, wantPass)
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port
}

func handleSOCKS5(c net.Conn, wantUser, wantPass string) {
	defer c.Close()

	// Greeting: VER, NMETHODS, METHODS
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(c, hdr); err != nil || hdr[0] != 0x05 {
		return
	}
	methods := make([]byte, int(hdr[1]))
	if _, err := io.ReadFull(c, methods); err != nil {
		return
	}

	requireAuth := wantUser != "" || wantPass != ""
	if requireAuth {
		// Only offer username/password (0x02).
		if _, err := c.Write([]byte{0x05, 0x02}); err != nil {
			return
		}
		// Auth sub-negotiation: VER(1) ULEN UNAME PLEN PASSWD
		ah := make([]byte, 2)
		if _, err := io.ReadFull(c, ah); err != nil || ah[0] != 0x01 {
			return
		}
		user := make([]byte, int(ah[1]))
		if _, err := io.ReadFull(c, user); err != nil {
			return
		}
		pl := make([]byte, 1)
		if _, err := io.ReadFull(c, pl); err != nil {
			return
		}
		pass := make([]byte, int(pl[0]))
		if _, err := io.ReadFull(c, pass); err != nil {
			return
		}
		if string(user) != wantUser || string(pass) != wantPass {
			_, _ = c.Write([]byte{0x01, 0x01})
			return
		}
		if _, err := c.Write([]byte{0x01, 0x00}); err != nil {
			return
		}
	} else {
		if _, err := c.Write([]byte{0x05, 0x00}); err != nil {
			return
		}
	}

	// Request: VER CMD RSV ATYP DST.ADDR DST.PORT
	req := make([]byte, 4)
	if _, err := io.ReadFull(c, req); err != nil {
		return
	}
	if req[0] != 0x05 || req[1] != 0x01 { // CONNECT only
		return
	}
	var host string
	switch req[3] {
	case 0x01: // IPv4
		b := make([]byte, 4)
		if _, err := io.ReadFull(c, b); err != nil {
			return
		}
		host = net.IP(b).String()
	case 0x03: // domain
		l := make([]byte, 1)
		if _, err := io.ReadFull(c, l); err != nil {
			return
		}
		b := make([]byte, int(l[0]))
		if _, err := io.ReadFull(c, b); err != nil {
			return
		}
		host = string(b)
	case 0x04: // IPv6
		b := make([]byte, 16)
		if _, err := io.ReadFull(c, b); err != nil {
			return
		}
		host = net.IP(b).String()
	default:
		return
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(c, pb); err != nil {
		return
	}
	target := net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(pb))))

	dst, err := net.Dial("tcp", target)
	if err != nil {
		_, _ = c.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer dst.Close()
	// Success reply.
	if _, err := c.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}

	go func() { _, _ = io.Copy(dst, c) }()
	_, _ = io.Copy(c, dst)
}

func TestSendViaSOCKS5NoAuth(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("socks-ok"))
	}))
	defer target.Close()

	host, port := startSOCKS5(t, "", "")
	resp := Send(model.SendRequest{URL: target.URL, Method: "GET", TimeoutMS: 5000},
		&model.ProxySettings{Mode: "custom", Type: "socks5", Host: host, Port: port})
	if resp.Error != "" {
		t.Fatalf("socks5 request failed: %s", resp.Error)
	}
	if resp.Body != "socks-ok" {
		t.Errorf("body = %q", resp.Body)
	}
}

func TestSendViaSOCKS5WithAuth(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("socks-auth-ok"))
	}))
	defer target.Close()

	host, port := startSOCKS5(t, "user1", "pass1")
	resp := Send(model.SendRequest{URL: target.URL, Method: "GET", TimeoutMS: 5000},
		&model.ProxySettings{Mode: "custom", Type: "socks5", Host: host, Port: port, Username: "user1", Password: "pass1"})
	if resp.Error != "" {
		t.Fatalf("socks5 auth request failed: %s", resp.Error)
	}
	if resp.Body != "socks-auth-ok" {
		t.Errorf("body = %q", resp.Body)
	}
}
