// =====================================================================
// AEGIS SCRIBE v0.1.0 - field notes + evidence report companion for
// M5Stack Cardputer ADV. Third member of the AEGIS family (PHOENIX =
// defense, TALON = scoped assessment, SCRIBE = the written record).
//
// Reads PHOENIX (/AEGIS/data/event_index.jsonl) and TALON
// (/AEGIS_RT/evidence.jsonl) READ-ONLY, verifies their FNV-1a hash
// chains, lets the operator add timestamped notes/findings, and
// exports a clean Markdown + CSV report to /AEGIS_SCRIBE/.
//
// Input model matches the family standard (v0.9.x / r15+): ONE grouped
// menu, arrow keys + Enter only, Esc back/stop. No letter keybinds.
// Arrows use the hardware-proven ADV mapping: the printed cluster
// ; , . / with OR without Fn, shifted forms (: < > ?) included -
// the keyboard reports Fn+arrows as shifted punctuation and the Fn
// flag in KeysState is not reliable (PHOENIX/TALON r16 finding).
// In the note editor, TAB moves between fields so , . ; / stay
// typeable in text fields.
// =====================================================================
#include <M5Cardputer.h>
#include "chain_core.h"  // shared FNV-1a chain math (host-fuzzed, see firmware-host-tests)
#include <SD.h>
#include <SPI.h>
#include <ArduinoJson.h>
#include <WiFi.h>
#include <WiFiUdp.h>
#include <Preferences.h>
#include <vector>

static const char* kVer = "0.1.7";
static const char* kFw = "AEGIS SCRIBE 0.1.5";
static M5Canvas cv(&M5Cardputer.Display);

static const uint16_t C_BG = 0x0000, C_BAR = 0x180E, C_ACC = 0xC7FF,
                      C_DIM = 0x8410, C_TXT = 0xFFFF, C_SEL = 0x3010,
                      C_OK = 0x07E0, C_WARN = 0xFFE0, C_BAD = 0xF800,
                      C_CYAN = 0x07FF;

// ------------------------------------------------------------- battery
// Voltage-curve gauge (family standard): PMIC % is unreliable on the
// ADV, so percent comes from the Li-ion curve, slew-limited, frozen
// while charging. Black bolt on the icon while charging.
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
  if (g_batt.charging) {  // black bolt
    d.fillRect(x + 9, y + 2, 3, 4, 0x0000); d.fillRect(x + 8, y + 5, 3, 3, 0x0000);
  }
}

// ---------------------------------------------------------------- data
struct Item {
  String src;     // PHX / TAL / NOTE
  String title;
  String kind;
  int sev = 1;    // 0 info, 1 low, 2 med, 3 high
  bool resolved = false;
  bool note = false;
};
static std::vector<Item> g_items;
// Family pixel mark (16x16, full-bleed glyph - PHOENIX/TALON style).
static const char* kMark[16] = {
  "................",
  "............AA..",
  "...........A.A..",
  "..........A.A...",
  "...VVVVVVAVA....",
  "...VWWdWAWA.....",
  "...VWWdWWAV.....",
  "...VWddddWV.....",
  "...VWWdWWWV.....",
  "...VWddddWV.....",
  "...VWddddWV.....",
  "...VWWdWWWV.....",
  "...VVVVVVVV.....",
  "................",
  "................",
  "................"
};
static void drawMark(int cx, int cy, int sc) {
  for (int y = 0; y < 16; y++) for (int x = 0; x < 16; x++) {
    const char c = kMark[y][x];
    if (c == '.') continue;
    uint16_t col = C_ACC;
    if (c=='V') col=C_ACC; else if (c=='W') col=C_TXT; else if (c=='d') col=C_DIM; else if (c=='A') col=C_WARN;
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
static String g_status = "boot";

struct ChainStat { int lines = 0, chained = 0, bad = 0, unchained = 0; bool headOk = false; };
static ChainStat g_phxChain, g_talChain, g_noteStat;
static bool g_verified = false;

// The chain math lives in chain_core.h (single implementation, also
// fuzzed on the host). fnvHex stays as a String-flavoured shim for the
// note writer below.
static String fnvHex(uint32_t h) { return String(aegis_chain::fnvHex(h).c_str()); }

// Verify one family's chain file. blue lines end ...,"chain":"x"}
// (hashed body = line minus that suffix). Red/notes lines end the
// same but the hashed body needs its closing brace restored - the
// core handles both; this wrapper only moves lines from SD to it.
static ChainStat verifyChain(const char* path, const char* headPath) {
  ChainStat st;
  File f = SD.open(path, FILE_READ);
  if (!f) return st;
  std::vector<std::string> lines;
  while (f.available()) {
    String line = f.readStringUntil('\n');
    lines.emplace_back(line.c_str());
  }
  f.close();
  const aegis_chain::ChainStatCore core = aegis_chain::verifyChainLines(lines);
  st.lines = core.lines;
  st.chained = core.chained;
  st.bad = core.bad;
  st.unchained = core.unchained;
  if (headPath && SD.exists(headPath)) {
    File c = SD.open(headPath, FILE_READ);
    if (c) { String head = c.readString(); head.trim(); st.headOk = (head == fnvHex(core.head)); c.close(); }
  }
  return st;
}

static int sevFromConf(int conf) { return conf >= 85 ? 3 : conf >= 60 ? 2 : conf >= 30 ? 1 : 0; }

static void loadPhoenix() {
  File f = SD.open("/AEGIS/data/event_index.jsonl", FILE_READ);
  if (!f) return;
  int n = 0;
  while (f.available() && n < 80) {
    String line = f.readStringUntil('\n'); line.trim();
    if (!line.length()) continue;
    JsonDocument doc;
    if (deserializeJson(doc, line)) continue;
    Item it; it.src = "PHX";
    it.title = doc["title"] | "?"; it.kind = doc["kind"] | "?";
    it.sev = sevFromConf(doc["conf"] | 0); it.resolved = doc["rev"] | false;
    g_items.push_back(it); n++;
  }
  f.close();
}

static void loadTalon() {
  File f = SD.open("/AEGIS_RT/evidence.jsonl", FILE_READ);
  if (!f) return;
  int n = 0;
  while (f.available() && n < 80) {
    String line = f.readStringUntil('\n'); line.trim();
    if (!line.length()) continue;
    JsonDocument doc;
    if (deserializeJson(doc, line)) continue;
    Item it; it.src = "TAL"; it.kind = doc["kind"] | "?";
    it.title = it.kind;
    JsonObject fl = doc["fields"];
    if (!fl.isNull()) {
      if (fl["result"].is<const char*>()) it.title += ": " + String((const char*)fl["result"]);
      else if (fl["ssid"].is<const char*>()) it.title += ": " + String((const char*)fl["ssid"]);
    }
    it.sev = (it.kind == "accept" || it.kind == "boot") ? 0 : 1;
    g_items.push_back(it); n++;
  }
  f.close();
}

// ------------------------------------------------------------- notes
static uint32_t g_noteHead = 0x811c9dc5u;
static void noteChainLoad() {
  File c = SD.open("/AEGIS_SCRIBE/chain.txt", FILE_READ);
  if (c) { String h = c.readString(); h.trim(); g_noteHead = (uint32_t)strtoul(h.c_str(), nullptr, 16); c.close(); }
}
static void loadNotes() {
  File f = SD.open("/AEGIS_SCRIBE/notes.jsonl", FILE_READ);
  if (!f) return;
  while (f.available()) {
    String line = f.readStringUntil('\n'); line.trim();
    if (!line.length()) continue;
    JsonDocument doc;
    if (deserializeJson(doc, line)) continue;
    Item it; it.src = "NOTE"; it.note = true;
    it.title = doc["title"] | "(untitled)"; it.kind = doc["body"] | "";
    it.sev = doc["sev"] | 1; it.resolved = doc["done"] | false;
    g_items.push_back(it);
  }
  f.close();
}
static void reloadAll() {
  g_items.clear();
  if (!g_sdOk) return;
  loadPhoenix(); loadTalon(); loadNotes();
  g_status = String((unsigned)g_items.size()) + " findings loaded";
}
static void saveNote(const String& title, const String& body, int sev, bool done) {
  if (!g_sdOk) { g_status = "note failed: no SD"; return; }
  if (!SD.exists("/AEGIS_SCRIBE")) SD.mkdir("/AEGIS_SCRIBE");
  JsonDocument doc;
  doc["title"] = title; doc["body"] = body; doc["sev"] = sev; doc["done"] = done;
  doc["uptime_ms"] = millis();
  String bodyJson; serializeJson(doc, bodyJson);
  g_noteHead = aegis_chain::chainAdvance(g_noteHead, std::string(bodyJson.c_str()));
  File f = SD.open("/AEGIS_SCRIBE/notes.jsonl", FILE_APPEND);
  if (f) {
    f.print(bodyJson.substring(0, bodyJson.length() - 1) +
            ",\"chain\":\"" + fnvHex(g_noteHead) + "\"}\n");
    f.close();
  }
  SD.remove("/AEGIS_SCRIBE/chain.txt");
  File c = SD.open("/AEGIS_SCRIBE/chain.txt", FILE_WRITE);
  if (c) { c.print(fnvHex(g_noteHead)); c.close(); }
  g_status = "note saved";
}

// ----------------------------------------------------------------- UI
enum Screen { SCR_SPLASH, SCR_MENU, SCR_FIND, SCR_VERIFY, SCR_EDIT, SCR_REPORT, SCR_SD, SCR_ABOUT, SCR_SYNC, SCR_LIVETGL, SCR_ASYNCTGL };
static Screen g_screen = SCR_SPLASH;
static uint32_t g_bootMs = 0;

static String fit(const String& s, int n) { return s.length() > (size_t)n ? s.substring(0, n) : s; }
static void line(int y, const String& t, uint16_t col = C_TXT) {
  cv.setTextSize(1); cv.setTextColor(col, C_BG); cv.setCursor(4, y);
  cv.print(fit(t, 39));
}
static void hdr(const char* title) {
  cv.fillScreen(C_BG);
  cv.fillRect(0, 0, 240, 16, C_BAR); cv.fillRect(0, 16, 240, 2, C_ACC);
  cv.setTextSize(1); cv.setTextColor(C_TXT, C_BAR); cv.setCursor(4, 4);
  cv.print(fit(String(title), 26));
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
struct MenuItem { const char* group; const char* label; Screen target; int filter; };
static const MenuItem kMenu[] = {
  { "SOURCES", "All findings",        SCR_FIND,   0 },
  { "SOURCES", "Phoenix findings",    SCR_FIND,   1 },
  { "SOURCES", "Talon evidence",      SCR_FIND,   2 },
  { "SOURCES", "Verify hash chains",  SCR_VERIFY, 0 },
  { "WRITE",   "New note / finding",  SCR_EDIT,   0 },
  { "WRITE",   "My notes",            SCR_FIND,   3 },
  { "EXPORT",  "Export report MD+CSV",SCR_REPORT, 0 },
  { "EXPORT",  "Sync to desktop",    SCR_SYNC,   0 },
  { "EXPORT",  "Desktop live link",  SCR_LIVETGL, 0 },
  { "EXPORT",  "Desktop auto-sync",  SCR_ASYNCTGL, 0 },
  { "SYSTEM",  "SD + storage",        SCR_SD,     0 },
  { "SYSTEM",  "About SCRIBE",        SCR_ABOUT,  0 },
};
static const int kMenuCount = sizeof(kMenu) / sizeof(kMenu[0]);
static int g_menuSel = 0;
static int g_filter = 0;          // 0 all, 1 PHX, 2 TAL, 3 NOTE
static int g_findSel = 0;
static int g_chipSel = 0;

// editor state
static String g_edTitle, g_edBody;
static int g_edSev = 1, g_edField = 0; static bool g_edDone = false;
static const char* kSev[] = {"INFO", "LOW", "MED", "HIGH"};

static bool srcMatch(const Item& it) {
  if (g_filter == 1) return it.src == "PHX";
  if (g_filter == 2) return it.src == "TAL";
  if (g_filter == 3) return it.note;
  return true;
}
static std::vector<int> visibleIdx() {
  std::vector<int> v;
  for (size_t i = 0; i < g_items.size(); i++) if (srcMatch(g_items[i])) v.push_back((int)i);
  return v;
}

static String csvEsc(const String& s) {
  String o = "\"";
  for (size_t i = 0; i < s.length(); i++) { if (s[i] == '"') o += "\"\""; else o += s[i]; }
  return o + "\"";
}
static void drawReportProgress(int pct, const String& what) {
  hdr("EXPORT REPORT");
  line(34, what, C_ACC);
  progressBar(4, 48, 180, pct, C_OK);
  line(66, "writing to /AEGIS_SCRIBE/", C_DIM);
  cv.pushSprite(0, 0);
}
static void exportReport() {
  if (!g_sdOk) { g_status = "export failed: no SD"; return; }
  if (!SD.exists("/AEGIS_SCRIBE")) SD.mkdir("/AEGIS_SCRIBE");
  File md = SD.open("/AEGIS_SCRIBE/report.md", FILE_WRITE);
  File cf = SD.open("/AEGIS_SCRIBE/report.csv", FILE_WRITE);
  if (!md || !cf) { g_status = "export failed: open"; return; }
  md.println("# AEGIS field report"); md.print("generated by AEGIS SCRIBE "); md.println(kVer);
  md.print("device uptime (s): "); md.println(millis() / 1000);
  md.print("findings: "); md.println((unsigned)g_items.size());
  md.println(); md.println("| # | source | severity | status | title |");
  md.println("|---|--------|----------|--------|-------|");
  cf.println("n,source,severity,status,kind,title");
  const int n = g_items.size();
  for (int i = 0; i < n; i++) {
    const Item& it = g_items[i];
    md.printf("| %d | %s | %s | %s | %s |\n", i + 1, it.src.c_str(), kSev[it.sev],
              it.resolved ? "resolved" : "open", fit(it.title, 40).c_str());
    cf.printf("%d,%s,%s,%s,%s,%s\n", i + 1, it.src.c_str(), kSev[it.sev],
              it.resolved ? "resolved" : "open", csvEsc(it.kind).c_str(), csvEsc(it.title).c_str());
    if ((i % 8) == 0) drawReportProgress((i + 1) * 100 / max(1, n), "writing findings...");
  }
  md.println(); md.println("## chain verification");
  md.printf("- PHOENIX: %d lines, %d chained, %d bad, %d unchained\n",
            g_phxChain.lines, g_phxChain.chained, g_phxChain.bad, g_phxChain.unchained);
  md.printf("- TALON: %d lines, %d chained, %d bad, %d unchained, head %s\n",
            g_talChain.lines, g_talChain.chained, g_talChain.bad, g_talChain.unchained,
            g_talChain.headOk ? "MATCH" : "n/a");
  md.printf("- NOTES: %d lines, %d chained, %d bad, head %s\n",
            g_noteStat.lines, g_noteStat.chained, g_noteStat.bad,
            g_noteStat.headOk ? "MATCH" : "n/a");
  md.close(); cf.close();
  drawReportProgress(100, "done");
  g_status = "report exported";
}

static void runVerify() {
  g_phxChain = verifyChain("/AEGIS/data/event_index.jsonl", nullptr);
  g_talChain = verifyChain("/AEGIS_RT/evidence.jsonl", "/AEGIS_RT/chain.txt");
  g_noteStat = verifyChain("/AEGIS_SCRIBE/notes.jsonl", "/AEGIS_SCRIBE/chain.txt");
  g_verified = true;
}

// ---------------------------------------------------------------- draw

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
// stops working: broadcast a nonce, require an HMAC proof keyed by
// the shared token, adopt the answering address, write it back.
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
  {"/AEGIS/data/event_index.jsonl", "phoenix_event_index.jsonl"},
  {"/AEGIS_RT/evidence.jsonl", "talon_evidence.jsonl"},
  {"/AEGIS_SCRIBE/notes.jsonl", "notes.jsonl"},
  {"/AEGIS/logs/wardrive.csv", "phoenix_wardrive.csv"},
  {"/AEGIS_RT/wardrive.csv", "talon_wardrive.csv"},
  {"/AEGIS/data/wifi_map.csv", "wifi_map.csv"},
};
static bool syncFilesDirty() {
  String wm;
  File f = SD.open("/AEGIS_SCRIBE/.autosync", FILE_READ);
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
  File f = SD.open("/AEGIS_SCRIBE/.autosync", FILE_WRITE);
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
  if (g_screen == SCR_EDIT) return;            // never yank the editor
  if ((int32_t)(millis() - g_nextAutoSync) < 0) return;
  if (!syncFilesDirty()) { g_nextAutoSync = millis() + 60000; return; }
  if (!g_liveLinked) {
    WiFiClient probe;
    if (!g_deskHost.length() || !probe.connect(g_deskHost.c_str(), g_deskPort, 1200)) {
      g_nextAutoSync = millis() + 180000;
      return;
    }
    probe.stop();
  }
  const Screen prev = g_screen;
  const int stored = runSync(kSyncFiles, 6, "SCRIBE");
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
// Streams live status to AEGIS COMPANION while on home WiFi so the
// desktop dashboard shows this hub. Same /AEGIS_LINK/desktop.txt +
// HMAC auth as sync; one-way frames, ~2 s cadence, own task.
static bool g_liveEnabled = true;
struct LiveSnap { char state[16]; char detail[80]; char last[20]; int aps; int batt; int pct; uint32_t events; bool chg; };
static LiveSnap g_liveSnap = {"idle", "starting", "", 0, -1, -1, 0, false};
static String jsonEsc(const String& s) {
  String o; o.reserve(s.length() + 4);
  for (size_t i = 0; i < s.length(); i++) { const char c = s[i]; if (c == '"' || c == '\\') o += '\\'; if (c >= 32) o += c; }
  return o;
}
static void liveUpdate() {
  g_batt.update();
  LiveSnap s;
  const int bad = g_phxChain.bad + g_talChain.bad + g_noteStat.bad;
  if (g_syncPct >= 0) {
    strlcpy(s.state, "syncing", sizeof(s.state));
    snprintf(s.detail, sizeof(s.detail), "pushing logs to desktop - %u items", (unsigned)g_items.size());
    s.pct = g_syncPct;
  } else {
    strlcpy(s.state, "idle", sizeof(s.state));
    snprintf(s.detail, sizeof(s.detail), "hub: %u items - %s", (unsigned)g_items.size(),
             bad ? "CHAIN PROBLEM - verify!" : "all chains ok");
    s.pct = -1;
  }
  s.last[0] = 0;
  s.aps = 0;
  s.batt = g_batt.pct; s.chg = g_batt.charging;
  s.events = (uint32_t)g_items.size();
  g_liveSnap = s;
}
static void liveTaskFn(void*) {
  static char frame[360];
  for (;;) {
    if (!g_liveEnabled || !g_sdOk || WiFi.status() != WL_CONNECTED) { delay(2000); continue; }
    if (g_syncPct >= 0) { delay(1000); continue; }
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
      c.print("{\"auth\":\"" + hmacHex(g_deskToken, nonce) + "\",\"prog\":\"SCRIBE\",\"ver\":\"" + kVer + "\"}\n");
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
      drawMark(120, 42, 4);
      cv.setTextSize(2); cv.setTextColor(C_TXT, C_BG); cv.setCursor(48, 78); cv.print("AEGIS");
      cv.setTextColor(C_ACC, C_BG); cv.setCursor(128, 78); cv.print("SCRIBE");
      line(106, "field notes - evidence - reports", C_DIM);
      line(120, "the written record of the family", C_DIM);
      break;
    }
    case SCR_MENU: {
      cv.fillScreen(C_BG);
      cv.fillRect(0, 0, 240, 24, C_BAR);
      cv.setTextSize(2); cv.setTextColor(C_TXT, C_BAR); cv.setCursor(8, 5); cv.print("SCRIBE");
      drawBatteryIcon(cv, 218, 8, C_BAR);
      line(30, "one menu - grouped: sources/write/export", C_DIM);
      int y = 44;
      int mfirst = g_menuSel - 3; if (mfirst < 0) mfirst = 0;
      if (mfirst + 8 > kMenuCount) mfirst = max(0, kMenuCount - 8);
      for (int i = mfirst; i < kMenuCount && i < mfirst + 8; i++) {
        const bool sel = i == g_menuSel;
        if (sel) cv.fillRect(0, y - 2, 240, 11, C_SEL);
        cv.setTextSize(1);
        cv.setTextColor(kMenu[i].group[0] == 'S' && kMenu[i].group[1] == 'O' ? C_CYAN :
                        kMenu[i].group[0] == 'W' ? C_WARN : kMenu[i].group[0] == 'E' ? 0x87FF : C_ACC,
                        sel ? C_SEL : C_BG);
        cv.setCursor(6, y); cv.print(kMenu[i].group);
        cv.setTextColor(sel ? C_WARN : C_TXT, sel ? C_SEL : C_BG);
        cv.setCursor(66, y); cv.print(fit(String(kMenu[i].label), 26));
        if (kMenu[i].target == SCR_LIVETGL) { cv.setTextColor(g_liveEnabled ? C_OK : C_BAD, sel ? C_SEL : C_BG); cv.print(g_liveEnabled ? " ON" : " OFF"); }
        if (kMenu[i].target == SCR_ASYNCTGL) { cv.setTextColor(g_autoSync ? C_OK : C_BAD, sel ? C_SEL : C_BG); cv.print(g_autoSync ? " ON" : " OFF"); }
        if (sel) { cv.setCursor(228, y); cv.print(">"); }
        y += 11;
      }
      foot(String("v") + kVer + " - arrows + ENTER - ESC");
      break;
    }
    case SCR_FIND: {
      hdr(g_filter == 1 ? "PHOENIX FINDINGS" : g_filter == 2 ? "TALON EVIDENCE" :
          g_filter == 3 ? "MY NOTES" : "ALL FINDINGS");
      const auto vis = visibleIdx();
      if (vis.empty()) { line(40, "nothing here yet", C_WARN); line(54, g_status, C_DIM); }
      const int rows = 7;
      int first = g_findSel - rows / 2; if (first < 0) first = 0;
      if (first + rows > (int)vis.size()) first = max(0, (int)vis.size() - rows);
      int y = 26;
      for (int i = first; i < (int)vis.size() && i < first + rows; i++) {
        const Item& it = g_items[vis[i]];
        const bool sel = i == g_findSel;
        if (sel) cv.fillRect(0, y - 1, 240, 11, C_SEL);
        cv.setTextSize(1);
        cv.setTextColor(it.src == "PHX" ? C_CYAN : it.src == "TAL" ? C_WARN : C_ACC, sel ? C_SEL : C_BG);
        cv.setCursor(4, y); cv.print(it.src);
        cv.setTextColor(it.sev >= 3 ? C_BAD : it.sev == 2 ? C_WARN : C_TXT, sel ? C_SEL : C_BG);
        cv.setCursor(34, y); cv.print(fit(it.title, 28));
        cv.setTextColor(it.resolved ? C_OK : C_DIM, sel ? C_SEL : C_BG);
        cv.setCursor(208, y); cv.print(it.resolved ? "done" : kSev[it.sev]);
        y += 11;
      }
      foot(vis.empty() ? "ESC menu" : "UP/DN browse - ENTER toggles note done");
      break;
    }
    case SCR_VERIFY: {
      hdr("HASH CHAIN VERIFICATION");
      auto row = [&](int y, const char* name, const ChainStat& st, bool head) {
        const uint16_t col = st.bad ? C_BAD : (st.lines && st.chained == st.lines && (!head || st.headOk)) ? C_OK : C_WARN;
        line(y, String(name) + " " + st.lines + " lines " + st.chained + " chained " +
                st.bad + " bad" + (head ? (st.headOk ? " head OK" : " head ?") : ""), col);
      };
      if (!g_verified) { line(40, "press ENTER to verify", C_WARN); }
      else {
        row(34, "PHOENIX", g_phxChain, false);
        row(48, "TALON", g_talChain, true);
        row(62, "NOTES", g_noteStat, true);
        line(80, "bad > 0 = evidence edited after writing", C_DIM);
        line(92, "unchained lines are not tamper-proof", C_DIM);
      }
      foot("ENTER verify - ESC menu");
      break;
    }
    case SCR_EDIT: {
      hdr("NEW NOTE / FINDING");
      auto fld = [&](int idx, int y, const String& label, const String& val) {
        const bool f = g_edField == idx;
        if (f) cv.fillRect(0, y - 1, 240, 11, C_SEL);
        cv.setTextSize(1); cv.setTextColor(f ? C_ACC : C_DIM, f ? C_SEL : C_BG);
        cv.setCursor(4, y); cv.print(label);
        cv.setTextColor(C_TXT, f ? C_SEL : C_BG); cv.setCursor(52, y); cv.print(fit(val, 30));
      };
      fld(0, 28, "TITLE", g_edTitle + (g_edField == 0 ? "_" : ""));
      fld(1, 44, "DETAIL", g_edBody + (g_edField == 1 ? "_" : ""));
      fld(2, 60, "SEVERITY", String("< ") + kSev[g_edSev] + " >");
      fld(3, 76, "STATUS", g_edDone ? "resolved" : "open");
      fld(4, 92, "SAVE", "press ENTER to save note");
      line(110, "TAB next field - type in TITLE/DETAIL", C_DIM);
      foot("ESC cancel (note is not saved)");
      break;
    }
    case SCR_REPORT: {
      hdr("EXPORT REPORT");
      line(34, g_status, C_ACC);
      line(50, "report.md + report.csv", C_TXT);
      line(64, "in /AEGIS_SCRIBE/ on the SD card", C_DIM);
      foot("ENTER export again - ESC menu");
      break;
    }
    case SCR_SD: {
      hdr("SD + STORAGE");
      line(30, g_sdOk ? "SD mounted" : "SD NOT FOUND", g_sdOk ? C_OK : C_BAD);
      if (g_sdOk) {
        const double freeGb = (SD.totalBytes() - SD.usedBytes()) / 1073741824.0;
        line(44, "free " + String(freeGb, 1) + " GB", C_TXT);
        line(58, "PHOENIX index: " + String(SD.exists("/AEGIS/data/event_index.jsonl") ? "present" : "absent"), C_DIM);
        line(70, "TALON evidence: " + String(SD.exists("/AEGIS_RT/evidence.jsonl") ? "present" : "absent"), C_DIM);
        line(82, "SCRIBE notes: " + String(SD.exists("/AEGIS_SCRIBE/notes.jsonl") ? "present" : "none yet"), C_DIM);
        line(94, "SCRIBE reads PHX/TAL read-only", C_WARN);
        line(106, "home WiFi: " + String(g_homeSsid.length() ? g_homeSsid : "not set") + (WiFi.status() == WL_CONNECTED ? " " + WiFi.localIP().toString() : ""), C_DIM);
      }
      foot("ESC menu");
      break;
    }
    case SCR_SYNC: {
      hdr("SYNC TO DESKTOP");
      line(32, g_status, C_ACC);
      line(48, WiFi.status() == WL_CONNECTED ? "home WiFi joined" : "join home WiFi first", WiFi.status() == WL_CONNECTED ? C_OK : C_WARN);
      line(62, "sends PHX + TAL + notes + wardrive", C_TXT);
      line(76, "to AEGIS COMPANION (HMAC auth)", C_DIM);
      line(90, "needs /AEGIS_LINK/desktop.txt", C_DIM);
      foot("ENTER sync now - ESC menu");
      break;
    }
    case SCR_ABOUT: {
      hdr("ABOUT AEGIS SCRIBE");
      line(30, "Third family program: the record.", C_ACC);
      line(44, "PHOENIX defends. TALON assesses.", C_DIM);
      line(56, "SCRIBE writes it down and proves", C_DIM);
      line(68, "the evidence was not edited.", C_DIM);
      line(84, "No radios used. No keybinds:", C_TXT);
      line(96, "arrows + ENTER, ESC back.", C_TXT);
      foot("ESC menu");
      break;
    }
  }
  cv.pushSprite(0, 0);
}

// ---------------------------------------------------------------- input
static void onKey(const String& word, bool enter, bool del, bool tab, bool fn) {
  if (g_screen == SCR_SPLASH) { g_screen = SCR_MENU; draw(); return; }
  const bool esc = word.indexOf('`') >= 0 || (fn && word.indexOf('~') >= 0);
  char c = word.length() ? word[0] : 0;
  // Arrow cluster: with or without Fn, shifted forms included.
  // In the editor's text fields these characters TYPE instead;
  // TAB moves between fields there, so nothing is lost.
  const bool inText = g_screen == SCR_EDIT && (g_edField == 0 || g_edField == 1);
  bool up = false, down = false, left = false, right = false;
  if (!inText) {
    up = (c == ';' || c == ':'); down = (c == '.' || c == '>');
    left = (c == ',' || c == '<'); right = (c == '/' || c == '?');
  }
  if (esc) {
    if (g_screen == SCR_MENU) { draw(); return; }
    g_screen = SCR_MENU; draw(); return;
  }
  switch (g_screen) {
    case SCR_MENU:
      if (up) g_menuSel = (g_menuSel + kMenuCount - 1) % kMenuCount;
      else if (down) g_menuSel = (g_menuSel + 1) % kMenuCount;
      else if (enter) {
        const MenuItem& m = kMenu[g_menuSel];
        if (m.target == SCR_LIVETGL) { g_liveEnabled = !g_liveEnabled; draw(); break; }
        if (m.target == SCR_ASYNCTGL) { g_autoSync = !g_autoSync; draw(); break; }
        g_screen = m.target;
        if (m.target == SCR_FIND) { g_filter = m.filter; g_findSel = 0; }
        if (m.target == SCR_EDIT) { g_edTitle = ""; g_edBody = ""; g_edSev = 1; g_edField = 0; g_edDone = false; }
        if (m.target == SCR_REPORT) { runVerify(); exportReport(); g_screen = SCR_REPORT; }
        if (m.target == SCR_VERIFY && !g_verified) runVerify();
      }
      break;
    case SCR_FIND: {
      const auto vis = visibleIdx();
      if (up && g_findSel > 0) g_findSel--;
      else if (down && g_findSel + 1 < (int)vis.size()) g_findSel++;
      else if (left) { g_filter = (g_filter + 3) % 4; g_findSel = 0; }
      else if (right) { g_filter = (g_filter + 1) % 4; g_findSel = 0; }
      else if (enter && !vis.empty()) {
        Item& it = g_items[vis[g_findSel]];
        if (it.note) {  // notes are ours: toggle + persist by rewrite marker in status line
          it.resolved = !it.resolved;
          g_status = "note marked " + String(it.resolved ? "resolved" : "open") + " (session)";
        } else g_status = "source records are read-only";
      }
      break;
    }
    case SCR_VERIFY:
      if (enter) runVerify();
      break;
    case SCR_EDIT:
      if (tab) { g_edField = (g_edField + 1) % 5; }
      else if (g_edField == 2 && (left || right)) g_edSev = (g_edSev + (right ? 1 : 3)) % 4;
      else if (g_edField == 3 && (left || right || enter)) { if (!enter || g_edField == 3) g_edDone = !g_edDone; if (enter) g_edField = 4; }
      else if (enter && g_edField == 4) {
        saveNote(g_edTitle.length() ? g_edTitle : String("(untitled)"), g_edBody, g_edSev, g_edDone);
        reloadAll(); g_filter = 3; g_findSel = 0; g_screen = SCR_FIND;
      } else if (enter && (g_edField == 0 || g_edField == 1)) { g_edField++; }
      else if (del) {
        if (g_edField == 0 && g_edTitle.length()) g_edTitle.remove(g_edTitle.length() - 1);
        if (g_edField == 1 && g_edBody.length()) g_edBody.remove(g_edBody.length() - 1);
      } else if (inText && word.length()) {
        if (g_edField == 0 && g_edTitle.length() < 48) g_edTitle += word;
        if (g_edField == 1 && g_edBody.length() < 96) g_edBody += word;
      }
      break;
    case SCR_REPORT:
      if (enter) { runVerify(); exportReport(); }
      break;
    case SCR_SYNC:
      if (enter) {
        runSync(kSyncFiles, 6, "SCRIBE");
      }
      break;
    default: break;
  }
  (void)up; (void)down;
  draw();
}

// ----------------------------------------------------------------- main
static String g_serLine;
void setup() {
  auto cfg = M5.config();
  M5Cardputer.begin(cfg, true);
  setCpuFrequencyMhz(80);
  Serial.begin(115200);
  Serial.println(kFw);
  cv.createSprite(240, 135);
  if (M5Cardputer.Display.width() != 240 || M5Cardputer.Display.height() != 135)
    Serial.printf("[WARN] display geometry %dx%d\n", M5Cardputer.Display.width(), M5Cardputer.Display.height());
  g_bootMs = millis();
  loadHomeProfile();
  SPI.begin(40, 39, 14);
  g_sdOk = SD.begin(12);
  if (g_sdOk) {
    if (!SD.exists("/AEGIS_SCRIBE")) SD.mkdir("/AEGIS_SCRIBE");
    noteChainLoad(); loadDesktopCfg();
    reloadAll();
    runVerify();
  } else g_status = "no SD - notes/reports disabled";
  liveBegin();
  draw();
}

void loop() {
  M5Cardputer.update();
  homeJoinTick(false);
  liveUpdate();
  autoSyncTick();
  if (g_screen == SCR_SPLASH && millis() - g_bootMs > 1800) { g_screen = SCR_MENU; draw(); }
  if (M5Cardputer.Keyboard.isChange() && M5Cardputer.Keyboard.isPressed()) {
    auto ks = M5Cardputer.Keyboard.keysState();
    String word; for (char ch : ks.word) word += ch;
    onKey(word, ks.enter, ks.del, ks.tab, ks.fn);
  }
  while (Serial.available()) {   // bench CLI: STATUS / VERIFY / EXPORT / RELOAD
    char ch = (char)Serial.read();
    if (ch == '\n') {
      g_serLine.trim();
      if (g_serLine == "STATUS")
        Serial.printf("+SCRIBE: {\"items\":%u,\"sd\":%s,\"ver\":\"%s\"}\n",
                      (unsigned)g_items.size(), g_sdOk ? "true" : "false", kVer);
      else if (g_serLine == "VERIFY") {
        runVerify();
        Serial.printf("+SCRIBE: {\"phx_bad\":%d,\"tal_bad\":%d,\"note_bad\":%d,\"tal_head\":%s}\n",
                      g_phxChain.bad, g_talChain.bad, g_noteStat.bad, g_talChain.headOk ? "true" : "false");
      } else if (g_serLine == "EXPORT") { runVerify(); exportReport(); Serial.println("+SCRIBE: exported"); }
      else if (g_serLine == "RELOAD") { reloadAll(); Serial.println("+SCRIBE: reloaded"); }
      else if (g_serLine.length()) Serial.println("+SCRIBE: commands STATUS VERIFY EXPORT RELOAD");
      g_serLine = "";
      draw();
    } else if (ch != '\r' && g_serLine.length() < 64) g_serLine += ch;
  }
  static uint32_t battTick = 0;              // keep the gauge fresh
  if (millis() - battTick > 5000 && g_screen == SCR_MENU) { battTick = millis(); draw(); }
  delay(5);
}
