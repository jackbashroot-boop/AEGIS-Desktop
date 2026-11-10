package main

import (
	"crypto/hmac"
	"crypto/rand"
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

	// Active ping (v1.7.9): every ~15 s we broadcast AEGIS_PING and
	// Cardputers holding the SD link file answer AEGIS_CARD with
	// prog/fw/mac + an HMAC proof, so the Dashboard can FIND and
	// name devices before any link exists. Finding is not linking:
	// the on-device ENTER confirmation still gates connections.
	found     map[string]*FoundDevice
	pingNonce string
	pingAt    time.Time

	// Recent ping nonces (v1.8.6): a device may answer the previous
	// shout just after we rotated to a new one; accepting only the
	// newest nonce silently dropped those honest replies.
	nonces []pingNonceRec
	// Rejected AEGIS_CARD diagnostics (v1.8.6): proof failures used
	// to vanish silently. Counted so the Dashboard can say WHY no
	// device is showing (usually a stale token on the SD card).
	cardRejects int
	rejectIP    string
	rejectAt    time.Time
}

type pingNonceRec struct {
	nonce string
	at    time.Time
}

// FoundDevice is one AEGIS device this Companion has located on
// the LAN (by answering its shout, or by replying to our ping).
type FoundDevice struct {
	Prog     string // "" until a ping answer names it
	Ver      string
	MAC      string
	IP       string
	LastSeen time.Time
}

func NewDiscovery(token string, log *Log) *Discovery {
	return &Discovery{token: token, log: log, lastBy: map[string]time.Time{}, seen: map[string]bool{}, found: map[string]*FoundDevice{}}
}

// SetToken swaps the shared secret live (v1.8.6). Discovery used to
// keep the token from process start forever: regenerate the token
// in Settings and every ping proof silently failed from then on -
// devices still linked (the TCP server reads the live config) and
// pushed updates fine, but "On your network" stayed empty forever.
func (d *Discovery) SetToken(t string) {
	d.mu.Lock()
	d.token = t
	d.mu.Unlock()
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
	go d.pinger(conn, d.done)
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

// pingMAC proves an AEGIS_CARD answer: HMAC of the ping nonce,
// keyed by the shared token (the firmware computes the same).
func pingMAC(token, nonce string) string {
	m := hmac.New(sha256.New, []byte(token))
	m.Write([]byte("AEGIS_PING:" + nonce))
	return hex.EncodeToString(m.Sum(nil))
}

// parseCard validates an AEGIS_CARD answer packet.
func parseCard(pkt []byte) (prog, ver, mac, proof string, ok bool) {
	f := strings.Fields(string(pkt))
	if len(f) != 6 || f[0] != "AEGIS_CARD" || f[1] != "1" {
		return "", "", "", "", false
	}
	return f[2], f[3], f[4], f[5], true
}

// proofOK reports whether an AEGIS_CARD proof matches any ping
// nonce still inside its 90 s validity window (current or recent).
func (d *Discovery) proofOK(proof string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pingNonce != "" && time.Since(d.pingAt) < 90*time.Second && proof == pingMAC(d.token, d.pingNonce) {
		return true
	}
	for _, n := range d.nonces {
		if time.Since(n.at) < 90*time.Second && proof == pingMAC(d.token, n.nonce) {
			return true
		}
	}
	return false
}

// recordReject counts an AEGIS_CARD whose proof failed, so the UI
// can explain an empty "On your network" list instead of hiding it.
func (d *Discovery) recordReject(ip string) {
	d.mu.Lock()
	d.cardRejects++
	n := d.cardRejects
	d.rejectIP, d.rejectAt = ip, time.Now()
	d.mu.Unlock()
	if n == 1 || n%25 == 0 {
		d.log.Warnf("discovery: AEGIS_CARD from %s REJECTED (security proof mismatch - token on its SD card differs from this PC's? re-write AEGIS_LINK from Settings > Connection) [%d total]", ip, n)
	}
}

// Rejects returns the count and source of the latest rejected
// AEGIS_CARD announcement (0 when none).
func (d *Discovery) Rejects() (int, string, time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cardRejects, d.rejectIP, d.rejectAt
}

func (d *Discovery) noteFound(ip, prog, ver, mac string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fd := d.found[ip]
	if fd == nil {
		fd = &FoundDevice{IP: ip}
		d.found[ip] = fd
	}
	if prog != "" {
		known := fd.Prog
		fd.Prog, fd.Ver, fd.MAC = prog, ver, mac
		if known == "" {
			d.log.Infof("discovery: found %s (fw %s) at %s via active ping", prog, ver, ip)
		}
	}
	fd.LastSeen = time.Now()
}

// Found lists devices located in the last 5 minutes, freshest first.
func (d *Discovery) Found() []FoundDevice {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]FoundDevice, 0, len(d.found))
	for ip, fd := range d.found {
		if time.Since(fd.LastSeen) > 5*time.Minute {
			delete(d.found, ip)
			continue
		}
		out = append(out, *fd)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].LastSeen.After(out[j-1].LastSeen); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
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
		if prog, ver, mac, proof, ok := parseCard(buf[:n]); ok {
			if d.proofOK(proof) {
				d.noteFound(from.IP.String(), prog, ver, mac)
			} else {
				d.recordReject(from.IP.String())
			}
			continue
		}
		nonce, ok := parseDiscovery(buf[:n])
		if !ok {
			continue
		}
		d.noteFound(from.IP.String(), "", "", "")
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

// pingBroadcastAddrs returns every broadcast target worth shouting
// on: the limited broadcast plus each up interface's subnet
// broadcast. A lone 255.255.255.255 shout quietly dies on some
// Windows / multi-homed / VPN setups, and then the Companion swears
// no Cardputer exists (v1.8.5 link hardening).
func pingBroadcastAddrs() []*net.UDPAddr {
	seen := map[string]bool{}
	var out []*net.UDPAddr
	add := func(ip net.IP) {
		if ip == nil {
			return
		}
		s := ip.String()
		if seen[s] {
			return
		}
		seen[s] = true
		out = append(out, &net.UDPAddr{IP: ip, Port: 8791})
	}
	add(net.IPv4bcast)
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil || ipnet.Mask == nil {
				continue
			}
			mask4 := ipnet.Mask
			if len(mask4) == 16 {
				mask4 = mask4[12:]
			}
			if len(mask4) != 4 {
				continue
			}
			bc := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				bc[i] = ip4[i] | ^mask4[i]
			}
			add(bc)
		}
	}
	return out
}

// pinger broadcasts AEGIS_PING every 15 s so link-carrying
// Cardputers can announce themselves (see noteFound).
func (d *Discovery) pinger(conn *net.UDPConn, done chan struct{}) {
	send := func() {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return
		}
		nonce := hex.EncodeToString(raw[:])
		d.mu.Lock()
		d.pingNonce, d.pingAt = nonce, time.Now()
		d.nonces = append(d.nonces, pingNonceRec{nonce, time.Now()})
		kept := d.nonces[:0]
		for _, n := range d.nonces {
			if time.Since(n.at) < 2*time.Minute {
				kept = append(kept, n)
			}
		}
		d.nonces = kept
		d.mu.Unlock()
		payload := []byte("AEGIS_PING 1 " + nonce + "\n")
		for _, addr := range pingBroadcastAddrs() {
			_, _ = conn.WriteToUDP(payload, addr)
		}
	}
	send()
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			send()
		}
	}
}
