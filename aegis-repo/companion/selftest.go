package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// runSelfTest exercises the shipped binary end to end on loopback with a
// scratch library: a legacy-framing file sync (exactly what SCRIBE v0.1.3
// sends), a PHOENIX live telemetry stream, a bad-token attempt, and a
// traversal attempt. Exit-code friendly; safe to run any time.
func runSelfTest(core *Core) (okOut bool) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("SELFTEST FAIL panic:", r)
			okOut = false
		}
	}()
	scratch, err := os.MkdirTemp("", "aegis-selftest-*")
	if err != nil {
		fmt.Println("SELFTEST cannot make scratch dir:", err)
		return false
	}
	defer os.RemoveAll(scratch)

	cfg := &Config{Port: 0, Token: core.Cfg.Token, Library: filepath.Join(scratch, "Library")}
	log := NewLog()
	store := NewStore(cfg.Library, log)
	live := NewLiveRegistry()
	srv := NewServer(cfg, log, live, store)
	if err := srv.Start(0); err != nil {
		fmt.Println("SELFTEST FAIL listener:", err)
		return false
	}
	defer srv.Stop()
	port, _ := srv.Listening()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ok := true
	pass := func(name string, good bool, detail string) {
		status := "PASS"
		if !good {
			status = "FAIL"
			ok = false
		}
		fmt.Printf("SELFTEST %s %s %s\n", status, name, detail)
	}

	dial := func() (net.Conn, *bufio.Reader) {
		c, err := net.DialTimeout("tcp", addr, 3*time.Second)
		if err != nil {
			return nil, nil
		}
		return c, bufio.NewReader(c)
	}
	readJSON := func(r *bufio.Reader) map[string]any {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil
		}
		m, _ := parseMsg(strings.TrimSpace(line))
		return m
	}
	auth := func(c net.Conn, r *bufio.Reader, prog, ver string) bool {
		hello := readJSON(r)
		nonce, _ := hello["nonce"].(string)
		sendJSON(c, map[string]any{"auth": hmacHex(cfg.Token, nonce), "prog": prog, "ver": ver})
		resp := readJSON(r)
		return resp != nil && resp["ok"] == true
	}

	// 1. legacy file sync, odd-sized chunks like the firmware's 3072-byte reads
	{
		c, r := dial()
		good := c != nil && auth(c, r, "SCRIBE", "0.1.3")
		payload := buildChainPayload()
		sum := sha256Hex(payload)
		sendJSON(c, map[string]any{"file": "event_index.jsonl", "size": len(payload), "sha256": sum})
		resp := readJSON(r)
		good = good && resp != nil && resp["send"] == true
		for off := 0; off < len(payload); off += 1000 {
			end := off + 1000
			if end > len(payload) {
				end = len(payload)
			}
			sendJSON(c, map[string]any{"data": base64.StdEncoding.EncodeToString(payload[off:end])})
		}
		sendJSON(c, map[string]any{"eof": true})
		resp = readJSON(r)
		good = good && resp != nil && resp["stored"] == true
		sendJSON(c, map[string]any{"done": true})
		resp = readJSON(r)
		good = good && resp != nil && resp["bye"] == true
		c.Close()
		storedPath := filepath.Join(cfg.Library, time.Now().Format("2006-01-02"), "SCRIBE", "raw", "event_index.jsonl")
		data, err := os.ReadFile(storedPath)
		good = good && err == nil && string(data) == string(payload)
		pass("legacy sync (SCRIBE v0.1.3 framing)", good, storedPath)
	}

	// 2. reports built with chain verdict intact
	{
		store.Rebuild()
		found := false
		for _, dp := range store.Summaries() {
			if dp.Prog == "SCRIBE" {
				v := dp.Chains["event_index.jsonl"]
				found = v.Chained == 2 && v.Bad == 0 && len(dp.Findings) == 2
			}
		}
		pass("report + FNV chain verify", found, "")
	}

	// 3. live telemetry stream
	{
		c, r := dial()
		good := c != nil && auth(c, r, "PHOENIX", "0.9.3")
		sendJSON(c, map[string]any{"live_start": true})
		resp := readJSON(r)
		good = good && resp != nil && resp["live_ok"] == true
		for i, aps := range []int{9, 14, 11} {
			sendJSON(c, map[string]any{"type": "live", "state": "scanning", "detail": "Wi-Fi scan",
				"aps": aps, "batt": 87 - i, "events": 3, "last": "new_ap"})
		}
		deadline := time.Now().Add(2 * time.Second)
		var d DeviceLive
		var got bool
		for time.Now().Before(deadline) {
			d, got = live.Get("PHOENIX")
			if got && d.APs == 11 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		good = good && got && d.State == "scanning" && d.Batt == 85 && d.Frames == 3
		pass("live dashboard stream (PHOENIX)", good, fmt.Sprintf("state=%s aps=%d", d.State, d.APs))
		c.Close()
	}

	// 4. bad token rejected
	{
		c, r := dial()
		hello := readJSON(r)
		nonce, _ := hello["nonce"].(string)
		sendJSON(c, map[string]any{"auth": hmacHex("wrong-token", nonce), "prog": "PHOENIX"})
		resp := readJSON(r)
		pass("bad token rejected", resp != nil && resp["ok"] == false, "")
		c.Close()
	}

	// 5. traversal contained inside the raw folder
	{
		c, r := dial()
		good := auth(c, r, "HEARTH", "0.1.4")
		payload := []byte("hello")
		sendJSON(c, map[string]any{"file": "../../evil.txt", "size": len(payload), "sha256": sha256Hex(payload)})
		resp := readJSON(r)
		good = good && resp != nil && resp["send"] == true
		sendJSON(c, map[string]any{"data": base64.StdEncoding.EncodeToString(payload)})
		sendJSON(c, map[string]any{"eof": true})
		resp = readJSON(r)
		good = good && resp != nil && resp["stored"] == true
		sendJSON(c, map[string]any{"done": true})
		readJSON(r)
		c.Close()
		_, errOutside := os.Stat(filepath.Join(scratch, "evil.txt"))
		inside := filepath.Join(cfg.Library, time.Now().Format("2006-01-02"), "HEARTH", "raw", "evil.txt")
		_, errInside := os.Stat(inside)
		pass("path traversal contained", good && os.IsNotExist(errOutside) && errInside == nil, "")
	}

	// 6. discovery responder: a device that lost our address finds us
	//    again, and can prove the answer is really from its companion.
	{
		disco := NewDiscovery(cfg.Token, log)
		derr := disco.Start(port)
		good := derr == nil
		if good {
			defer disco.Stop()
			nonce := "0123456789abcdeffedcba9876543210"
			uc, err := net.DialTimeout("udp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
			good = err == nil
			if good {
				defer uc.Close()
				_, err = uc.Write([]byte("AEGIS_DISCOVER 1 " + nonce + "\n"))
				good = err == nil
			}
			if good {
				buf := make([]byte, 256)
				_ = uc.SetReadDeadline(time.Now().Add(2 * time.Second))
				n, err := uc.Read(buf)
				good = err == nil && n > 0
				if good {
					want := "AEGIS_HERE 1 " + fmt.Sprint(port) + " " + discoveryMAC(cfg.Token, nonce)
					good = strings.TrimSpace(string(buf[:n])) == want &&
						discoveryMAC("some-other-token", nonce) != discoveryMAC(cfg.Token, nonce)
				}
			}
			// and garbage is simply ignored (no reply within 300 ms)
			if good {
				uc2, err := net.DialTimeout("udp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
				if err == nil {
					defer uc2.Close()
					_, _ = uc2.Write([]byte("AEGIS_DISCOVER 1 nothex\n"))
					buf := make([]byte, 64)
					_ = uc2.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
					_, rerr := uc2.Read(buf)
					good = rerr != nil
				}
			}
		}
		pass("discovery responder + HMAC proof", good, "")
	}

	fmt.Println("SELFTEST", map[bool]string{true: "ALL PASS", false: "FAILURES PRESENT"}[ok])
	return ok
}

func buildChainPayload() []byte {
	// Exact family format: body fields, then ,"chain":"xxxxxxxx" before }.
	return []byte("{\"kind\":\"boot\",\"n\":1,\"chain\":\"6d854838\"}\n{\"kind\":\"new_ap\",\"n\":2,\"chain\":\"43bdfb22\"}\n")
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
