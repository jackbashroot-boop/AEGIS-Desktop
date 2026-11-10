// =====================================================================
// AEGIS LANTERN v0.1.0 - LoRa/GNSS off-grid field companion for
// M5Stack Cardputer ADV + Cap. Waypoints, track logging, backtrack
// bearing/distance, and a LoRa receive meter (SX1262 via the family
// driver). Input = family standard: one grouped menu, arrows +
// Enter, Esc back. No letter keybinds.
//
// v0.1 scope (honest): LoRa is RECEIVE/meter only - the shared family
// driver exposes no transmit path yet, so there is no beacon/TX here
// rather than a fake one. GNSS comes from the Cap (G15/G13, 115200).
// =====================================================================
#include <M5Cardputer.h>
#include <SD.h>
#include <SPI.h>
#include <WiFi.h>
#include <WiFiUdp.h>
#include <Preferences.h>
#include <math.h>
#include <vector>
#include "lora_hat.h"

LoRaHatDriver loraHat;
static const char* kVer = "0.1.3";
static const char* kFw = "AEGIS LANTERN 0.1.3";
static M5Canvas cv(&M5Cardputer.Display);
static const uint16_t C_BG = 0x0000, C_BAR = 0x2100, C_ACC = 0xFD20,
                      C_DIM = 0x8410, C_TXT = 0xFFFF, C_SEL = 0x2010,
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
  "......A..A......",
  "......AAAA......",
  ".....AAAAAA.....",
  "....gA....Ag....",
  "....gA.WW.Ag....",
  "....gA.WW.Ag....",
  "....gA.WW.Ag....",
  ".....A....A.....",
  ".....AAAAAA.....",
  ".......AA.......",
  "................",
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
    if (c=='A') col=C_ACC; else if (c=='W') col=C_WARN; else if (c=='g') col=0x8200;
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

static String g_status = "boot";
static void progressBar(int x, int y, int w, int pct, uint16_t col) {
  if (pct < 0) pct = 0; if (pct > 100) pct = 100;
  cv.drawRect(x, y, w, 7, C_DIM);
  if (pct > 0) cv.fillRect(x + 1, y + 1, (w - 2) * pct / 100, 5, col);
  cv.setTextSize(1); cv.setTextColor(C_TXT, C_BG);
  cv.setCursor(x + w + 4, y); cv.print(String(pct) + "%");
}

// ---------------------------------------------------------- desktop sync
// (v0.1.3: same Companion link as SCRIBE - discovery + auto-sync.)
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
// stops working: broadcast a nonce, require an HMAC proof keyed by
// the shared token, adopt the answering address, write it back.
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
static const SyncFile kSyncFiles[] = {
  {"/AEGIS_LANTERN/track.csv", "lantern_track.csv"},
  {"/AEGIS_LANTERN/waypoints.csv", "lantern_waypoints.csv"},
};
static bool syncFilesDirty() {
  String wm;
  File f = SD.open("/AEGIS_LANTERN/.autosync", FILE_READ);
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
  File f = SD.open("/AEGIS_LANTERN/.autosync", FILE_WRITE);
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
  {  // no live link in this program: probe first
    WiFiClient probe;
    if (!g_deskHost.length() || !probe.connect(g_deskHost.c_str(), g_deskPort, 1200)) {
      g_nextAutoSync = millis() + 180000;
      return;
    }
    probe.stop();
  }
  const int stored = runSync(kSyncFiles, 2, "LANTERN");
  if (stored >= 0) { writeSyncWatermark(); g_nextAutoSync = millis() + 900000; }
  else g_nextAutoSync = millis() + 180000;
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


// ------------------------------------------------------------ GNSS/nav
static bool g_fix = false; static int g_sats = 0;
static double g_lat = 0, g_lon = 0;
static int g_nmeaCount = 0;
struct Waypoint { String name; double lat, lon; };
static std::vector<Waypoint> g_wps;
static int g_wpSel = 0, g_target = -1;
static int g_trackCount = 0;

static double nmeaToDeg(const String& v, const String& hemi) {
  if (!v.length()) return 0;
  const double raw = v.toDouble();
  const int deg = (int)(raw / 100);
  double out = deg + (raw - deg * 100) / 60.0;
  if (hemi == "S" || hemi == "W") out = -out;
  return out;
}
static double distM(double la1, double lo1, double la2, double lo2) {
  const double R = 6371000.0, dLa = (la2 - la1) * DEG_TO_RAD, dLo = (lo2 - lo1) * DEG_TO_RAD;
  const double a = sin(dLa / 2) * sin(dLa / 2) + cos(la1 * DEG_TO_RAD) * cos(la2 * DEG_TO_RAD) * sin(dLo / 2) * sin(dLo / 2);
  return 2 * R * asin(sqrt(a));
}
static double bearingDeg(double la1, double lo1, double la2, double lo2) {
  const double dLo = (lo2 - lo1) * DEG_TO_RAD;
  const double y = sin(dLo) * cos(la2 * DEG_TO_RAD);
  const double x = cos(la1 * DEG_TO_RAD) * sin(la2 * DEG_TO_RAD) - sin(la1 * DEG_TO_RAD) * cos(la2 * DEG_TO_RAD) * cos(dLo);
  double b = atan2(y, x) / DEG_TO_RAD;
  if (b < 0) b += 360;
  return b;
}
static const char* compass(double b) {
  static const char* pts[8] = {"N", "NE", "E", "SE", "S", "SW", "W", "NW"};
  return pts[((int)((b + 22.5) / 45)) % 8];
}
static void saveWaypoints() {
  if (!g_sdOk) return;
  if (!SD.exists("/AEGIS_LANTERN")) SD.mkdir("/AEGIS_LANTERN");
  File f = SD.open("/AEGIS_LANTERN/waypoints.csv", FILE_WRITE);
  if (!f) return;
  f.println("name,lat,lon");
  for (auto& w : g_wps) f.printf("%s,%.6f,%.6f\n", w.name.c_str(), w.lat, w.lon);
  f.close();
}
static void loadWaypoints() {
  File f = SD.open("/AEGIS_LANTERN/waypoints.csv", FILE_READ);
  if (!f) return;
  g_wps.clear();
  while (f.available()) {
    String s = f.readStringUntil('\n'); s.trim();
    if (!s.length() || s.startsWith("name,")) continue;
    const int a = s.indexOf(','), b = s.indexOf(',', a + 1);
    if (a < 0 || b < 0) continue;
    Waypoint w; w.name = s.substring(0, a); w.lat = s.substring(a + 1, b).toDouble(); w.lon = s.substring(b + 1).toDouble();
    g_wps.push_back(w);
  }
  f.close();
}
static void logTrackPoint() {
  if (!g_sdOk || !g_fix) { g_status = g_fix ? "no SD" : "no fix yet"; return; }
  if (!SD.exists("/AEGIS_LANTERN")) SD.mkdir("/AEGIS_LANTERN");
  const bool fresh = !SD.exists("/AEGIS_LANTERN/track.csv");
  File f = SD.open("/AEGIS_LANTERN/track.csv", FILE_APPEND);
  if (f) {
    if (fresh) f.println("uptime_s,lat,lon,sats");
    f.printf("%lu,%.6f,%.6f,%d\n", millis() / 1000, g_lat, g_lon, g_sats);
    f.close(); g_trackCount++; g_status = "track point logged";
  }
}

enum Screen { SCR_SPLASH, SCR_MENU, SCR_NAV, SCR_WPS, SCR_TRACK, SCR_LORA, SCR_ABOUT };
static Screen g_screen = SCR_SPLASH;
static uint32_t g_bootMs = 0;
struct MenuItem { const char* group; const char* label; Screen target; };
static const MenuItem kMenu[] = {
  { "NAV",    "Position + backtrack", SCR_NAV },
  { "NAV",    "Waypoints",            SCR_WPS },
  { "NAV",    "Track log",            SCR_TRACK },
  { "RADIO",  "LoRa receive meter",   SCR_LORA },
  { "SYSTEM", "About LANTERN",        SCR_ABOUT },
};
static const int kMenuCount = sizeof(kMenu) / sizeof(kMenu[0]);
static int g_menuSel = 0;
static String g_lastPkt = "-";
static void onLoraPacket(const String& hexData, int rssi, float snr) {
  g_lastPkt = hexData.substring(0, 24) + " " + rssi + "dBm";
}

static void draw() {
  switch (g_screen) {
    case SCR_SPLASH: {
      cv.fillScreen(C_BG);
      drawMark(120, 40, 4);
      cv.setTextSize(2); cv.setTextColor(C_TXT, C_BG); cv.setCursor(44, 76); cv.print("AEGIS");
      cv.setTextColor(C_ACC, C_BG); cv.setCursor(120, 76); cv.print("LANTERN");
      line(102, "waypoints - track - LoRa ears", C_DIM);
      line(116, "find your way back", C_DIM);
      break;
    }
    case SCR_MENU: {
      cv.fillScreen(C_BG);
      cv.fillRect(0, 0, 240, 24, C_BAR);
      cv.setTextSize(2); cv.setTextColor(C_TXT, C_BAR); cv.setCursor(8, 5); cv.print("LANTERN");
      drawBatteryIcon(cv, 218, 8, C_BAR);
      line(30, "one menu - grouped: nav/radio/system", C_DIM);
      int y = 48;
      for (int i = 0; i < kMenuCount; i++) {
        const bool sel = i == g_menuSel;
        if (sel) cv.fillRect(0, y - 2, 240, 11, C_SEL);
        cv.setTextSize(1);
        cv.setTextColor(kMenu[i].group[0] == 'N' ? C_ACC : kMenu[i].group[0] == 'R' ? C_CYAN : C_WARN,
                        sel ? C_SEL : C_BG);
        cv.setCursor(6, y); cv.print(kMenu[i].group);
        cv.setTextColor(sel ? C_WARN : C_TXT, sel ? C_SEL : C_BG);
        cv.setCursor(66, y); cv.print(fit(String(kMenu[i].label), 26));
        if (sel) { cv.setCursor(228, y); cv.print(">"); }
        y += 12;
      }
      foot(String("v") + kVer + " - arrows + ENTER - ESC");
      break;
    }
    case SCR_NAV: {
      hdr("POSITION + BACKTRACK");
      line(28, g_fix ? ("FIX  sats " + String(g_sats)) : "no fix yet - go outside", g_fix ? C_OK : C_WARN);
      line(42, "lat " + String(g_lat, 6), C_TXT);
      line(54, "lon " + String(g_lon, 6), C_TXT);
      if (g_target >= 0 && g_target < (int)g_wps.size() && g_fix) {
        const Waypoint& w = g_wps[g_target];
        const double d = distM(g_lat, g_lon, w.lat, w.lon), b = bearingDeg(g_lat, g_lon, w.lat, w.lon);
        line(72, "to " + w.name + ":", C_ACC);
        cv.setTextSize(2); cv.setTextColor(C_TXT, C_BG); cv.setCursor(4, 84);
        cv.print((d >= 1000 ? String(d / 1000, 2) + " km" : String((int)d) + " m") + " " + String((int)b) + compass(b));
      } else line(72, g_wps.empty() ? "no waypoints yet" : "pick a target in Waypoints", C_DIM);
      line(96, "home: " + String(WiFi.status() == WL_CONNECTED ? WiFi.localIP().toString() : (g_homeSsid.length() ? "joining..." : "not set")), C_DIM);
      line(108, g_status, C_DIM);
      foot("ESC menu");
      break;
    }
    case SCR_WPS: {
      hdr("WAYPOINTS");
      if (g_wps.empty()) line(36, "none saved yet", C_WARN);
      const int rows = 6;
      int first = g_wpSel - rows / 2; if (first < 0) first = 0;
      if (first + rows > (int)g_wps.size()) first = max(0, (int)g_wps.size() - rows);
      int y = 26;
      for (int i = first; i < (int)g_wps.size() && i < first + rows; i++) {
        const bool sel = i == g_wpSel;
        if (sel) cv.fillRect(0, y - 1, 240, 11, C_SEL);
        cv.setTextSize(1); cv.setTextColor(sel ? C_WARN : C_TXT, sel ? C_SEL : C_BG);
        cv.setCursor(4, y);
        cv.print(String(i == g_target ? "* " : "  ") + fit(g_wps[i].name, 12) + " " +
                 String(g_wps[i].lat, 4) + "," + String(g_wps[i].lon, 4));
        y += 11;
      }
      line(104, g_fix ? "ENTER target - TAB saves here" : "need a fix to save points", g_fix ? C_DIM : C_WARN);
      foot("UP/DN pick - ENTER target - ESC");
      break;
    }
    case SCR_TRACK: {
      hdr("TRACK LOG");
      line(34, "points this session: " + String(g_trackCount), C_TXT);
      line(50, "/AEGIS_LANTERN/track.csv", C_DIM);
      line(66, g_fix ? "ENTER logs the current position" : "no fix yet", g_fix ? C_ACC : C_WARN);
      line(84, g_status, C_DIM);
      foot("ENTER log point - ESC menu");
      break;
    }
    case SCR_LORA: {
      hdr("LORA RECEIVE METER 915");
      if (!loraHat.active) {
        line(36, "SX1262 not detected", C_WARN);
        line(52, "Cap absent or RF path off.", C_DIM);
        line(66, "GNSS/nav still work without it.", C_DIM);
      } else {
        line(32, "listening (RX only in v0.1)", C_OK);
        line(46, "channel RSSI " + String(loraHat.channelRssi) + " dBm", C_TXT);
        line(60, "packets: " + String((unsigned)loraHat.packetsCaptured), C_TXT);
        line(74, "last: " + g_lastPkt, C_DIM);
        line(90, "TX beacon needs a driver send", C_DIM);
        line(102, "path - planned, not faked here", C_DIM);
      }
      foot("ESC menu");
      break;
    }
    case SCR_ABOUT: {
      hdr("ABOUT AEGIS LANTERN");
      line(30, "The family guide.", C_ACC);
      line(44, "Waypoints, backtrack bearings,", C_DIM);
      line(56, "track logs, and LoRa ears for", C_DIM);
      line(68, "the Cap. No keybinds.", C_DIM);
      line(84, "915 MHz US default (family cfg).", C_WARN);
      foot("ESC menu");
      break;
    }
  }
  cv.pushSprite(0, 0);
}

static void onKey(const String& word, bool enter, bool del, bool tab, bool fn) {
  (void)del;
  if (g_screen == SCR_SPLASH) { g_screen = SCR_MENU; draw(); return; }
  const bool esc = word.indexOf('`') >= 0 || (fn && word.indexOf('~') >= 0);
  if (esc) { if (g_screen != SCR_MENU) g_screen = SCR_MENU; draw(); return; }
  char c = word.length() ? word[0] : 0;
  const bool up = (c == ';' || c == ':'), down = (c == '.' || c == '>'),
             left = (c == ',' || c == '<'), right = (c == '/' || c == '?');
  (void)left; (void)right;
  switch (g_screen) {
    case SCR_MENU:
      if (up) g_menuSel = (g_menuSel + kMenuCount - 1) % kMenuCount;
      else if (down) g_menuSel = (g_menuSel + 1) % kMenuCount;
      else if (enter) g_screen = kMenu[g_menuSel].target;
      break;
    case SCR_WPS:
      if (up && g_wpSel > 0) g_wpSel--;
      else if (down && g_wpSel + 1 < (int)g_wps.size()) g_wpSel++;
      else if (enter && !g_wps.empty()) { g_target = g_wpSel; g_status = "target: " + g_wps[g_target].name; g_screen = SCR_NAV; }
      else if (tab && g_fix) {
        Waypoint w; w.name = "WP" + String((unsigned)(g_wps.size() + 1)); w.lat = g_lat; w.lon = g_lon;
        g_wps.push_back(w); saveWaypoints(); g_status = "saved " + w.name;
      }
      break;
    case SCR_TRACK:
      if (enter) logTrackPoint();
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
  if (g_sdOk) { loadWaypoints(); loadDesktopCfg(); }
  Serial2.begin(115200, SERIAL_8N1, 15, 13);
  loraHat.begin();
  draw();
}

void loop() {
  M5Cardputer.update();
  if (g_screen == SCR_SPLASH && millis() - g_bootMs > 1800) { g_screen = SCR_MENU; draw(); }
  if (M5Cardputer.Keyboard.isChange() && M5Cardputer.Keyboard.isPressed()) {
    auto ks = M5Cardputer.Keyboard.keysState();
    String word; for (char ch : ks.word) word += ch;
    onKey(word, ks.enter, ks.del, ks.tab, ks.fn);
  }
  homeJoinTick(false);
  autoSyncTick();
  loraHat.poll(onLoraPacket);
  while (Serial2.available()) {
    String s = Serial2.readStringUntil('\n'); s.trim();
    if (!s.length()) continue;
    g_nmeaCount++;
    if (s.startsWith("$GPGGA") || s.startsWith("$GNGGA")) {
      String f[10]; int idx = 0; String rest = s;
      while (idx < 10 && rest.indexOf(',') >= 0) { int p = rest.indexOf(','); f[idx++] = rest.substring(0, p); rest = rest.substring(p + 1); }
      if (idx >= 8) {
        g_lat = nmeaToDeg(f[2], f[3]);
        g_lon = nmeaToDeg(f[4], f[5]);
        g_fix = f[6].toInt() > 0;
        g_sats = f[7].toInt();
      }
    }
    if ((g_screen == SCR_NAV || g_screen == SCR_WPS) && (g_nmeaCount % 5) == 0) draw();
  }
  delay(5);
}
