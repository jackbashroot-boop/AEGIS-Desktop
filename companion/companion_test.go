package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------- shared harness ----------

type testEnv struct {
	addr  string
	cfg   *Config
	store *Store
	live  *LiveRegistry
	srv   *Server
	dir   string
}

func startTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	cfg := &Config{Port: 0, Token: "testtoken0123456789abcdef", Library: filepath.Join(dir, "Library")}
	log := NewLog()
	store := NewStore(cfg.Library, log)
	live := NewLiveRegistry()
	srv := NewServer(cfg, log, live, store)
	srv.IdleTimeout = 10 * time.Second
	if err := srv.Start(0); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(srv.Stop)
	port, _ := srv.Listening()
	return &testEnv{addr: fmt.Sprintf("127.0.0.1:%d", port), cfg: cfg, store: store, live: live, srv: srv, dir: dir}
}

type client struct {
	t    *testing.T
	conn net.Conn
	r    *bufio.Reader
}

func dial(t *testing.T, addr string) *client {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return &client{t: t, conn: c, r: bufio.NewReader(c)}
}

func (c *client) send(obj map[string]any) {
	c.t.Helper()
	if err := sendJSON(c.conn, obj); err != nil {
		c.t.Fatalf("send: %v", err)
	}
}

func (c *client) sendRaw(s string) {
	c.t.Helper()
	if _, err := c.conn.Write([]byte(s)); err != nil {
		c.t.Fatalf("sendRaw: %v", err)
	}
}

func (c *client) read() map[string]any {
	c.t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := c.r.ReadString('\n')
	if err != nil {
		c.t.Fatalf("read: %v (got %q)", err, line)
	}
	m, err := parseMsg(strings.TrimSpace(line))
	if err != nil {
		c.t.Fatalf("parse %q: %v", line, err)
	}
	return m
}

func (c *client) auth(prog, ver string) bool {
	c.t.Helper()
	hello := c.read()
	nonce, _ := hello["nonce"].(string)
	if hello["hello"] != "AEGIS/1" || nonce == "" {
		c.t.Fatalf("bad hello: %v", hello)
	}
	c.send(map[string]any{"auth": hmacHex(testToken, nonce), "prog": prog, "ver": ver})
	return c.read()["ok"] == true
}

const testToken = "testtoken0123456789abcdef"

func (c *client) pushFile(name string, payload []byte, announceSize int64, sha string) map[string]any {
	c.t.Helper()
	c.send(map[string]any{"file": name, "size": announceSize, "sha256": sha})
	resp := c.read()
	if resp["send"] != true {
		return resp
	}
	for off := 0; off < len(payload); off += 3072 { // firmware chunk size
		end := off + 3072
		if end > len(payload) {
			end = len(payload)
		}
		c.send(map[string]any{"data": base64.StdEncoding.EncodeToString(payload[off:end])})
	}
	c.send(map[string]any{"eof": true})
	return c.read()
}

func (c *client) done() map[string]any {
	c.t.Helper()
	c.send(map[string]any{"done": true})
	return c.read()
}

// findStored locates a stored raw file without assuming which dated
// folder the session landed in (a sync can straddle midnight).
func findStored(t *testing.T, env *testEnv, prog, name string) string {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(env.cfg.Library, "*", prog, "raw", name))
	if len(matches) != 1 {
		t.Fatalf("stored %s/%s: %d matches %v", prog, name, len(matches), matches)
	}
	return matches[0]
}

func shaOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ---------- unit vectors (locked against the Python implementation) ----

func TestHMACVectorPythonParity(t *testing.T) {
	got := hmacHex("testtoken0123456789abcdef", "00112233445566778899aabbccddeeff")
	want := "396008cfde85fd36a17cb20c5c1aaea6e68f36f36f5cb291e14acaf8660eed16"
	if got != want {
		t.Fatalf("hmac = %s, want %s", got, want)
	}
}

func TestChainVectorsPythonParity(t *testing.T) {
	lines := []string{
		`{"kind":"boot","n":1,"chain":"6d854838"}`,
		`{"kind":"new_ap","n":2,"chain":"43bdfb22"}`,
	}
	v := verifyChainLines(lines)
	if v.Chained != 2 || v.Bad != 0 || v.Unchained != 0 {
		t.Fatalf("verdict %+v", v)
	}
	tampered := []string{lines[0], `{"kind":"new_ap","n":3,"chain":"43bdfb22"}`}
	if v := verifyChainLines(tampered); v.Bad != 1 {
		t.Fatalf("tamper verdict %+v", v)
	}
	mixed := []string{lines[0], `{"kind":"note"}`, lines[1]}
	v = verifyChainLines(mixed)
	if v.Chained != 2 || v.Unchained != 1 || v.Bad != 1 { // unchained resets the head
		t.Fatalf("mixed verdict %+v", v)
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]bool{
		"event_index.jsonl":     true,
		"../evil.txt":           true, // basename-contained
		`..\..\evil.txt`:        true,
		"..":                    false,
		".":                     false,
		"":                      false,
		"a/b/c.txt":             true,
		"0..":                   false, // no ".." substring (fuzzer-found)
		"a..b":                  false,
		"trailingdot.":          false, // Windows aliasing hazard
		"has space.txt":         false,
		"semi;colon.txt":        false,
		strings.Repeat("a", 81): false,
		strings.Repeat("a", 80): true,
		"name-1.2_final.csv":    true,
	}
	for in, want := range cases {
		_, ok := sanitizeName(in)
		if ok != want {
			t.Errorf("sanitizeName(%q) ok=%v want %v", in, ok, want)
		}
	}
	if got, _ := sanitizeName("../evil.txt"); got != "evil.txt" {
		t.Errorf("basename = %q", got)
	}
}

// ---------- protocol behaviour ----------

func TestGoodSyncLegacyFraming(t *testing.T) {
	env := startTestEnv(t)
	c := dial(t, env.addr)
	defer c.conn.Close()
	if !c.auth("SCRIBE", "0.1.3") {
		t.Fatal("auth failed")
	}
	payload := []byte("{\"kind\":\"boot\",\"n\":1,\"chain\":\"6d854838\"}\n")
	resp := c.pushFile("notes.jsonl", payload, int64(len(payload)), shaOf(payload))
	if resp["stored"] != true {
		t.Fatalf("stored resp %v", resp)
	}
	if bye := c.done(); bye["bye"] != true {
		t.Fatalf("bye %v", bye)
	}
	p := findStored(t, env, "SCRIBE", "notes.jsonl")
	data, err := os.ReadFile(p)
	if err != nil || string(data) != string(payload) {
		t.Fatalf("stored file: %v %q", err, data)
	}
}

func TestWrongTokenRejected(t *testing.T) {
	env := startTestEnv(t)
	c := dial(t, env.addr)
	defer c.conn.Close()
	hello := c.read()
	nonce, _ := hello["nonce"].(string)
	c.send(map[string]any{"auth": hmacHex("not-the-token", nonce), "prog": "PHOENIX"})
	if resp := c.read(); resp["ok"] != false {
		t.Fatalf("resp %v", resp)
	}
}

func TestReplayRejected(t *testing.T) {
	env := startTestEnv(t)
	c1 := dial(t, env.addr)
	hello := c1.read()
	nonce1, _ := hello["nonce"].(string)
	authLine := map[string]any{"auth": hmacHex(testToken, nonce1), "prog": "PHOENIX"}
	c1.send(authLine)
	if resp := c1.read(); resp["ok"] != true {
		t.Fatalf("first auth %v", resp)
	}
	c1.conn.Close()
	// Attacker replays the captured auth line on a fresh connection.
	c2 := dial(t, env.addr)
	defer c2.conn.Close()
	hello2 := c2.read()
	if hello2["nonce"] == nonce1 {
		t.Fatal("nonce reused across connections")
	}
	c2.send(authLine)
	if resp := c2.read(); resp["ok"] != false {
		t.Fatalf("replay accepted: %v", resp)
	}
}

func TestTraversalContained(t *testing.T) {
	env := startTestEnv(t)
	c := dial(t, env.addr)
	defer c.conn.Close()
	if !c.auth("HEARTH", "0.1.4") {
		t.Fatal("auth")
	}
	payload := []byte("x")
	resp := c.pushFile("../../evil.txt", payload, 1, shaOf(payload))
	if resp["stored"] != true {
		t.Fatalf("resp %v", resp)
	}
	c.done()
	if _, err := os.Stat(filepath.Join(env.dir, "evil.txt")); !os.IsNotExist(err) {
		t.Fatal("file escaped the library")
	}
	inside := findStored(t, env, "HEARTH", "evil.txt")
	// nothing anywhere above the raw dir
	_ = filepath.Walk(env.dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Name() == "evil.txt" && p != inside {
			t.Errorf("evil.txt at unexpected %s", p)
		}
		return nil
	})
}

func TestOversizeAnnounceRefused(t *testing.T) {
	env := startTestEnv(t)
	c := dial(t, env.addr)
	defer c.conn.Close()
	if !c.auth("PHOENIX", "0.9.3") {
		t.Fatal("auth")
	}
	c.send(map[string]any{"file": "big.bin", "size": maxFileBytes + 1, "sha256": shaOf(nil)})
	if resp := c.read(); resp["send"] != false {
		t.Fatalf("resp %v", resp)
	}
}

func TestOversizeStreamKilled(t *testing.T) {
	env := startTestEnv(t)
	c := dial(t, env.addr)
	defer c.conn.Close()
	if !c.auth("PHOENIX", "0.9.3") {
		t.Fatal("auth")
	}
	payload := []byte(strings.Repeat("A", 5000))
	c.send(map[string]any{"file": "f.bin", "size": 100, "sha256": shaOf(payload)}) // lies: stream exceeds size
	if resp := c.read(); resp["send"] != true {
		t.Fatalf("resp %v", resp)
	}
	c.send(map[string]any{"data": base64.StdEncoding.EncodeToString(payload)})
	// server must drop the session; next read fails or times out
	_ = c.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, err := c.r.ReadString('\n')
	if err == nil {
		t.Fatal("server kept an oversize-stream session alive")
	}
	raw := filepath.Join(env.cfg.Library, time.Now().Format("2006-01-02"), "PHOENIX", "raw")
	entries, _ := os.ReadDir(raw)
	for _, e := range entries {
		if !e.IsDir() {
			t.Fatalf("leftover file %s after aborted transfer", e.Name())
		}
	}
	// and the server still serves a good client afterwards
	c2 := dial(t, env.addr)
	defer c2.conn.Close()
	if !c2.auth("SCRIBE", "0.1.3") {
		t.Fatal("server damaged after oversize stream")
	}
}

func TestSizeLieRejected(t *testing.T) {
	env := startTestEnv(t)
	c := dial(t, env.addr)
	defer c.conn.Close()
	if !c.auth("SCRIBE", "0.1.3") {
		t.Fatal("auth")
	}
	payload := []byte("short")
	resp := c.pushFile("a.txt", payload, 9999, shaOf(payload)) // announced 9999, sent 5
	if resp["stored"] != false {
		t.Fatalf("resp %v", resp)
	}
}

func TestShaLieRejected(t *testing.T) {
	env := startTestEnv(t)
	c := dial(t, env.addr)
	defer c.conn.Close()
	if !c.auth("SCRIBE", "0.1.3") {
		t.Fatal("auth")
	}
	payload := []byte("honest bytes")
	resp := c.pushFile("a.txt", payload, int64(len(payload)), shaOf([]byte("different")))
	if resp["stored"] != false {
		t.Fatalf("resp %v", resp)
	}
	matches, _ := filepath.Glob(filepath.Join(env.cfg.Library, "*", "SCRIBE", "raw", "a.txt"))
	if len(matches) != 0 {
		t.Fatal("sha-lying file was stored")
	}
}

func TestOversizeLineSurvives(t *testing.T) {
	env := startTestEnv(t)
	c := dial(t, env.addr)
	c.read() // hello
	c.sendRaw(strings.Repeat("A", maxLineBytes+100) + "\n")
	_ = c.conn.Close()
	for i := 0; i < 5; i++ { // half a dozen garbage connections too
		g := dial(t, env.addr)
		g.sendRaw("not json at all\n")
		_ = g.conn.Close()
	}
	c2 := dial(t, env.addr)
	defer c2.conn.Close()
	if !c2.auth("PHOENIX", "0.9.3") {
		t.Fatal("server did not survive oversize-line abuse")
	}
}

func TestFuzzGarbageServerSurvives(t *testing.T) {
	env := startTestEnv(t)
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 60; i++ {
		c := dial(t, env.addr)
		buf := make([]byte, rng.Intn(2048))
		_, _ = rng.Read(buf)
		_ = c.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, _ = c.conn.Write(buf)
		_ = c.conn.Close()
	}
	c := dial(t, env.addr)
	defer c.conn.Close()
	if !c.auth("HEARTH", "0.1.4") {
		t.Fatal("server damaged by fuzz")
	}
	payload := []byte("{\"kind\":\"boot\"}\n")
	if resp := c.pushFile("events.jsonl", payload, int64(len(payload)), shaOf(payload)); resp["stored"] != true {
		t.Fatalf("post-fuzz sync failed: %v", resp)
	}
}

// ---------- live telemetry ----------

func TestLiveStreamAndWatchdog(t *testing.T) {
	env := startTestEnv(t)
	env.live.OnlineAfter = 200 * time.Millisecond
	c := dial(t, env.addr)
	defer c.conn.Close()
	if !c.auth("PHOENIX", "0.9.3") {
		t.Fatal("auth")
	}
	c.send(map[string]any{"live_start": true})
	if resp := c.read(); resp["live_ok"] != true {
		t.Fatalf("live_start resp %v", resp)
	}
	for _, aps := range []int{7, 12} {
		c.send(map[string]any{"type": "live", "state": "scanning", "detail": "Wi-Fi scan ch 6",
			"aps": aps, "batt": 66, "pct": 40, "events": 2, "last": "new_ap"})
	}
	deadline := time.Now().Add(2 * time.Second)
	var d DeviceLive
	var got bool
	for time.Now().Before(deadline) {
		d, got = env.live.Get("PHOENIX")
		if got && d.Frames == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !got || d.APs != 12 || d.Batt != 66 || d.Pct != 40 || d.State != "scanning" {
		t.Fatalf("live state %+v", d)
	}
	if !env.live.Online(d, time.Now()) {
		t.Fatal("device should be online right after frames")
	}
	time.Sleep(300 * time.Millisecond)
	if env.live.Online(d, time.Now()) {
		t.Fatal("watchdog did not mark silent device offline")
	}
	if n := env.live.OnlineCount(); n != 0 {
		t.Fatalf("online count %d", n)
	}
}

// ---------- reports ----------

func TestReportPlainEnglishAndEscaping(t *testing.T) {
	env := startTestEnv(t)
	raw := filepath.Join(env.cfg.Library, "2026-11-09", "PHOENIX", "raw")
	if err := os.MkdirAll(raw, 0o755); err != nil {
		t.Fatal(err)
	}
	line := "{\"kind\":\"deauth_flood\",\"title\":\"<script>alert(1)</script>\",\"chain\":\"00000000\"}\n"
	if err := os.WriteFile(filepath.Join(raw, "event_index.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	env.store.Rebuild()
	report, err := os.ReadFile(filepath.Join(env.cfg.Library, "2026-11-09", "PHOENIX", "report.html"))
	if err != nil {
		t.Fatal(err)
	}
	htmlStr := string(report)
	if !strings.Contains(htmlStr, "Possible Wi-Fi kick-off attack nearby") {
		t.Error("plain-English finding missing")
	}
	if strings.Contains(htmlStr, "<script>alert(1)</script>") {
		t.Error("unescaped payload in report HTML (XSS)")
	}
	if !strings.Contains(htmlStr, "&lt;script&gt;") {
		t.Error("escaped payload not present")
	}
	if !strings.Contains(htmlStr, "<details>") {
		t.Error("raw details not folded")
	}
	var dp *DayProg
	for i, s := range env.store.Summaries() {
		if s.Prog == "PHOENIX" {
			dp = &env.store.Summaries()[i]
		}
	}
	if dp == nil || dp.Bad != 1 {
		t.Fatalf("tampered chain not detected: %+v", dp)
	}
}

func TestImportFromSD(t *testing.T) {
	env := startTestEnv(t)
	sd := t.TempDir()
	files := map[string]string{
		"AEGIS/data/event_index.jsonl": "{\"kind\":\"boot\"}\n",
		"AEGIS_RT/evidence.jsonl":      "{\"kind\":\"note\"}\n",
		"AEGIS_HERALD/events.jsonl":    "{\"kind\":\"new_ap\"}\n",
		"AEGIS_HERALD/not-tracked.txt": "ignore me",
		"random/other.txt":             "ignore me too",
	}
	for rel, content := range files {
		p := filepath.Join(sd, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	n, err := env.store.ImportFrom(sd)
	if err != nil || n != 3 {
		t.Fatalf("import n=%d err=%v", n, err)
	}
	progs := map[string]bool{}
	for _, dp := range env.store.Summaries() {
		progs[dp.Prog] = true
	}
	for _, want := range []string{"PHOENIX", "TALON", "HERALD"} {
		if !progs[want] {
			t.Errorf("missing imported program %s", want)
		}
	}
}
