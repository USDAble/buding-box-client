package tunnel

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"

	"github.com/open-octo/octo-agent/internal/datapath"
)

// DefaultRelayURL is the existing managed relay used by the CLI and desktop.
// OCTO-FORK: keep one relay choice for both entry points until a product relay is configured.
const DefaultRelayURL = "wss://relay.octo.dev"

type Pairing struct {
	URL      string
	Relay    string
	TunnelID string
	HostKey  string
	Token    string
}

// NewHost prepares the same tunnel for CLI and desktop without starting a
// second HTTP server. The caller owns its Serve context and pairing lifetime.
func NewHost(addr, relayURL, accessKey string, onState func(bool, error)) (*Tunnel, Pairing, error) {
	u, err := url.Parse(relayURL)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") {
		return nil, Pairing{}, fmt.Errorf("--relay must be a ws:// or wss:// URL (got %q)", relayURL)
	}
	idPath, err := datapath.Join("tunnel.json")
	if err != nil {
		return nil, Pairing{}, err
	}
	identity, err := LoadOrCreateIdentity(idPath)
	if err != nil {
		return nil, Pairing{}, err
	}
	token, err := NewPairToken()
	if err != nil {
		return nil, Pairing{}, err
	}
	pairing := Pairing{
		Relay:    relayURL,
		TunnelID: identity.TunnelID(),
		HostKey:  identity.PublicKeyBase64(),
		Token:    token,
	}
	q := url.Values{"relay": {relayURL}, "tid": {pairing.TunnelID}, "hk": {pairing.HostKey}, "tok": {token}}
	pairing.URL = "octo-pair://v1?" + q.Encode()
	tun, err := New(Config{
		RelayURL:      relayURL,
		TunnelID:      pairing.TunnelID,
		PairTokens:    []string{token},
		LoopbackURL:   LoopbackWSURL(addr),
		AccessKey:     accessKey,
		Identity:      identity,
		OnStateChange: onState,
	})
	if err != nil {
		return nil, Pairing{}, err
	}
	return tun, pairing, nil
}

func NewPairToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func LoopbackWSURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = "127.0.0.1", "8088"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "ws://" + net.JoinHostPort(host, port) + "/ws"
}
