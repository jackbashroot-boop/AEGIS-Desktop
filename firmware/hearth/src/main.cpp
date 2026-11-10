// =====================================================================
// AEGIS HEARTH v0.1.0 - docked home Sentinel for M5Stack Cardputer
// ADV. The plugged-in family member: periodic Wi-Fi scans against a
// learned baseline, new-AP alerts, hash-chained event log
// (/AEGIS_HEARTH/events.jsonl), big at-a-glance status screen.
//
// Scope (honest v0.1): Wi-Fi only (no BLE in this build), receive-
// only scanning, no intruding/connecting to anything. Input = family
// standard: one grouped menu, arrows + Enter, Esc. No keybinds.
// =====================================================================
#include <M5Cardputer.h>
#include <SD.h>
#include <SPI.h>
#include <WiFi.h>
#include <WiFiUdp.h>
#include <Preferences.h>
#include <vector>

static const char* kVer = "0.1.6";
static const char* kFw = "AEGIS HEARTH 0.1.5";
static M5Canvas cv(&M5Cardputer.Display);
static const uint16_t C_BG = 0x0000, C_BAR = 0x3000, C_ACC = 0xFA20,
                      C_DIM = 0x8410, C_TXT = 0xFFFF, C_SEL = 0x2008,
                      C_OK = 0x07E0, C_WARN = 0xFFE0, C_BAD = 0xF800, C_CYAN = 0x07FF;

struct BatteryGauge {
  int pct = -1; bool charging = false; int mv = 0;
  void update() {
    const uint32_t now = millis();
    if (pct >= 0 && now - lastMs_ < 1000) return;
    const int v = M5Cardputer.Power.getBatteryVoltage();
    if (v <= 0) { if (pct < 0) pct = M5Cardputer.Power.getBatteryLevel(); return; }
    const bool pm = M5Cardputer.Power.isCharging();
    if (!charging && (pm || (lastMv_ > 0 && v - lastMv_ >= 50))) { charging = true; csPct_ = pct; csMs_ = now; }
    else if (charging && !pm && lastMv_ > 0 && lastMv_ - v >= 60) { charging = false; csPct_ = -1; }
    lastMv_ = v; mv = v; lastMs_ = now;
    int target = pctFromMv(v);
    if (charging) {
      if (csPct_ < 0) { csPct_ = pct < 0 ? target : pct; csMs_ = now; }
      const int allowed = csPct_ + (int)((now - csMs_) / 120000);
      if (target > allowed) target = allowed;
      if (pct >= 0 && target < pct) target = pct;
    }
    if (pct < 0) pct = target;
    else if (target > pct) pct = min(target, pct + 1);
    else if (target < pct) pct = max(target, pct - 1);
  }
 private:
  uint32_t lastMs_ = 0, csMs_ = 0; int lastMv_ = 0, csPct_ = -1;
  static int pctFromMv(int v) {
    static const int pts[][2] = {{3270,0},{3610,10},{3690,20},{3730,30},{3780,40},
                                 {3830,50},{3880,60},{3940,70},{4000,80},{4070,90},{4200,100}};
    if (v <= pts[0][0]) return 0;
    for (int i = 1; i < 11; i++)
      if (v <= pts[i][0]) {
        const int lo = pts[i-1][0], hi = pts[i][0];
        return pts[i-1][1] + (v - lo) * (pts[i][1] - pts[i-1][1]) / (hi - lo);
      }
    return 100;
  }
};
static BatteryGauge g_batt;
template <typename T>
static void drawBatteryIcon(T& d, int x, int y, uint16_t bg) {
  g_batt.update();
  const String pct = String(g_batt.pct) + "%";
  d.setTextSize(1);
  d.setTextColor(g_batt.pct <= 15 ? C_BAD : (g_batt.pct <= 35 ? C_WARN : C_OK), bg);
  d.setCursor(x - (int)pct.length() * 6 - 3, y + 2); d.print(pct);
  d.drawRect(x, y, 20, 10, 0xC618); d.fillRect(x + 20, y + 3, 2, 4, 0xC618);
  const int fill = (g_batt.pct * 16) / 100;
  if (fill > 0) d.fillRect(x + 2, y + 2, fill, 6, g_batt.charging ? C_CYAN : C_OK);
  if (g_batt.charging) { d.fillRect(x + 9, y + 2, 3, 4, 0x0000); d.fillRect(x + 8, y + 5, 3, 3, 0x0000); }
}

// Family pixel mark (16x16, full-bleed glyph - PHOENIX/TALON style).
static const char* kMark[16] = {
  ".......AA.......",
  "......AAAA......",
  ".....AAAAAA.....",
  "....AAAAAAAA....",
  "...AAAAAAAAAA...",
  "....W......W....",
  "....W......W....",
  "....W..FF..W....",
  "....W.FFFF.W....",
  "....W.FFFF.W....",
  "....W..FF..W....",
  "....WWWWWWWW....",
  "................",
  "................",
  "................",
  "................"
};
static void drawMark(int cx, int cy, int sc) {
  for (int y = 0; y < 16; y++) for (int x = 0; x < 16; x++) {
    const char c = kMark[y][x];
    if (c == '.') continue;
    uint16_t col = C_ACC;
    if (c=='A') col=C_ACC; else if (c=='W') col=C_TXT; else if (c=='F') col=C_WARN;
    cv.fillRect(cx - 8 * sc + x * sc, cy - 8 * sc + y * sc, sc, sc, col);
  }
}


// Shared home profile (family "aegisnet" NVS - set once in PHOENIX).
static String g_homeSsid, g_homePsk;
static void loadHomeProfile() {
  Preferences np;
  if (np.begin("aegisnet", true)) { g_homeSsid = np.getString("ssid", ""); g_homePsk = np.getString("psk", ""); np.end(); }
}

static uint32_t g_netNext = 0, g_netBackoff = 5000;
static void homeJoinTick(bool busy) {
  if (!g_homeSsid.length()) return;
  if (WiFi.status() == WL_CONNECTED) { g_netBackoff = 5000; return; }
  if (busy || (int32_t)(millis() - g_netNext) < 0) return;
  WiFi.mode(WIFI_STA); WiFi.begin(g_homeSsid.c_str(), g_homePsk.c_str());
  g_netNext = millis() + g_netBackoff;
  g_netBackoff = min<uint32_t>(g_netBackoff * 2 + 5000, 60000);
}
static bool g_sdOk = false;
static String fit(const String& s, int n) { return s.length() > (size_t)n ? s.substring(0, n) : s; }
static void line(int y, const String& t, uint16_t col = C_TXT) {
  cv.setTextSize(1); cv.setTextColor(col, C_BG); cv.setCursor(4, y); cv.print(fit(t, 39));
}
static void hdr(const char* title) {
  cv.fillScreen(C_BG);
  cv.fillRect(0, 0, 240, 16, C_BAR); cv.fillRect(0, 16, 240, 2, C_ACC);
  cv.setTextSize(1); cv.setTextColor(C_TXT, C_BAR); cv.setCursor(4, 4); cv.print(fit(String(title), 26));
  drawBatteryIcon(cv, 218, 3, C_BAR);
}
static void foot(const String& t) { line(125, t, C_DIM); }
static void progressBar(int x, int y, int w, int pct, uint16_t col) {
  if (pct < 0) pct = 0; if (pct > 100) pct = 100;
  cv.drawRect(x, y, w, 7, C_DIM);
  if (pct > 0) cv.fillRect(x + 1, y + 1, (w - 2) * pct / 100, 5, col);
  cv.setTextSize(1); cv.setTextColor(C_TXT, C_BG);
  cv.setCursor(x + w + 4, y); cv.print(String(pct) + "%");
}

// ------------------------------------------------------------- state
static std::vector<String> g_baseline;   // BSSIDs
static std::vector<String> g_alerts;     // "NEW ssid bssid rssi"
static bool g_hasBaseline = false;
static bool g_scanning = false;
static uint32_t g_scanStart = 0, g_lastScanMs = 0, g_nextScanMs = 15000;
static int g_lastCount = -1, g_scanCount = 0, g_newThisScan = 0;
static String g_status = "boot";
static uint32_t g_chain = 0x811c9dc5u;
static uint32_t g_eventCount = 0;
static char g_lastKind[20] = "";

static String fnvHex(uint32_t h) { char b[9]; snprintf(b, sizeof(b), "%08x", h); return String(b); }
static void logEvent(const String& kind, const String& fieldsJson) {
  g_eventCount++;
  strlcpy(g_lastKind, kind.c_str(), sizeof(g_lastKind));
  if (!g_sdOk) return;
  if (!SD.exists("/AEGIS_HEARTH")) SD.mkdir("/AEGIS_HEARTH");
  String body = "{\"kind\":\"" + kind + "\",\"uptime_ms\":" + String(millis()) + ",\"fields\":" + fieldsJson + "}";
  uint32_t h = g_chain;
  for (size_t i = 0; i < body.length(); i++) { h ^= (uint8_t)body[i]; h *= 0x01000193u; }
  g_chain = h;
  File f = SD.open("/AEGIS_HEARTH/events.jsonl", FILE_APPEND);
  if (f) { f.print(body.substring(0, body.length() - 1) + ",\"chain\":\"" + fnvHex(h) + "\"}\n"); f.close(); }
  SD.remove("/AEGIS_HEARTH/chain.txt");
  File c = SD.open("/AEGIS_HEARTH/chain.txt", FILE_WRITE);
  if (c) { c.print(fnvHex(h)); c.close(); }
}
static void saveBaseline() {
  if (!g_sdOk) return;
  if (!SD.exists("/AEGIS_HEARTH")) SD.mkdir("/AEGIS_HEARTH");
  File f = SD.open("/AEGIS_HEARTH/baseline.txt", FILE_WRITE);
  if (!f) return;
  for (auto& b : g_baseline) f.println(b);
  f.close();
  g_hasBaseline = true;
}
static void loadBaseline() {
  File f = SD.open("/AEGIS_HEARTH/baseline.txt", FILE_READ);
  if (!f) return;
  g_baseline.clear();
  while (f.available()) { String s = f.readStringUntil('\n'); s.trim(); if (s.length()) g_baseline.push_back(s); }
  f.close();
  g_hasBaseline = !g_baseline.empty();
}
static bool inBaseline(const String& bssid) {
  for (auto& b : g_baseline) if (b == bssid) return true;
  return false;
}
static void finishScan(int n) {
  g_lastCount = n; g_scanCount++; g_newThisScan = 0;
  std::vector<String> seen;
  for (int i = 0; i < n; i++) {
    const String b = WiFi.BSSIDstr(i);
    seen.push_back(b);
    if (g_hasBaseline && !inBaseline(b)) {
      g_newThisScan++;
      const String a = "NEW " + fit(WiFi.SSID(i), 14) + " " + b + " " + WiFi.RSSI(i);
      g_alerts.push_back(a);
      if (g_alerts.size() > 40) g_alerts.erase(g_alerts.begin());
      logEvent("new_ap", "{\"ssid\":\"" + WiFi.SSID(i) + "\",\"bssid\":\"" + b + "\"}");
    }
  }
  if (!g_hasBaseline) { g_baseline = seen; saveBaseline(); g_status = "baseline learned"; logEvent("baseline", "{\"aps\":" + String(n) + "}"); }
  logEvent("scan", "{\"aps\":" + String(n) + ",\"new\":" + String(g_newThisScan) + "}");
  g_lastScanMs = millis();
  g_nextScanMs = millis() + 60000;
  g_netNext = millis() + 3000;   // rejoin home shortly after the scan
}

enum Screen { SCR_SPLASH, SCR_MENU, SCR_LIVE, SCR_ALERTS, SCR_BASE, SCR_ABOUT, SCR_SYNC, SCR_LIVETGL, SCR_ASYNCTGL };
static Screen g_screen = SCR_SPLASH;
static uint32_t g_bootMs = 0;
struct MenuItem { const char* group; const char* label; Screen target; };
static const MenuItem kMenu[] = {
  { "WATCH",  "Sentinel live",   SCR_LIVE },
  { "WATCH",  "Alerts (new APs)",SCR_ALERTS },
  { "WATCH",  "Baseline",        SCR_BASE },
  { "WATCH",  "Sync to desktop", SCR_SYNC },
  { "SYSTEM", "Desktop live link", SCR_LIVETGL },
  { "SYSTEM", "Desktop auto-sync", SCR_ASYNCTGL },
  { "SYSTEM", "About HEARTH",    SCR_ABOUT },
};
static const int kMenuCount = sizeof(kMenu) / sizeof(kMenu[0]);
static int g_menuSel = 0, g_alertSel = 0;

static String uptimeStr() {
  const uint32_t s = millis() / 1000;
  char b[16]; snprintf(b, sizeof(b), "%02lu:%02lu:%02lu", s / 3600, (s / 60) % 60, s % 60);
  return String(b);
}


// ---------------------------------------------------------- desktop sync
// Push family logs to AEGIS COMPANION on the home LAN. Config:
// /AEGIS_LINK/desktop.txt  (host=, port=, token=). Auth is HMAC-
// SHA256 challenge-response; the token never crosses the wire.
#include <mbedtls/md.h>
#include <mbedtls/sha256.h>
#include <mbedtls/base64.h>
static String g_deskHost, g_deskToken;
static int g_deskPort = 8791;
static int g_syncPct = -1;
static void loadDesktopCfg() {
  File f = SD.open("/AEGIS_LINK/desktop.txt", FILE_READ);
  if (!f) return;
  while (f.available()) {
    String ln = f.readStringUntil('\n'); ln.trim();
    if (ln.startsWith("host=")) g_deskHost = ln.substring(5);
    else if (ln.startsWith("port=")) g_deskPort = ln.substring(5).toInt();
    else if (ln.startsWith("token=")) g_deskToken = ln.substring(6);
  }
  f.close();
}
static String hmacHex(const String& key, const String& msg) {
  const mbedtls_md_info_t* info = mbedtls_md_info_from_type(MBEDTLS_MD_SHA256);
  mbedtls_md_context_t ctx; mbedtls_md_init(&ctx);
  if (mbedtls_md_setup(&ctx, info, 1) != 0) { mbedtls_md_free(&ctx); return ""; }
  mbedtls_md_hmac_starts(&ctx, (const uint8_t*)key.c_str(), key.length());
  mbedtls_md_hmac_update(&ctx, (const uint8_t*)msg.c_str(), msg.length());
  uint8_t out[32]; mbedtls_md_hmac_finish(&ctx, out); mbedtls_md_free(&ctx);
  char hex[65]; for (int i = 0; i < 32; i++) snprintf(hex + i * 2, 3, "%02x", out[i]);
  return String(hex);
}
static String fileSha256(const char* path, uint32_t& sizeOut) {
  File f = SD.open(path, FILE_READ);
  if (!f) { sizeOut = 0; return ""; }
  sizeOut = f.size();
  mbedtls_sha256_context ctx; mbedtls_sha256_init(&ctx); mbedtls_sha256_starts(&ctx, 0);
  uint8_t buf[1024]; size_t n;
  while ((n = f.read(buf, sizeof(buf))) > 0) mbedtls_sha256_update(&ctx, buf, n);
  uint8_t out[32]; mbedtls_sha256_finish(&ctx, out); mbedtls_sha256_free(&ctx);
  f.close();
  char hex[65]; for (int i = 0; i < 32; i++) snprintf(hex + i * 2, 3, "%02x", out[i]);
  return String(hex);
}
static bool syncAbortWanted() {
  M5Cardputer.update();
  if (M5Cardputer.Keyboard.isChange() && M5Cardputer.Keyboard.isPressed()) {
    auto ks = M5Cardputer.Keyboard.keysState();
    for (char ch : ks.word) if (ch == '`') return true;
  }
  return false;
}
static void drawSyncProgress(const String& what) {
  hdr("SYNC TO DESKTOP");
  line(34, what, C_ACC);
  if (g_syncPct >= 0) progressBar(4, 50, 170, g_syncPct, C_OK);
  line(70, "ESC aborts - files are SHA-verified", C_DIM);
  cv.pushSprite(0, 0);
}
static bool syncReadLine(WiFiClient& c, String& out) {
  const uint32_t t0 = millis();
  out = "";
  while (millis() - t0 < 9000) {
    while (c.available()) {
      char ch = (char)c.read();
      if (ch == '\n') { out.trim(); return out.length() > 0; }
      if (out.length() < 4096) out += ch;
    }
    if (!c.connected() && !c.available()) break;
    delay(5);
  }
  out.trim();
  return out.length() > 0;
}
struct SyncFile { const char* path; const char* name; };
static int runSync(const SyncFile* files, int nFiles, const char* prog);
static void draw();

// ------------------------------------------------- discovery + auto-sync
// Finds AEGIS COMPANION on the LAN when the address in desktop.txt
// stops working (DHCP moves happen): broadcast a nonce, require an
// HMAC proof keyed by the shared token, adopt the answering address
// and write it back to the card so sync uses it too.
static bool g_liveLinked = false;
static bool g_autoSync = true;
static uint32_t g_nextAutoSync = 0, g_lastDiscoverMs = 0;
static void saveDesktopCfg() {
  if (!g_sdOk) return;
  if (!SD.exists("/AEGIS_LINK")) SD.mkdir("/AEGIS_LINK");
  File f = SD.open("/AEGIS_LINK/desktop.txt", FILE_WRITE);
  if (!f) return;
  f.print("host=" + g_deskHost + "\nport=" + String(g_deskPort) + "\ntoken=" + g_deskToken + "\n");
  f.close();
}
static bool discoverDesktop() {
  if (!g_deskToken.length() || WiFi.status() != WL_CONNECTED) return false;
  char nonce[33];
  for (int i = 0; i < 4; i++) snprintf(nonce + i * 8, 9, "%08lx", (unsigned long)esp_random());
  WiFiUDP udp;
  if (!udp.begin(0)) return false;
  const IPAddress bcast(255, 255, 255, 255);
  udp.beginPacket(bcast, g_deskPort); udp.print("AEGIS_DISCOVER 1 " + String(nonce) + "\n"); udp.endPacket();
  if (g_deskPort != 8791) { udp.beginPacket(bcast, 8791); udp.print("AEGIS_DISCOVER 1 " + String(nonce) + "\n"); udp.endPacket(); }
  const uint32_t t0 = millis();
  bool found = false;
  while (millis() - t0 < 1500 && !found) {
    const int sz = udp.parsePacket();
    if (sz > 0) {
      char buf[160]; const int n = udp.read(buf, sizeof(buf) - 1); buf[max(0, n)] = 0;
      String r(buf);
      if (r.startsWith("AEGIS_HERE 1 ")) {
        const int sp = r.lastIndexOf(' ');
        const String mac = r.substring(sp + 1); String macT = mac; macT.trim();
        const int port = r.substring(11, sp).toInt();
        if (port > 0 && macT == hmacHex(g_deskToken, "AEGIS_DISCOVER:" + String(nonce))) {
          g_deskHost = udp.remoteIP().toString();
          g_deskPort = port;
          saveDesktopCfg();
          found = true;
        }
      }
    }
    delay(10);
  }
  udp.stop();
  return found;
}
static const SyncFile kSyncFiles[] = {
  {"/AEGIS_HEARTH/events.jsonl", "events.jsonl"},
  {"/AEGIS_HEARTH/baseline.txt", "baseline.txt"},
};
static bool syncFilesDirty() {
  // watermark: name=size lines in /AEGIS_HEARTH/.autosync
  String wm;
  File f = SD.open("/AEGIS_HEARTH/.autosync", FILE_READ);
  if (f) { wm = f.readString(); f.close(); }
  for (auto& sf : kSyncFiles) {
    File x = SD.open(sf.path, FILE_READ);
    const long sz = x ? (long)x.size() : -1;
    if (x) x.close();
    const String key = String(sf.name) + "=" + String(sz) + "\n";
    if (sz > 0 && wm.indexOf(key) < 0) return true;
  }
  return false;
}
static void writeSyncWatermark() {
  File f = SD.open("/AEGIS_HEARTH/.autosync", FILE_WRITE);
  if (!f) return;
  for (auto& sf : kSyncFiles) {
    File x = SD.open(sf.path, FILE_READ);
    const long sz = x ? (long)x.size() : -1;
    if (x) x.close();
    if (sz > 0) f.print(String(sf.name) + "=" + String(sz) + "\n");
  }
  f.close();
}
static void autoSyncTick() {
  if (!g_autoSync || g_syncPct >= 0 || !g_sdOk || WiFi.status() != WL_CONNECTED) return;
  if ((int32_t)(millis() - g_nextAutoSync) < 0) return;
  if (!syncFilesDirty()) { g_nextAutoSync = millis() + 60000; return; }
  if (!g_liveLinked) {
    // prove the desktop is there before taking over the screen
    WiFiClient probe;
    if (!g_deskHost.length() || !probe.connect(g_deskHost.c_str(), g_deskPort, 1200)) {
      g_nextAutoSync = millis() + 180000;
      return;
    }
    probe.stop();
  }
  const Screen prev = g_screen;
  const int stored = runSync(kSyncFiles, 2, "HEARTH");
  if (stored >= 0) { writeSyncWatermark(); g_nextAutoSync = millis() + 900000; }
  else g_nextAutoSync = millis() + 180000;
  g_screen = prev;
  draw();
}

static int runSync(const SyncFile* files, int nFiles, const char* prog) {
  if (!g_sdOk) { g_status = "sync: no SD"; return -1; }
  loadDesktopCfg();
  if (!g_deskHost.length() || !g_deskToken.length()) { g_status = "sync: no /AEGIS_LINK/desktop.txt"; return -1; }
  if (WiFi.status() != WL_CONNECTED) { g_status = "sync: join home WiFi first"; return -1; }
  WiFiClient c;
  g_syncPct = 0; drawSyncProgress("connecting...");
  if (!c.connect(g_deskHost.c_str(), g_deskPort, 5000)) { g_status = "sync: desktop unreachable"; g_syncPct = -1; return -1; }
  String ln;
  if (!syncReadLine(c, ln) || ln.indexOf("AEGIS/1") < 0) { g_status = "sync: bad hello"; c.stop(); g_syncPct = -1; return -1; }
  int np = ln.indexOf("\"nonce\":\"");
  String nonce = np >= 0 ? ln.substring(np + 9, ln.indexOf("\"", np + 9)) : "";
  c.print("{\"auth\":\"" + hmacHex(g_deskToken, nonce) + "\",\"prog\":\"" + prog + "\",\"ver\":\"" + kVer + "\"}\n");
  if (!syncReadLine(c, ln) || ln.indexOf("\"ok\": true") < 0 && ln.indexOf("\"ok\":true") < 0) {
    g_status = "sync: auth rejected"; c.stop(); g_syncPct = -1; return -1;
  }
  uint32_t totalBytes = 0, sentBytes = 0;
  for (int i = 0; i < nFiles; i++) { uint32_t sz; if (SD.exists(files[i].path)) { fileSha256(files[i].path, sz); totalBytes += sz; } }
  int stored = 0;
  for (int i = 0; i < nFiles; i++) {
    if (!SD.exists(files[i].path)) continue;
    if (syncAbortWanted()) { g_status = "sync aborted"; c.stop(); g_syncPct = -1; return -1; }
    uint32_t sz; const String sha = fileSha256(files[i].path, sz);
    c.print("{\"file\":\"" + String(files[i].name) + "\",\"size\":" + String(sz) + ",\"sha256\":\"" + sha + "\"}\n");
    if (!syncReadLine(c, ln) || ln.indexOf("\"send\": true") < 0 && ln.indexOf("\"send\":true") < 0) continue;
    File f = SD.open(files[i].path, FILE_READ);
    uint8_t buf[3072]; size_t n;
    bool aborted = false;
    while ((n = f.read(buf, sizeof(buf))) > 0) {
      uint8_t b64[4200]; size_t olen = 0;
      mbedtls_base64_encode(b64, sizeof(b64), &olen, buf, n);
      b64[olen] = 0;
      c.print("{\"data\":\"" + String((const char*)b64) + "\"}\n");
      sentBytes += n;
      g_syncPct = totalBytes ? (int)(sentBytes * 100 / totalBytes) : 100;
      drawSyncProgress(String("sending ") + files[i].name);
      if (syncAbortWanted()) { aborted = true; break; }
    }
    f.close();
    if (aborted) { g_status = "sync aborted"; c.stop(); g_syncPct = -1; return -1; }
    c.print("{\"eof\":true}\n");
    if (syncReadLine(c, ln) && ln.indexOf("\"stored\": true") >= 0 || ln.indexOf("\"stored\":true") >= 0) stored++;
  }
  c.print("{\"done\":true}\n");
  syncReadLine(c, ln);
  c.stop();
  g_syncPct = -1;
  g_status = "synced " + String(stored) + " file(s) to desktop";
  return stored;
}

// ------------------------------------------------------- desktop live link
// Streams live status to AEGIS COMPANION while on home WiFi, so the
// desktop shows this Sentinel on its dashboard. Same /AEGIS_LINK/
// desktop.txt + HMAC auth as sync; frames are one-way, ~2 s cadence.
// Runs on its own low-priority task - never blocks scanning.
static bool g_liveEnabled = true;
struct LiveSnap { char state[16]; char detail[80]; char last[20]; int aps; int batt; int pct; uint32_t events; bool chg; };
static LiveSnap g_liveSnap = {"watching", "starting", "", 0, -1, -1, 0, false};
static String jsonEsc(const String& s) {
  String o; o.reserve(s.length() + 4);
  for (size_t i = 0; i < s.length(); i++) { const char c = s[i]; if (c == '"' || c == '\\') o += '\\'; if (c >= 32) o += c; }
  return o;
}
static void liveUpdate() {
  g_batt.update();
  LiveSnap s;
  if (g_scanning) {
    strlcpy(s.state, "scanning", sizeof(s.state));
    strlcpy(s.detail, "Wi-Fi scan in progress", sizeof(s.detail));
    s.pct = min(99, (int)((millis() - g_scanStart) * 100 / 5000));
  } else if (!g_alerts.empty()) {
    strlcpy(s.state, "alert", sizeof(s.state));
    snprintf(s.detail, sizeof(s.detail), "%u new-AP alert(s) - see Alerts", (unsigned)g_alerts.size());
    s.pct = -1;
  } else {
    strlcpy(s.state, "watching", sizeof(s.state));
    snprintf(s.detail, sizeof(s.detail), "baseline watch - %d known APs", g_hasBaseline ? (int)g_baseline.size() : 0);
    s.pct = -1;
  }
  strlcpy(s.last, g_lastKind, sizeof(s.last));
  s.aps = g_lastCount >= 0 ? g_lastCount : (int)g_baseline.size();
  s.batt = g_batt.pct; s.chg = g_batt.charging;
  s.events = g_eventCount;
  g_liveSnap = s;
}
static void liveTaskFn(void*) {
  static char frame[360];
  for (;;) {
    if (!g_liveEnabled || !g_sdOk || WiFi.status() != WL_CONNECTED || g_syncPct >= 0) { delay(2000); continue; }
    if (!g_deskHost.length()) { loadDesktopCfg(); delay(3000); continue; }
    WiFiClient c;
    if (!c.connect(g_deskHost.c_str(), g_deskPort, 4000)) {
      if (millis() - g_lastDiscoverMs > 30000) {
        g_lastDiscoverMs = millis();
        if (discoverDesktop()) { delay(500); continue; }
      }
      delay(5000); continue;
    }
    String ln;
    bool ok = syncReadLine(c, ln) && ln.indexOf("AEGIS/1") >= 0;
    String nonce;
    if (ok) { const int np = ln.indexOf("\"nonce\":\""); nonce = np >= 0 ? ln.substring(np + 9, ln.indexOf("\"", np + 9)) : ""; }
    if (ok) {
      c.print("{\"auth\":\"" + hmacHex(g_deskToken, nonce) + "\",\"prog\":\"HEARTH\",\"ver\":\"" + kVer + "\"}\n");
      ok = syncReadLine(c, ln) && (ln.indexOf("\"ok\": true") >= 0 || ln.indexOf("\"ok\":true") >= 0);
    }
    if (ok) {
      c.print("{\"live_start\":true}\n");
      ok = syncReadLine(c, ln) && ln.indexOf("live_ok") >= 0;
    }
    g_liveLinked = ok;
    while (ok && c.connected() && g_liveEnabled && WiFi.status() == WL_CONNECTED && g_syncPct < 0) {
      const LiveSnap s = g_liveSnap;
      int n = snprintf(frame, sizeof(frame),
        "{\"type\":\"live\",\"state\":\"%s\",\"detail\":\"%s\",\"aps\":%d,\"batt\":%d,\"chg\":%s,\"events\":%lu,\"last\":\"%s\"",
        s.state, jsonEsc(String(s.detail)).c_str(), s.aps, s.batt, s.chg ? "true" : "false",
        (unsigned long)s.events, s.last);
      if (s.pct >= 0 && n > 0 && n < (int)sizeof(frame) - 24) n += snprintf(frame + n, sizeof(frame) - n, ",\"pct\":%d", s.pct);
      if (n > 0 && n < (int)sizeof(frame) - 2) { frame[n++] = '}'; frame[n++] = '\n'; frame[n] = 0; }
      if (c.print(frame) == 0) break;
      delay(2000);
    }
    g_liveLinked = false;
    c.stop();
    delay(3000);
  }
}
static void liveBegin() { xTaskCreatePinnedToCore(liveTaskFn, "aegislive", 6144, nullptr, 1, nullptr, 0); }

static void draw() {
  switch (g_screen) {
    case SCR_SPLASH: {
      cv.fillScreen(C_BG);
      drawMark(120, 36, 4);
      cv.setTextSize(2); cv.setTextColor(C_TXT, C_BG); cv.setCursor(52, 80); cv.print("AEGIS");
      cv.setTextColor(C_ACC, C_BG); cv.setCursor(124, 80); cv.print("HEARTH");
      line(100, "docked home Sentinel", C_DIM);
      line(114, "baseline watch - new AP alerts", C_DIM);
      break;
    }
    case SCR_MENU: {
      cv.fillScreen(C_BG);
      cv.fillRect(0, 0, 240, 24, C_BAR);
      cv.setTextSize(2); cv.setTextColor(C_TXT, C_BAR); cv.setCursor(8, 5); cv.print("HEARTH");
      drawBatteryIcon(cv, 218, 8, C_BAR);
      line(30, "one menu - grouped: watch/system", C_DIM);
      int y = 48;
      for (int i = 0; i < kMenuCount; i++) {
        const bool sel = i == g_menuSel;
        if (sel) cv.fillRect(0, y - 2, 240, 11, C_SEL);
        cv.setTextSize(1);
        cv.setTextColor(kMenu[i].group[0] == 'W' ? C_ACC : C_WARN, sel ? C_SEL : C_BG);
        cv.setCursor(6, y); cv.print(kMenu[i].group);
        cv.setTextColor(sel ? C_WARN : C_TXT, sel ? C_SEL : C_BG);
        cv.setCursor(66, y); cv.print(fit(String(kMenu[i].label), 26));
        if (kMenu[i].target == SCR_LIVETGL) { cv.setTextColor(g_liveEnabled ? C_OK : C_BAD, sel ? C_SEL : C_BG); cv.print(g_liveEnabled ? " ON" : " OFF"); }
        if (kMenu[i].target == SCR_ASYNCTGL) { cv.setTextColor(g_autoSync ? C_OK : C_BAD, sel ? C_SEL : C_BG); cv.print(g_autoSync ? " ON" : " OFF"); }
        if (sel) { cv.setCursor(228, y); cv.print(">"); }
        y += 12;
      }
      foot(String("v") + kVer + " - arrows + ENTER - ESC");
      break;
    }
    case SCR_LIVE: {
      hdr("Sentinel LIVE");
      cv.setTextSize(2); cv.setTextColor(C_TXT, C_BG); cv.setCursor(4, 24); cv.print(uptimeStr());
      cv.setTextSize(1); cv.setTextColor(C_DIM, C_BG); cv.setCursor(130, 30); cv.print("uptime");
      if (g_scanning) {
        const int pct = min(99, (int)((millis() - g_scanStart) * 100 / 5000));
        line(52, "scanning...", C_ACC);
        progressBar(4, 64, 180, pct, C_OK);
      } else {
        line(52, "last scan: " + String(g_lastCount) + " APs  (#" + g_scanCount + ")", C_TXT);
        const int wait = (int)((g_nextScanMs - millis()) / 1000);
        line(64, "next scan in " + String(max(0, wait)) + " s", C_DIM);
      }
      line(80, "baseline: " + String(g_hasBaseline ? (unsigned)g_baseline.size() : 0) + " known APs", C_DIM);
      line(94, g_alerts.empty() ? "no new-AP alerts" : (String(g_alerts.size()) + " alert(s) - see Alerts"),
           g_alerts.empty() ? C_OK : C_BAD);
      line(108, "home: " + String(WiFi.status() == WL_CONNECTED ? WiFi.localIP().toString() : (g_homeSsid.length() ? "joining..." : "not set")) + (g_liveEnabled ? " - live on" : " - live off"), C_DIM);
      line(118, g_status, C_DIM);
      foot("ENTER scan now - ESC menu");
      break;
    }
    case SCR_ALERTS: {
      hdr("ALERTS - NEW APs");
      if (g_alerts.empty()) line(40, "none yet - baseline quiet", C_OK);
      const int rows = 7;
      int first = g_alertSel - rows / 2; if (first < 0) first = 0;
      if (first + rows > (int)g_alerts.size()) first = max(0, (int)g_alerts.size() - rows);
      int y = 26;
      for (int i = first; i < (int)g_alerts.size() && i < first + rows; i++) {
        const bool sel = i == g_alertSel;
        if (sel) cv.fillRect(0, y - 1, 240, 11, C_SEL);
        line(y, g_alerts[i], sel ? C_WARN : C_TXT);
        y += 11;
      }
      foot("UP/DN browse - ESC menu");
      break;
    }
    case SCR_BASE: {
      hdr("BASELINE");
      line(32, g_hasBaseline ? (String((unsigned)g_baseline.size()) + " BSSIDs learned") : "no baseline yet", g_hasBaseline ? C_OK : C_WARN);
      line(48, "first scan auto-learns.", C_DIM);
      line(62, "ENTER re-learns from the", C_TXT);
      line(74, "next scan (old one replaced)", C_TXT);
      line(90, "saved /AEGIS_HEARTH/baseline.txt", C_DIM);
      foot("ENTER re-learn - ESC menu");
      break;
    }
    case SCR_SYNC: {
      hdr("SYNC TO DESKTOP");
      line(32, g_status, C_ACC);
      line(48, WiFi.status() == WL_CONNECTED ? "home WiFi joined" : "join home WiFi first", WiFi.status() == WL_CONNECTED ? C_OK : C_WARN);
      line(62, "sends events + baseline", C_TXT);
      line(76, "to AEGIS COMPANION (HMAC auth)", C_DIM);
      line(90, "needs /AEGIS_LINK/desktop.txt", C_DIM);
      line(104, String("auto-sync ") + (g_autoSync ? "ON" : "off") + " - live " + (g_liveLinked ? "linked" : "idle"), g_liveLinked ? C_OK : C_DIM);
      foot("ENTER sync now - ESC menu");
      break;
    }
    case SCR_ABOUT: {
      hdr("ABOUT AEGIS HEARTH");
      line(30, "The family hearth.", C_ACC);
      line(44, "Plug it in, let it learn the", C_DIM);
      line(56, "house, and it taps you on the", C_DIM);
      line(68, "shoulder when a stranger AP", C_DIM);
      line(80, "appears. Wi-Fi only in v0.1.", C_DIM);
      line(96, "No keybinds: arrows + ENTER.", C_TXT);
      foot("ESC menu");
      break;
    }
  }
  cv.pushSprite(0, 0);
}

static void startScan() {
  if (g_scanning) return;
  WiFi.mode(WIFI_STA); WiFi.disconnect(true);   // scans preempt the home join
  WiFi.scanNetworks(true, false);
  g_scanning = true; g_scanStart = millis();
}

static void onKey(const String& word, bool enter, bool del, bool tab, bool fn) {
  (void)del; (void)tab;
  if (g_screen == SCR_SPLASH) { g_screen = SCR_MENU; draw(); return; }
  const bool esc = word.indexOf('`') >= 0 || (fn && word.indexOf('~') >= 0);
  if (esc) { if (g_screen != SCR_MENU) g_screen = SCR_MENU; draw(); return; }
  char c = word.length() ? word[0] : 0;
  const bool up = (c == ';' || c == ':'), down = (c == '.' || c == '>');
  switch (g_screen) {
    case SCR_MENU:
      if (up) g_menuSel = (g_menuSel + kMenuCount - 1) % kMenuCount;
      else if (down) g_menuSel = (g_menuSel + 1) % kMenuCount;
      else if (enter) {
        if (kMenu[g_menuSel].target == SCR_LIVETGL) g_liveEnabled = !g_liveEnabled;
        else if (kMenu[g_menuSel].target == SCR_ASYNCTGL) g_autoSync = !g_autoSync;
        else g_screen = kMenu[g_menuSel].target;
      }
      break;
    case SCR_LIVE:
      if (enter) startScan();
      break;
    case SCR_ALERTS:
      if (up && g_alertSel > 0) g_alertSel--;
      else if (down && g_alertSel + 1 < (int)g_alerts.size()) g_alertSel++;
      break;
    case SCR_BASE:
      if (enter) { g_hasBaseline = false; g_baseline.clear(); g_status = "will re-learn next scan"; startScan(); g_screen = SCR_LIVE; }
      break;
    case SCR_SYNC:
      if (enter) {
        runSync(kSyncFiles, 2, "HEARTH");
      }
      break;
    default: break;
  }
  draw();
}

void setup() {
  auto cfg = M5.config();
  M5Cardputer.begin(cfg, true);
  setCpuFrequencyMhz(80);
  Serial.begin(115200);
  Serial.println(kFw);
  cv.createSprite(240, 135);
  g_bootMs = millis();
  loadHomeProfile();
  SPI.begin(40, 39, 14);
  g_sdOk = SD.begin(12);
  if (g_sdOk) {
    if (!SD.exists("/AEGIS_HEARTH")) SD.mkdir("/AEGIS_HEARTH");
    loadBaseline(); loadDesktopCfg();
    File c = SD.open("/AEGIS_HEARTH/chain.txt", FILE_READ);
    if (c) { String h = c.readString(); h.trim(); g_chain = (uint32_t)strtoul(h.c_str(), nullptr, 16); c.close(); }
    logEvent("boot", "{\"fw\":\"aegis-hearth-0.1.5\"}");
  }
  liveBegin();
  draw();
}

void loop() {
  M5Cardputer.update();
  if (g_screen == SCR_SPLASH && millis() - g_bootMs > 1800) { g_screen = SCR_LIVE; draw(); }
  if (M5Cardputer.Keyboard.isChange() && M5Cardputer.Keyboard.isPressed()) {
    auto ks = M5Cardputer.Keyboard.keysState();
    String word; for (char ch : ks.word) word += ch;
    onKey(word, ks.enter, ks.del, ks.tab, ks.fn);
  }
  homeJoinTick(g_scanning);
  liveUpdate();
  autoSyncTick();
  if (!g_scanning && (int32_t)(millis() - g_nextScanMs) >= 0) startScan();
  if (g_scanning) {
    const int n = WiFi.scanComplete();
    if (n >= 0) { g_scanning = false; finishScan(n); WiFi.scanDelete(); if (g_screen == SCR_LIVE || g_screen == SCR_BASE) draw(); }
    else if (g_screen == SCR_LIVE && (millis() % 500) < 10) draw();
  }
  static uint32_t tick = 0;
  if (g_screen == SCR_LIVE && millis() - tick > 1000) { tick = millis(); draw(); }
  delay(5);
}
