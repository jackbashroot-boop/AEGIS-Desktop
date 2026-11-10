package main

import (
	"net"
	"testing"
	"time"
)

// Active ping (v1.7.9): an AEGIS_CARD answer is accepted only with
// the HMAC proof keyed by OUR token over the CURRENT ping nonce.
func TestPingCardAcceptReject(t *testing.T) {
	d := NewDiscovery("secret-token", NewLog())
	nonce := "0123456789abcdef0123456789abcdef"
	d.pingNonce, d.pingAt = nonce, time.Now()

	good := []byte("AEGIS_CARD 1 PHOENIX 0.12.2 ABCDEF012345 " + pingMAC("secret-token", nonce) + "\n")
	prog, ver, mac, proof, ok := parseCard(good)
	if !ok || prog != "PHOENIX" || ver != "0.12.2" || mac != "ABCDEF012345" {
		t.Fatalf("parseCard good: %q %q %q ok=%v", prog, ver, mac, ok)
	}
	if proof != pingMAC(d.token, d.pingNonce) {
		t.Fatal("proof must verify against the current ping nonce")
	}
	if pingMAC("wrong-token", nonce) == proof {
		t.Fatal("a foreign token must not produce a valid proof")
	}
	if _, _, _, _, ok := parseCard([]byte("AEGIS_CARD 1 PHOENIX\n")); ok {
		t.Fatal("truncated card accepted")
	}
	d.noteFound("192.0.2.7", prog, ver, mac)
	d.noteFound("192.0.2.8", "", "", "") // anonymous DISCOVER sighting
	found := d.Found()
	if len(found) != 2 {
		t.Fatalf("Found() = %+v", found)
	}
	byIP := map[string]FoundDevice{}
	for _, fd := range found {
		byIP[fd.IP] = fd
	}
	if byIP["192.0.2.7"].Prog != "PHOENIX" || byIP["192.0.2.8"].Prog != "" {
		t.Fatalf("Found() = %+v", found)
	}
	_ = net.IPv4bcast
}
