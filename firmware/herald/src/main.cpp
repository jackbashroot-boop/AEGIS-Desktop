// =====================================================================
// AEGIS HERALD v0.1.0 - big-screen Sentinel/dashboard for the CYD
// (ESP32-2432S028, 2.8" 320x240 landscape touch). The family's loud
// voice: HEARTH-style Wi-Fi baseline watching with new-AP alerts on a
// readable always-on screen.
//
// Standalone v0.1: own Wi-Fi scans + baseline (/AEGIS_HERALD on its
// microSD), hash-chained alert log (family FNV-1a format), touch tab
// bar (BOOT button also cycles tabs - calibration-proof), optional
// NTP clock when /AEGIS_HERALD/wifi.txt gives SSID/PASS. Receive-only
// scanning; nothing connects to other devices. AEGIS-LINK companion
// packets from HEARTH/PHOENIX are a planned step 2 - not in v0.1.
// TALON is never linked, by design. (v0.1.2: HERALD itself now
// streams live status to AEGIS COMPANION and syncs its event log
// automatically - same AEGIS/1 link as the Cardputer family.)
// =====================================================================
#include <TFT_eSPI.h>
#include <XPT2046_Touchscreen.h>
#include <SD.h>
#include <WiFi.h>
#include <WiFiUdp.h>
#include <mbedtls/md.h>
#include <mbedtls/sha256.h>
#include <mbedtls/base64.h>
#include <time.h>
#include <vector>

static const char* kVer = "0.1.5";
static const char* kFw = "AEGIS HERALD 0.1.2";
static TFT_eSPI tft;
static SPIClass touchSPI(VSPI);
static XPT2046_Touchscreen ts(33, 36);   // T_CS, T_IRQ

static const uint16_t C_BG = TFT_BLACK, C_BAR = 0x3000, C_ACC = 0xFA20,
                      C_DIM = 0x8410, C_TXT = TFT_WHITE, C_SEL = 0x0015,
                      C_OK = TFT_GREEN, C_WARN = TFT_YELLOW, C_BAD = TFT_RED,
                      C_CYAN = TFT_CYAN;

// Family pixel mark: herald flag + sound arcs (16x16).
static const char* kMark[16] = {
  "................",
  ".....OOOO.......",
  "....OOOOOO......",
  "....OOOOOOOO.A..",
  "....OOOOOO...A..",
  "....OOOO.....A..",
  "....OO.......A..",
  "....O...........",
  "....P...........",
  "....P.....A.....",
  "....P...........",
  "....P...........",
  "....P...........",
  "..PPPPP.........",
  "................",
  "................",
};
static void drawMark(int cx, int cy, int sc) {
  for (int y = 0; y < 16; y++) for (int x = 0; x < 16; x++) {
    const char c = kMark[y][x];
    if (c == '.') continue;
    const uint16_t col = c == 'O' ? C_ACC : c == 'A' ? C_CYAN : C_TXT;
    tft.fillRect(cx - 8 * sc + x * sc, cy - 8 * sc + y * sc, sc, sc, col);
  }
}

static bool g_sdOk = false;
static String fit(const String& s, int n) { return s.length() > (size_t)n ? s.substring(0, n) : s; }
static void line(int y, const String& t, uint16_t col = C_TXT, int x = 6) {
  tft.setTextSize(1); tft.setTextColor(col, C_BG); tft.setCursor(x, y); tft.print(fit(t, 50));
}
static void foot(const String& t);   // fwd

// ------------------------------------------------------------- state
static std::vector<String> g_baseline, g_alerts, g_apList;
static bool g_hasBaseline = false, g_scanning = false;
static uint32_t g_scanStart = 0, g_nextScanMs = 8000, g_chain = 0x811c9dc5u;
static int g_lastCount = -1, g_scanCount = 0, g_tab = 0;
static String g_status = "boot";
static bool g_wifiJoined = false;
static const char* kTabs[4] = {"STATUS", "ALERTS", "AP LIST", "ABOUT"};

static String fnvHex(uint32_t h) { char b[9]; snprintf(b, sizeof(b), "%08x", h); return String(b); }
static uint32_t g_eventCount = 0;
static char g_lastKind[20] = "";
static void logEvent(const String& kind, const String& fieldsJson) {
  g_eventCount++;
  strlcpy(g_lastKind, kind.c_str(), sizeof(g_lastKind));
  if (!g_sdOk) return;
  if (!SD.exists("/AEGIS_HERALD")) SD.mkdir("/AEGIS_HERALD");
  String body = "{\"kind\":\"" + kind + "\",\"uptime_ms\":" + String(millis()) + ",\"fields\":" + fieldsJson + "}";
  uint32_t h = g_chain;
  for (size_t i = 0; i < body.length(); i++) { h ^= (uint8_t)body[i]; h *= 0x01000193u; }
  g_chain = h;
  File f = SD.open("/AEGIS_HERALD/events.jsonl", FILE_APPEND);
  if (f) { f.print(body.substring(0, body.length() - 1) + ",\"chain\":\"" + fnvHex(h) + "\"}\n"); f.close(); }
  SD.remove("/AEGIS_HERALD/chain.txt");
  File c = SD.open("/AEGIS_HERALD/chain.txt", FILE_WRITE);
  if (c) { c.print(fnvHex(h)); c.close(); }
}
static void saveBaseline() {
  if (!g_sdOk) return;
  if (!SD.exists("/AEGIS_HERALD")) SD.mkdir("/AEGIS_HERALD");
  File f = SD.open("/AEGIS_HERALD/baseline.txt", FILE_WRITE);
  if (!f) return;
  for (auto& b : g_baseline) f.println(b);
  f.close(); g_hasBaseline = true;
}
static void loadBaseline() {
  File f = SD.open("/AEGIS_HERALD/baseline.txt", FILE_READ);
  if (!f) return;
  g_baseline.clear();
  while (f.available()) { String s = f.readStringUntil('\n'); s.trim(); if (s.length()) g_baseline.push_back(s); }
  f.close(); g_hasBaseline = !g_baseline.empty();
}
static bool inBaseline(const String& b) { for (auto& x : g_baseline) if (x == b) return true; return false; }

static void finishScan(int n) {
  g_lastCount = n; g_scanCount++;
  g_apList.clear();
  std::vector<String> seen;
  int newN = 0;
  for (int i = 0; i < n; i++) {
    const String b = WiFi.BSSIDstr(i);
    seen.push_back(b);
    g_apList.push_back(fit(WiFi.SSID(i), 18) + " " + WiFi.RSSI(i) + "dBm ch" + WiFi.channel(i));
    if (g_hasBaseline && !inBaseline(b)) {
      newN++;
      g_alerts.push_back("NEW " + fit(WiFi.SSID(i), 16) + " " + b);
      if (g_alerts.size() > 40) g_alerts.erase(g_alerts.begin());
      logEvent("new_ap", "{\"ssid\":\"" + WiFi.SSID(i) + "\",\"bssid\":\"" + b + "\"}");
    }
  }
  if (!g_hasBaseline) { g_baseline = seen; saveBaseline(); g_status = "baseline learned"; logEvent("baseline", "{\"aps\":" + String(n) + "}"); }
  logEvent("scan", "{\"aps\":" + String(n) + ",\"new\":" + String(newN) + "}");
  g_nextScanMs = millis() + 60000;
}
static void startScan() {
  if (g_scanning) return;
  if (!g_wifiJoined) { WiFi.mode(WIFI_STA); WiFi.disconnect(true); }
  WiFi.scanNetworks(true, false);
  g_scanning = true; g_scanStart = millis();
}

// ------------------------------------------------------- desktop link
// v0.1.2: HERALD joins the family link. Discovers AEGIS COMPANION by
// HMAC-proven broadcast (address survives DHCP changes), streams
// live watch status to the desktop Live Monitor, and auto-syncs its
// event log + baseline. /AEGIS_LINK/desktop.txt comes from the
// desktop's Settings page, same as the Cardputer cards.
static String g_deskHost, g_deskToken;
static int g_deskPort = 8791;
static bool g_liveLinked = false;
static int g_syncPct = -1;
static uint32_t g_nextAutoSync = 0, g_lastDiscoverMs = 0;
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
static void saveDesktopCfg() {
  if (!g_sdOk) return;
  if (!SD.exists("/AEGIS_LINK")) SD.mkdir("/AEGIS_LINK");
  File f = SD.open("/AEGIS_LINK/desktop.txt", FILE_WRITE);
  if (!f) return;
  f.print("host=" + g_deskHost + "\nport=" + String(g_deskPort) + "\ntoken=" + g_deskToken + "\n");
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
static bool liveReadLine(WiFiClient& c, String& out) {
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
static bool deskAuth(WiFiClient& c) {
  String ln;
  if (!liveReadLine(c, ln) || ln.indexOf("AEGIS/1") < 0) return false;
  const int np = ln.indexOf("\"nonce\":\"");
  const String nonce = np >= 0 ? ln.substring(np + 9, ln.indexOf("\"", np + 9)) : "";
  c.print("{\"auth\":\"" + hmacHex(g_deskToken, nonce) + "\",\"prog\":\"HERALD\",\"ver\":\"" + kVer + "\"}\n");
  return liveReadLine(c, ln) && (ln.indexOf("\"ok\": true") >= 0 || ln.indexOf("\"ok\":true") >= 0);
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
    if (udp.parsePacket() > 0) {
      char buf[160]; const int n = udp.read(buf, sizeof(buf) - 1); buf[max(0, n)] = 0;
      String r(buf);
      if (r.startsWith("AEGIS_HERE 1 ")) {
        const int sp = r.lastIndexOf(' ');
        String macT = r.substring(sp + 1); macT.trim();
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
struct LiveSnap { char state[16]; char detail[80]; char last[20]; int aps; uint32_t events; };
static LiveSnap g_liveSnap = {"watching", "starting", "", 0, 0};
static void liveUpdate() {
  LiveSnap s;
  if (g_scanning) { strlcpy(s.state, "scanning", sizeof(s.state)); strlcpy(s.detail, "Wi-Fi scan in progress", sizeof(s.detail)); }
  else if (!g_alerts.empty()) { strlcpy(s.state, "alert", sizeof(s.state)); snprintf(s.detail, sizeof(s.detail), "%u new-AP alert(s)", (unsigned)g_alerts.size()); }
  else { strlcpy(s.state, "watching", sizeof(s.state)); snprintf(s.detail, sizeof(s.detail), "baseline watch - %d known APs", g_hasBaseline ? (int)g_baseline.size() : 0); }
  strlcpy(s.last, g_lastKind, sizeof(s.last));
  s.aps = g_lastCount >= 0 ? g_lastCount : (int)g_baseline.size();
  s.events = g_eventCount;
  g_liveSnap = s;
}
static String jsonEsc(const String& s) {
  String o; o.reserve(s.length() + 4);
  for (size_t i = 0; i < s.length(); i++) { const char c = s[i]; if (c == '"' || c == '\\') o += '\\'; if (c >= 32) o += c; }
  return o;
}
static void liveTaskFn(void*) {
  static char frame[320];
  for (;;) {
    if (!g_sdOk || WiFi.status() != WL_CONNECTED || g_syncPct >= 0) { delay(2000); continue; }
    if (!g_deskHost.length()) { loadDesktopCfg(); delay(3000); continue; }
    WiFiClient c;
    if (!c.connect(g_deskHost.c_str(), g_deskPort, 4000)) {
      if (millis() - g_lastDiscoverMs > 30000) {
        g_lastDiscoverMs = millis();
        if (discoverDesktop()) { delay(500); continue; }
      }
      delay(5000); continue;
    }
    bool ok = deskAuth(c);
    if (ok) {
      c.print("{\"live_start\":true}\n");
      String ln;
      ok = liveReadLine(c, ln) && ln.indexOf("live_ok") >= 0;
    }
    g_liveLinked = ok;
    while (ok && c.connected() && WiFi.status() == WL_CONNECTED && g_syncPct < 0) {
      const LiveSnap s = g_liveSnap;
      const int n = snprintf(frame, sizeof(frame),
        "{\"type\":\"live\",\"state\":\"%s\",\"detail\":\"%s\",\"aps\":%d,\"batt\":100,\"chg\":true,\"events\":%lu,\"last\":\"%s\"}\n",
        s.state, jsonEsc(String(s.detail)).c_str(), s.aps, (unsigned long)s.events, s.last);
      if (n <= 0 || c.print(frame) == 0) break;
      delay(2000);
    }
    g_liveLinked = false;
    c.stop();
    delay(3000);
  }
}
// auto-sync of the event log + baseline (watermark by size)
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
struct SyncFile { const char* path; const char* name; };
static const SyncFile kSyncFiles[] = {
  {"/AEGIS_HERALD/events.jsonl", "events.jsonl"},
  {"/AEGIS_HERALD/baseline.txt", "baseline.txt"},
};
static bool syncFilesDirty() {
  String wm;
  File f = SD.open("/AEGIS_HERALD/.autosync", FILE_READ);
  if (f) { wm = f.readString(); f.close(); }
  for (auto& sf : kSyncFiles) {
    File x = SD.open(sf.path, FILE_READ);
    const long sz = x ? (long)x.size() : -1;
    if (x) x.close();
    if (sz > 0 && wm.indexOf(String(sf.name) + "=" + String(sz) + "\n") < 0) return true;
  }
  return false;
}
static void writeSyncWatermark() {
  File f = SD.open("/AEGIS_HERALD/.autosync", FILE_WRITE);
  if (!f) return;
  for (auto& sf : kSyncFiles) {
    File x = SD.open(sf.path, FILE_READ);
    const long sz = x ? (long)x.size() : -1;
    if (x) x.close();
    if (sz > 0) f.print(String(sf.name) + "=" + String(sz) + "\n");
  }
  f.close();
}
static bool pushFile(WiFiClient& c, const SyncFile& sf) {
  uint32_t sz; const String sha = fileSha256(sf.path, sz);
  if (!sz || !sha.length()) return true;   // nothing to send
  String ln;
  c.print("{\"file\":\"" + String(sf.name) + "\",\"size\":" + String(sz) + ",\"sha256\":\"" + sha + "\"}\n");
  if (!liveReadLine(c, ln) || (ln.indexOf("\"send\": true") < 0 && ln.indexOf("\"send\":true") < 0)) return true;  // already there
  File f = SD.open(sf.path, FILE_READ);
  if (!f) return false;
  uint8_t buf[3072]; size_t n; uint32_t sent = 0;
  while ((n = f.read(buf, sizeof(buf))) > 0) {
    uint8_t b64[4200]; size_t olen = 0;
    mbedtls_base64_encode(b64, sizeof(b64), &olen, buf, n);
    b64[olen] = 0;
    c.print("{\"data\":\"" + String((const char*)b64) + "\"}\n");
    sent += n;
    g_syncPct = (int)(sent * 100 / sz);
  }
  f.close();
  c.print("{\"eof\":true}\n");
  return liveReadLine(c, ln) && (ln.indexOf("\"stored\": true") >= 0 || ln.indexOf("\"stored\":true") >= 0);
}
static void autoSyncTick() {
  if (!g_sdOk || WiFi.status() != WL_CONNECTED || g_syncPct >= 0) return;
  if ((int32_t)(millis() - g_nextAutoSync) < 0) return;
  if (!syncFilesDirty()) { g_nextAutoSync = millis() + 60000; return; }
  if (!g_liveLinked) {
    WiFiClient probe;
    if (!g_deskHost.length() || !probe.connect(g_deskHost.c_str(), g_deskPort, 1200)) { g_nextAutoSync = millis() + 180000; return; }
    probe.stop();
  }
  g_syncPct = 0;
  WiFiClient c;
  bool ok = c.connect(g_deskHost.c_str(), g_deskPort, 4000) && deskAuth(c);
  if (ok) ok = pushFile(c, kSyncFiles[0]) && pushFile(c, kSyncFiles[1]);
  if (ok) { c.print("{\"done\":true}\n"); String ln; liveReadLine(c, ln); writeSyncWatermark(); }
  c.stop();
  g_syncPct = -1;
  g_nextAutoSync = millis() + (ok ? 900000 : 180000);
}
static void liveBegin() { xTaskCreatePinnedToCore(liveTaskFn, "aegislive", 6144, nullptr, 1, nullptr, 0); }

// ----------------------------------------------------------------- UI
static String clockStr() {
  if (g_wifiJoined) {
    struct tm tmv;
    if (getLocalTime(&tmv, 5)) { char b[12]; snprintf(b, sizeof(b), "%02d:%02d:%02d", tmv.tm_hour, tmv.tm_min, tmv.tm_sec); return String(b); }
  }
  const uint32_t s = millis() / 1000;
  char b[16]; snprintf(b, sizeof(b), "%02lu:%02lu:%02lu", s / 3600, (s / 60) % 60, s % 60);
  return String(b);
}
static void progressBar(int x, int y, int w, int pct, uint16_t col) {
  if (pct < 0) pct = 0; if (pct > 100) pct = 100;
  tft.drawRect(x, y, w, 9, C_DIM);
  if (pct > 0) tft.fillRect(x + 1, y + 1, (w - 2) * pct / 100, 7, col);
  tft.setTextSize(1); tft.setTextColor(C_TXT, C_BG); tft.setCursor(x + w + 6, y + 1); tft.print(String(pct) + "%");
}
static void drawTabs() {
  tft.fillRect(0, 214, 320, 26, C_BAR);
  for (int i = 0; i < 4; i++) {
    const bool sel = i == g_tab;
    if (sel) tft.fillRect(i * 80, 214, 80, 26, C_SEL);
    tft.setTextSize(1); tft.setTextColor(sel ? C_WARN : C_DIM, sel ? C_SEL : C_BAR);
    tft.setCursor(i * 80 + 8, 223); tft.print(kTabs[i]);
  }
}
static void foot(const String& t) { line(200, t, C_DIM); }

static void draw() {
  tft.fillScreen(C_BG);
  tft.fillRect(0, 0, 320, 22, C_BAR);
  tft.setTextSize(1); tft.setTextColor(C_TXT, C_BAR); tft.setCursor(6, 7); tft.print("AEGIS HERALD");
  tft.setTextColor(C_DIM, C_BAR); tft.setCursor(250, 7); tft.print(kVer);
  if (g_tab == 0) {
    tft.setTextSize(4); tft.setTextColor(C_TXT, C_BG); tft.setCursor(6, 32); tft.print(clockStr());
    tft.setTextSize(1); tft.setTextColor(C_DIM, C_BG); tft.setCursor(232, 44); tft.print(g_wifiJoined ? "NTP" : "uptime");
    if (g_scanning) {
      const int pct = min(99, (int)((millis() - g_scanStart) * 100 / 5000));
      line(84, "scanning the airwaves...", C_ACC);
      progressBar(6, 100, 220, pct, C_OK);
    } else {
      line(84, "APs last scan: " + String(g_lastCount) + "   scans #" + g_scanCount, C_TXT);
      const int wait = (int)((g_nextScanMs - millis()) / 1000);
      line(100, "next scan in " + String(max(0, wait)) + " s", C_DIM);
    }
    line(124, "baseline: " + String(g_hasBaseline ? (unsigned)g_baseline.size() : 0) + " known APs", C_TXT);
    line(142, g_alerts.empty() ? "no new-AP alerts - house is quiet" : (String(g_alerts.size()) + " ALERT(S) - see ALERTS tab"),
         g_alerts.empty() ? C_OK : C_BAD);
    line(160, "SD " + String(g_sdOk ? "logging" : "ABSENT"), g_sdOk ? C_DIM : C_BAD);
    line(169, String("desktop: ") + (g_liveLinked ? "linked - streaming" : (g_deskHost.length() ? "auto-sync on" : "no desktop.txt")), g_liveLinked ? C_OK : C_DIM);
    line(178, g_status, C_DIM);
  } else if (g_tab == 1) {
    tft.setTextSize(2); tft.setTextColor(C_ACC, C_BG); tft.setCursor(6, 30); tft.print("ALERTS");
    if (g_alerts.empty()) line(64, "none yet", C_OK);
    int y = 60;
    const int start = max(0, (int)g_alerts.size() - 9);
    for (int i = start; i < (int)g_alerts.size() && y < 198; i++) { line(y, g_alerts[i], C_WARN); y += 15; }
  } else if (g_tab == 2) {
    tft.setTextSize(2); tft.setTextColor(C_ACC, C_BG); tft.setCursor(6, 30); tft.print("AP LIST");
    int y = 60;
    const int start = max(0, (int)g_apList.size() - 9);
    for (int i = start; i < (int)g_apList.size() && y < 198; i++) { line(y, g_apList[i], C_TXT); y += 15; }
    if (g_apList.empty()) line(64, "no scan yet", C_WARN);
  } else {
    drawMark(56, 62, 3);
    tft.setTextSize(2); tft.setTextColor(C_TXT, C_BG); tft.setCursor(110, 40); tft.print("HERALD");
    line(46, "the family's loud voice", C_ACC, 110);
    line(84, "Big-screen Sentinel for the CYD.", C_DIM);
    line(102, "Touch tabs or press BOOT to cycle.", C_DIM);
    line(120, "Optional clock: put SSID=/PASS=", C_DIM);
    line(138, "in /AEGIS_HERALD/wifi.txt on SD.", C_DIM);
    line(156, "TALON is never linked. Ever.", C_WARN);
  }
  drawTabs();
}

// ----------------------------------------------------------------- main
static String g_wifiSsidCfg, g_wifiPassCfg;
static void tryJoinWiFi() {
  // Own config first, then the family-shared SD profile (opt-in
  // plaintext sharing for the CYD pair): /AEGIS_LINK/wifi.txt.
  File f = SD.open("/AEGIS_HERALD/wifi.txt", FILE_READ);
  if (!f) f = SD.open("/AEGIS_LINK/wifi.txt", FILE_READ);
  if (!f) return;
  String ssid, pass;
  while (f.available()) {
    String s = f.readStringUntil('\n'); s.trim();
    if (s.startsWith("SSID=")) ssid = s.substring(5);
    if (s.startsWith("PASS=")) pass = s.substring(5);
  }
  f.close();
  if (!ssid.length()) return;
  g_wifiSsidCfg = ssid; g_wifiPassCfg = pass;
  WiFi.mode(WIFI_STA); WiFi.begin(ssid.c_str(), pass.c_str());
  const uint32_t t0 = millis();
  while (WiFi.status() != WL_CONNECTED && millis() - t0 < 9000) delay(50);
  g_wifiJoined = WiFi.status() == WL_CONNECTED;
  if (g_wifiJoined) { configTime(0, 0, "pool.ntp.org"); setenv("TZ", "EST5EDT,M3.2.0,M11.1.0", 1); tzset(); }
  Serial.printf("[HERALD] wifi joined=%d\n", g_wifiJoined);
}

void setup() {
  Serial.begin(115200);
  Serial.println(kFw);
  tft.init(); tft.setRotation(1);          // 320x240 landscape
  tft.fillScreen(C_BG);
  drawMark(160, 70, 5);
  tft.setTextSize(2); tft.setTextColor(C_TXT, C_BG); tft.setCursor(96, 132); tft.print("AEGIS");
  tft.setTextColor(C_ACC, C_BG); tft.setCursor(168, 132); tft.print("HERALD");
  line(162, "big-screen Sentinel - CYD 2432S028", C_DIM, 52);
  pinMode(0, INPUT_PULLUP);                // BOOT button cycles tabs
  g_sdOk = SD.begin(5);
  touchSPI.begin(25, 39, 32, 33);
  ts.begin(touchSPI); ts.setRotation(1);
  if (g_sdOk) {
    if (!SD.exists("/AEGIS_HERALD")) SD.mkdir("/AEGIS_HERALD");
    loadBaseline();
    File c = SD.open("/AEGIS_HERALD/chain.txt", FILE_READ);
    if (c) { String h = c.readString(); h.trim(); g_chain = (uint32_t)strtoul(h.c_str(), nullptr, 16); c.close(); }
    tryJoinWiFi();
    loadDesktopCfg();
    logEvent("boot", "{\"fw\":\"aegis-herald-0.1.5\"}");
  }
  liveBegin();
  draw();
}

// Failover (v0.1.1): if the home join drops, retry with backoff
// (5s -> 60s cap) without disturbing the scan schedule.
static void netTick() {
  static uint32_t nextTry = 0, backoff = 5000;
  if (!g_wifiSsidCfg.length()) return;   // no wifi.txt configured
  if (WiFi.status() == WL_CONNECTED) { g_wifiJoined = true; backoff = 5000; return; }
  // v0.1.5: also retry when the BOOT join failed - previously a failed
  // first join meant HERALD never came back on the network at all.
  if ((int32_t)(millis() - nextTry) < 0) return;
  WiFi.mode(WIFI_STA);
  WiFi.begin(g_wifiSsidCfg.c_str(), g_wifiPassCfg.c_str());
  g_wifiJoined = false;
  nextTry = millis() + backoff;
  backoff = min<uint32_t>(backoff * 2 + 5000, 60000);
}

static String g_serLine;
void loop() {
  netTick();
  liveUpdate();
  autoSyncTick();
  static bool bootWas = true;
  const bool bootNow = digitalRead(0) == LOW;
  if (bootWas && !bootNow) { /* release edge unused */ }
  if (!bootNow && bootWas) { g_tab = (g_tab + 1) % 4; draw(); }
  bootWas = bootNow;

  if (ts.touched()) {
    TS_Point pt = ts.getPoint();
    // Raw XPT2046 (resistive) -> landscape 320x240. Defaults suit most
    // 2432S028R panels; BOOT button is the calibration-proof fallback.
    const int sx = map(pt.y, 3800, 250, 0, 320);
    const int sy = map(pt.x, 250, 3800, 0, 240);
    if (sy >= 214 && sx >= 0 && sx < 320) { const int t = sx / 80; if (t != g_tab) { g_tab = t; draw(); } }
    delay(60);
  }

  if (!g_scanning && (int32_t)(millis() - g_nextScanMs) >= 0) startScan();
  if (g_scanning) {
    const int n = WiFi.scanComplete();
    if (n >= 0) { g_scanning = false; finishScan(n); WiFi.scanDelete(); draw(); }
  }
  static uint32_t tick = 0;
  if (g_tab == 0 && millis() - tick > 1000) { tick = millis(); draw(); }

  while (Serial.available()) {
    char ch = (char)Serial.read();
    if (ch == '\n') {
      g_serLine.trim();
      if (g_serLine == "STATUS")
        Serial.printf("+HERALD: {\"aps\":%d,\"alerts\":%u,\"baseline\":%u,\"ver\":\"%s\"}\n",
                      g_lastCount, (unsigned)g_alerts.size(), (unsigned)g_baseline.size(), kVer);
      else if (g_serLine == "SCAN") startScan();
      else if (g_serLine == "BASE") { g_hasBaseline = false; g_baseline.clear(); startScan(); }
      else if (g_serLine.startsWith("TAB")) { g_tab = (g_serLine.substring(3).toInt()) % 4; draw(); }
      else if (g_serLine.length()) Serial.println("+HERALD: commands STATUS SCAN BASE TAB<n>");
      g_serLine = "";
    } else if (ch != '\r' && g_serLine.length() < 64) g_serLine += ch;
  }
  delay(5);
}
