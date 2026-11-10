package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// Discovery lets a Cardputer find this computer on the home network
// even if its DHCP address changed since the SD card was written.
// The device broadcasts "AEGIS_DISCOVER 1 <nonce>"; we answer with
// an HMAC of that nonce keyed by the shared token, so a device can
// verify the answer really came from its companion (and ignore any
// other machine that happens to answer). The new address is then
// written back into the card's desktop.txt by the device itself.
type Discovery struct {
	mu     sync.Mutex
	conn   *net.UDPConn
	port   int
	token  string
	log    *Log
	lastBy map[string]time.Time // sender IP -> last discovery seen
	seen   map[string]bool      // sender IPs already logged once
	done   chan struct{}
}

func NewDiscovery(token string, log *Log) *Discovery {
	return &Discovery{token: token, log: log, lastBy: map[string]time.Time{}, seen: map[string]bool{}}
}

// discoveryMAC is the single source of truth for the reply proof;
// the firmware computes the same value with its mbedtls HMAC.
func discoveryMAC(token, nonce string) string {
	m := hmac.New(sha256.New, []byte(token))
	m.Write([]byte("AEGIS_DISCOVER:" + nonce))
	return hex.EncodeToString(m.Sum(nil))
}

func validNonce(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// parseDiscovery validates a discovery packet, returning the nonce.
func parseDiscovery(pkt []byte) (string, bool) {
	f := strings.Fields(string(pkt))
	if len(f) != 3 || f[0] != "AEGIS_DISCOVER" || f[1] != "1" || !validNonce(f[2]) {
		return "", false
	}
	return f[2], true
}

func discoveryReply(token string, tcpPort int, nonce string) string {
	return fmt.Sprintf("AEGIS_HERE 1 %d %s\n", tcpPort, discoveryMAC(token, nonce))
}

func (d *Discovery) Start(port int) error {
	d.Stop()
	addr := &net.UDPAddr{IP: net.IPv4zero, Port: port}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.conn, d.port, d.done = conn, port, make(chan struct{})
	d.mu.Unlock()
	go d.loop(conn, d.done)
	d.log.Infof("discovery responder on udp :%d - Cardputers can find this computer automatically", port)
	return nil
}

func (d *Discovery) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn != nil {
		close(d.done)
		d.conn.Close()
		d.conn = nil
	}
}

func (d *Discovery) Running() (int, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.port, d.conn != nil
}

// LastSeen returns when any device last went looking for us.
func (d *Discovery) Seekers() map[string]time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[string]time.Time, len(d.lastBy))
	for k, v := range d.lastBy {
		out[k] = v
	}
	return out
}

func (d *Discovery) loop(conn *net.UDPConn, done chan struct{}) {
	buf := make([]byte, 256)
	for {
		select {
		case <-done:
			return
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-done:
				return
			default:
				continue
			}
		}
		nonce, ok := parseDiscovery(buf[:n])
		if !ok {
			continue
		}
		ip := from.IP.String()
		d.mu.Lock()
		if prev, seen := d.lastBy[ip]; seen && time.Since(prev) < time.Second {
			d.mu.Unlock()
			continue // rate limit: one answer per second per sender
		}
		first := !d.seen[ip]
		d.lastBy[ip] = time.Now()
		d.seen[ip] = true
		port := d.port
		d.mu.Unlock()
		if first {
			d.log.Infof("discovery: a Cardputer at %s is looking for this computer - answering", ip)
		}
		_, _ = conn.WriteToUDP([]byte(discoveryReply(d.token, port, nonce)), from)
	}
}
