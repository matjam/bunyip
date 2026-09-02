package network

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

// ErrCertificate is returned when a server's certificate does not match
// the fingerprint a PinnedConfig expects.
var ErrCertificate = errors.New("network: server certificate does not match the pinned fingerprint")

// ListenTLS starts a server like Listen with every connection encrypted
// and authenticated by cfg, which needs a certificate: see
// SelfSignedConfig for one that takes no setup.
func ListenTLS(addr string, reg *Registry, cfg *tls.Config) (*Server, error) {
	ln, err := tls.Listen("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("network: %w", err)
	}
	return newServer(ln, reg), nil
}

// DialTLS connects to a ListenTLS server. The handshake completes
// before it returns, so a certificate the client does not trust is an
// error here rather than a later Disconnected event.
func DialTLS(addr string, reg *Registry, cfg *tls.Config, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	nc, err := tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("network: %w", err)
	}
	return newClient(nc, reg), nil
}

// SelfSignedConfig makes a fresh certificate for a LAN game and returns
// a server config that uses it and a client config that accepts only
// it. The certificate lists hosts (names or IP addresses) for the
// benefit of ordinary TLS clients; "localhost", 127.0.0.1 and ::1 when
// none are given. The client config pins the certificate by fingerprint
// instead, so it works whatever address the server is reached at. To
// let another machine join, show Fingerprint(server) to the host and
// have the joiner build its config with PinnedConfig.
func SelfSignedConfig(hosts ...string) (server, client *tls.Config, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("network: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, fmt.Errorf("network: %w", err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "bunyip game"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if len(hosts) == 0 {
		hosts = []string{"localhost", "127.0.0.1", "::1"}
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("network: %w", err)
	}
	server = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		MinVersion:   tls.VersionTLS13,
	}
	return server, PinnedConfig(Fingerprint(server)), nil
}

// Fingerprint is the SHA-256 of a server config's first certificate as
// lower-case hex, or "" when it has none. A host shows it to players so
// they can join with PinnedConfig.
func Fingerprint(server *tls.Config) string {
	if len(server.Certificates) == 0 || len(server.Certificates[0].Certificate) == 0 {
		return ""
	}
	sum := sha256.Sum256(server.Certificates[0].Certificate[0])
	return hex.EncodeToString(sum[:])
}

// PinnedConfig returns a client config for DialTLS that trusts exactly
// one server certificate, the one whose Fingerprint is given, whatever
// name or address the server is dialled at. Case and any ':' or ' '
// separators in the fingerprint are ignored.
func PinnedConfig(fingerprint string) *tls.Config {
	want := strings.ToLower(strings.NewReplacer(":", "", " ", "").Replace(fingerprint))
	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, // replaced by the pin below
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return ErrCertificate
			}
			sum := sha256.Sum256(raw[0])
			if hex.EncodeToString(sum[:]) != want {
				return ErrCertificate
			}
			return nil
		},
	}
}
