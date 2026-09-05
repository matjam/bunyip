package network

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTLS(t *testing.T) {
	server, client, err := SelfSignedConfig("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	reg := registry()
	srv, err := ListenTLS("127.0.0.1:0", reg, server)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	cl, err := DialTLS(srv.Addr(), reg, client, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	var clientActivity, serverActivity atomic.Int32
	cl.SetOnActivity(func() { clientActivity.Add(1) })
	if clientActivity.Load() != 1 {
		t.Fatal("TLS connected event did not wake late registration")
	}
	srv.SetOnActivity(func() { serverActivity.Add(1) })
	if err := cl.Send(hello{"eve-proof"}); err != nil {
		t.Fatal(err)
	}
	var conn *Conn
	wait(t, "message over tls", func() bool {
		for _, ev := range srv.Poll() {
			if h, ok := ev.Msg.(*hello); ok && h.Name == "eve-proof" {
				conn = ev.Conn
				return true
			}
		}
		return false
	})
	conn.Send(move{5, 6})
	wait(t, "TLS server activity", func() bool { return serverActivity.Load() > 0 })
	wait(t, "reply over tls", func() bool {
		for _, ev := range cl.Poll() {
			if m, ok := ev.Msg.(*move); ok && m.X == 5 {
				return true
			}
		}
		return false
	})

	// A client pinned to another certificate is refused at dial time.
	fp := Fingerprint(server)
	if len(fp) != 64 {
		t.Fatalf("fingerprint %q", fp)
	}
	other := strings.Repeat("00", 32)
	if _, err := DialTLS(srv.Addr(), reg, PinnedConfig(other), time.Second); !errors.Is(err, ErrCertificate) {
		t.Fatalf("wrong pin dialled: %v", err)
	}
	// Separators and case in a typed fingerprint are forgiven.
	typed := strings.ToUpper(fp[:8] + ":" + fp[8:])
	cl2, err := DialTLS(srv.Addr(), reg, PinnedConfig(typed), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	cl2.Close()
}
