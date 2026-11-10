package main

import (
	"bufio"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AEGIS/1 wire protocol (newline-delimited JSON), identical to the Python
// companion so firmware already in the field (SCRIBE/HEARTH v0.1.3+) keeps
// working byte-for-byte, plus the live telemetry extension:
//
//	server -> {"hello":"AEGIS/1","nonce":"<32 hex>"}
//	device -> {"auth":"<HMAC-SHA256 hex of nonce, key=token>","prog":"PHOENIX","ver":"..."}
//	server -> {"ok":true}                     ({"ok":false,"err":"auth"} on failure)
//	device -> {"file":"name","size":N,"sha256":"<hex>"}   server -> {"send":true}
//	device -> {"data":"<base64>"} ... {"eof":true}        server -> {"stored":true}
//	device -> {"done":true}                   server -> {"bye":true,"files":[...]}
//	device -> {"live_start":true}             server -> {"live_ok":true}
//	device -> {"type":"live","state":"scanning",...}  (fire-and-forget, ~every 2 s)

const (
	maxFileBytes    = 16 * 1024 * 1024
	maxSessionBytes = 256 * 1024 * 1024
	maxLineBytes    = 256 * 1024
)

type Server struct {
	cfg   *Config
	log   *Log
	live  *LiveRegistry
	store *Store

	// IdleTimeout kills silent connections so a lost device never leaves
	// a goroutine (or a half file) behind. Live devices frame every ~2 s.
	IdleTimeout time.Duration

	mu       sync.Mutex
	listener net.Listener
	port     int
	bg       sync.WaitGroup // in-flight report rebuilds; Stop drains them

	// Live sessions, keyed by program name, so the Dashboard can
	// send a firmware offer down an already-open link (v1.8.2).
	sessMu   sync.Mutex
	sessions map[string]*liveSession
}

// liveSession is one authenticated device connection. Writes from
// outside the handler goroutine (firmware offers) take wmu so bytes
// can never interleave with the handler's own replies.
type liveSession struct {
	conn net.Conn
	wmu  sync.Mutex
}

func (s *Server) registerSession(prog string, sess *liveSession) {
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	if s.sessions == nil {
		s.sessions = map[string]*liveSession{}
	}
	s.sessions[prog] = sess
}

func (s *Server) unregisterSession(prog string, sess *liveSession) {
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	if s.sessions[prog] == sess {
		delete(s.sessions, prog)
	}
}

// SendFirmwareOffer tells a live-linked device that a verified newer
// firmware image is waiting on this PC. The device prompts its user
// (ENTER) and only then pulls the image with fw_get. Returns false
// when the device has no live session right now.
func (s *Server) SendFirmwareOffer(prog string, info fwFeedInfo) bool {
	s.sessMu.Lock()
	sess := s.sessions[prog]
	s.sessMu.Unlock()
	if sess == nil {
		return false
	}
	sess.wmu.Lock()
	defer sess.wmu.Unlock()
	err := sendJSON(sess.conn, map[string]any{
		"cmd": "fw_offer", "prog": prog, "ver": info.Version,
		"size": info.Size, "sha256": info.SHA256,
	})
	return err == nil
}

func NewServer(cfg *Config, log *Log, live *LiveRegistry, store *Store) *Server {
	return &Server{cfg: cfg, log: log, live: live, store: store, IdleTimeout: 90 * time.Second}
}

func (s *Server) Start(port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return fmt.Errorf("already listening")
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return err
	}
	s.listener = ln
	s.port = ln.Addr().(*net.TCPAddr).Port
	s.log.Infof("listening on :%d - devices on the home network can sync + stream live status", s.port)
	go s.acceptLoop(ln)
	return nil
}

func (s *Server) Stop() {
	s.mu.Lock()
	if s.listener != nil {
		_ = s.listener.Close()
		s.listener = nil
		s.log.Infof("listener stopped")
	}
	s.mu.Unlock()
	// Drain report rebuilds so nothing writes into the library after
	// shutdown (this was a real flake: a rebuild goroutine outliving
	// its session and racing whatever came next). Launches are gated
	// on listener != nil under mu, so no Add can race this Wait.
	s.bg.Wait()
}

func (s *Server) Restart(port int) error {
	s.Stop()
	return s.Start(port)
}

func (s *Server) Listening() (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return 0, false
	}
	return s.port, true
}

func (s *Server) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed
		}
		go s.handle(conn)
	}
}

// sanitizeName: basename only, conservative charset, hard length cap.
// Nothing a device sends can name a path outside its own raw folder.
func sanitizeName(name string) (string, bool) {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	// ".." anywhere is refused (not just the exact parent segment):
	// trailing dots alias unpredictably on Windows, and no honest
	// device filename needs either.
	if name == "" || name == "." || strings.Contains(name, "..") ||
		strings.HasSuffix(name, ".") || len(name) > 80 {
		return "", false
	}
	for _, c := range name {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '_' || c == '-'
		if !ok {
			return "", false
		}
	}
	return name, true
}

func hmacHex(key, msg string) string {
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

type lineReader struct {
	r    *bufio.Reader
	conn net.Conn
	idle time.Duration
}

func (lr *lineReader) readLine() (string, error) {
	var buf []byte
	for {
		_ = lr.conn.SetReadDeadline(time.Now().Add(lr.idle))
		frag, err := lr.r.ReadSlice('\n')
		buf = append(buf, frag...)
		if len(buf) > maxLineBytes {
			return "", errors.New("line too long")
		}
		switch err {
		case nil:
			return strings.TrimSpace(string(buf)), nil
		case bufio.ErrBufferFull:
			continue
		default:
			if len(buf) > 0 && err == io.EOF {
				return strings.TrimSpace(string(buf)), nil
			}
			return "", err
		}
	}
}

func sendJSON(w io.Writer, obj map[string]any) error {
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

func parseMsg(line string) (map[string]any, error) {
	dec := json.NewDecoder(strings.NewReader(line))
	dec.UseNumber()
	m := map[string]any{}
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func msgString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func msgBool(m map[string]any, key string) bool {
	v, ok := m[key].(bool)
	return ok && v
}

func msgInt(m map[string]any, key string) (int64, bool) {
	n, ok := m[key].(json.Number)
	if !ok {
		return 0, false
	}
	i, err := n.Int64()
	return i, err == nil
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	remote := conn.RemoteAddr().String()
	lr := &lineReader{r: bufio.NewReaderSize(conn, 8192), conn: conn, idle: s.IdleTimeout}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return
	}
	nonce := hex.EncodeToString(nonceBytes)
	if err := sendJSON(conn, map[string]any{"hello": "AEGIS/1", "nonce": nonce}); err != nil {
		return
	}
	line, err := lr.readLine()
	if err != nil {
		return
	}
	auth, err := parseMsg(line)
	if err != nil {
		s.log.Warnf("auth: unparseable line from %s", remote)
		return
	}
	expect := hmacHex(s.cfg.Token, nonce)
	got := msgString(auth, "auth")
	if len(got) != len(expect) || subtle.ConstantTimeCompare([]byte(got), []byte(expect)) != 1 {
		_ = sendJSON(conn, map[string]any{"ok": false, "err": "auth"})
		s.log.Warnf("auth REJECTED from %s (bad token proof)", remote)
		return
	}
	prog, ok := sanitizeName(msgString(auth, "prog"))
	if !ok || prog == "" {
		prog = "device"
	}
	ver := msgString(auth, "ver")
	if err := sendJSON(conn, map[string]any{"ok": true}); err != nil {
		return
	}
	sess := &liveSession{conn: conn}
	defer s.unregisterSession(prog, sess)
	send := func(obj map[string]any) error {
		sess.wmu.Lock()
		defer sess.wmu.Unlock()
		return sendJSON(conn, obj)
	}
	s.log.Infof("auth ok: %s %s from %s", prog, ver, remote)

	day := time.Now().Format("2006-01-02")
	rawdir := filepath.Join(s.store.Root(), day, prog, "raw")
	_ = os.MkdirAll(rawdir, 0o755)

	var sessionBytes int64
	var stored []string
	liveMode := false
	liveFrames := 0
	lastState := ""

	for {
		line, err := lr.readLine()
		if err != nil {
			if liveMode {
				s.log.Infof("live link closed: %s (%d frames)", prog, liveFrames)
			}
			return
		}
		msg, err := parseMsg(line)
		if err != nil {
			s.log.Warnf("session error (%s): bad JSON line", prog)
			return
		}
		if msgBool(msg, "done") {
			_ = sendJSON(conn, map[string]any{"bye": true, "files": stored})
			break
		}
		if msgBool(msg, "fw_get") {
			// Device accepted a firmware offer and is pulling the
			// image over this LAN connection (v1.8.2): one JSON
			// header line, then the raw verified bytes.
			info, binPath, ok := stagedFw(s.cfg)
			if !ok {
				_ = send(map[string]any{"fw_begin": false, "err": "no firmware staged on this PC"})
				continue
			}
			if err := send(map[string]any{"fw_begin": true, "ver": info.Version, "size": info.Size, "sha256": info.SHA256}); err != nil {
				return
			}
			f, err := os.Open(binPath)
			if err != nil {
				return
			}
			n, copyErr := io.Copy(conn, f)
			f.Close()
			s.log.Infof("firmware: sent PHOENIX %s (%d bytes) to %s over the LAN (err=%v)", info.Version, n, prog, copyErr)
			return
		}
		if msgBool(msg, "live_start") {
			liveMode = true
			s.registerSession(prog, sess)
			_ = send(map[string]any{"live_ok": true})
			s.log.Infof("live link open: %s %s - dashboard is watching", prog, ver)
			continue
		}
		if msgString(msg, "type") == "live" {
			if !liveMode {
				liveMode = true
				s.registerSession(prog, sess)
				s.log.Infof("live link open: %s %s - dashboard is watching", prog, ver)
			}
			f := frameFromMsg(msg, prog, ver)
			s.live.Update(f)
			liveFrames++
			if f.State != lastState {
				s.log.Infof("live: %s -> %s %s", prog, f.State, f.Detail)
				lastState = f.State
			}
			continue
		}
		// ---- file announcement (legacy flow, unchanged) ----
		name, nameOK := sanitizeName(msgString(msg, "file"))
		size, sizeOK := msgInt(msg, "size")
		sha := msgString(msg, "sha256")
		if !nameOK || !sizeOK || size < 0 || size > maxFileBytes {
			_ = sendJSON(conn, map[string]any{"send": false})
			continue
		}
		if err := sendJSON(conn, map[string]any{"send": true}); err != nil {
			return
		}
		tmp, err := os.CreateTemp(rawdir, ".tmp-*")
		if err != nil {
			s.log.Errorf("cannot create temp file in %s: %v", rawdir, err)
			return
		}
		tmpName := tmp.Name()
		h := sha256.New()
		var got int64
		aborted := false
		for {
			cl, err := lr.readLine()
			if err != nil {
				aborted = true
				break
			}
			d, err := parseMsg(cl)
			if err != nil {
				aborted = true
				break
			}
			if msgBool(d, "eof") {
				break
			}
			chunk, err := base64.StdEncoding.DecodeString(msgString(d, "data"))
			if err != nil {
				aborted = true
				break
			}
			got += int64(len(chunk))
			sessionBytes += int64(len(chunk))
			if got > size || got > maxFileBytes || sessionBytes > maxSessionBytes {
				aborted = true
				break
			}
			h.Write(chunk)
			if _, err := tmp.Write(chunk); err != nil {
				aborted = true
				break
			}
		}
		_ = tmp.Close()
		if aborted {
			_ = os.Remove(tmpName)
			s.log.Warnf("session error (%s): transfer of %s aborted (cap or framing)", prog, name)
			return
		}
		if got != size || hex.EncodeToString(h.Sum(nil)) != sha {
			_ = os.Remove(tmpName)
			_ = sendJSON(conn, map[string]any{"stored": false, "err": "size/sha mismatch"})
			s.log.Warnf("rejected %s from %s: size/SHA-256 mismatch - nothing stored", name, prog)
			continue
		}
		if err := os.Rename(tmpName, filepath.Join(rawdir, name)); err != nil {
			_ = os.Remove(tmpName)
			_ = sendJSON(conn, map[string]any{"stored": false, "err": "io"})
			continue
		}
		stored = append(stored, name)
		_ = sendJSON(conn, map[string]any{"stored": true})
		s.log.Infof("received %s from %s (%d bytes, SHA-256 verified)", name, prog, size)
	}
	if len(stored) > 0 {
		s.log.Infof("%s: sync complete, %d file(s) stored - rebuilding reports", prog, len(stored))
		// Gate the launch on "still running" under mu: Stop nils the
		// listener before it drains bg, so an Add can never race Wait.
		// A rebuild skipped during shutdown is harmless (the next
		// startup rebuilds), a leaked one is a file-writing race.
		s.mu.Lock()
		if s.listener != nil {
			s.bg.Add(1)
			go func() {
				defer s.bg.Done()
				s.store.Rebuild()
			}()
		}
		s.mu.Unlock()
	}
}

func frameFromMsg(msg map[string]any, prog, ver string) Frame {
	f := Frame{Prog: prog, Ver: ver, Pct: -1}
	f.State = msgString(msg, "state")
	f.Detail = msgString(msg, "detail")
	f.Last = msgString(msg, "last")
	// Device-supplied numbers are clamped here, at the one choke point:
	// a buggy or hostile device must not push pct=99999 or batt=-4 into
	// the dashboard's bars and gauges (fuzzer-found).
	clampPct := func(v int64) int {
		if v < 0 {
			return 0
		}
		if v > 100 {
			return 100
		}
		return int(v)
	}
	clampCount := func(v int64) int {
		if v < 0 {
			return 0
		}
		if v > 1_000_000 {
			return 1_000_000
		}
		return int(v)
	}
	if v, ok := msgInt(msg, "batt"); ok {
		f.Batt, f.HasBatt = clampPct(v), true
	}
	if v, ok := msgInt(msg, "aps"); ok {
		f.APs, f.HasAPs = clampCount(v), true
	}
	if v, ok := msgInt(msg, "pct"); ok {
		f.Pct, f.HasPct = clampPct(v), true
	}
	if v, ok := msgInt(msg, "events"); ok {
		f.Events, f.HasEvts = clampCount(v), true
	}
	if v, ok := msgInt(msg, "gps_acc"); ok && v > 0 {
		if v > 1000 {
			v = 1000
		}
		f.GPSAcc, f.HasGPS = int(v), true
	}
	if v, ok := msgInt(msg, "gps_rej"); ok {
		f.GPSRej, f.HasGPSR = clampCount(v), true
	}
	f.Charging = msgBool(msg, "chg")
	return f
}
