package main

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestDiscoveryMACVectors(t *testing.T) {
	// The firmware computes this with mbedtls HMAC-SHA256 over the
	// exact same string; lock the format down.
	got := discoveryMAC("testtoken0123456789abcdef", "00112233445566778899aabbccddeeff")
	mac := hmacHex("testtoken0123456789abcdef", "AEGIS_DISCOVER:00112233445566778899aabbccddeeff")
	if got != mac {
		t.Fatalf("discoveryMAC = %s, want %s", got, mac)
	}
	if discoveryMAC("other", "00112233445566778899aabbccddeeff") == got {
		t.Fatal("different token must give different MAC")
	}
}

func TestParseDiscovery(t *testing.T) {
	if _, ok := parseDiscovery([]byte("AEGIS_DISCOVER 1 0123456789abcdeffedcba9876543210\n")); !ok {
		t.Fatal("valid packet rejected")
	}
	for _, bad := range []string{
		"AEGIS_DISCOVER 1 xyz",
		"AEGIS_DISCOVER 2 0123456789abcdeffedcba9876543210",
		"AEGIS_HERE 1 0123456789abcdeffedcba9876543210",
		"AEGIS_DISCOVER 1 0123456789ABCDEFFEDCBA9876543210", // uppercase hex: devices send lowercase
		"AEGIS_DISCOVER 1 0123456789abcdeffedcba987654321",  // 31 chars
		"",
	} {
		if _, ok := parseDiscovery([]byte(bad)); ok {
			t.Fatalf("bad packet accepted: %q", bad)
		}
	}
}

func TestDiscoveryRoundtrip(t *testing.T) {
	log := NewLog()
	d := NewDiscovery("tok-0123456789abcdef", log)
	if err := d.Start(0); err != nil {
		// port 0: UDP binds an ephemeral port; learn it from the conn
		t.Fatalf("start: %v", err)
	}
	defer d.Stop()
	d.mu.Lock()
	udpPort := d.conn.LocalAddr().(*net.UDPAddr).Port
	d.mu.Unlock()
	// advertise the "TCP port" as 8791
	d.mu.Lock()
	d.port = 8791
	d.mu.Unlock()

	nonce := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	c, err := net.DialTimeout("udp", fmt.Sprintf("127.0.0.1:%d", udpPort), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("AEGIS_DISCOVER 1 " + nonce + "\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := c.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	want := "AEGIS_HERE 1 8791 " + discoveryMAC("tok-0123456789abcdef", nonce)
	if strings.TrimSpace(string(buf[:n])) != want {
		t.Fatalf("reply = %q, want %q", buf[:n], want)
	}
	if s := d.Seekers(); len(s) != 1 {
		t.Fatalf("seekers = %v", s)
	}
}
