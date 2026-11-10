// =====================================================================
// AEGIS KEEPER v0.1.0 - SD + firmware maintenance for the AEGIS
// family on M5Stack Cardputer ADV. Verifies SHA-256 of .bin files on
// the SD card (with progress), backs up family configs (never gate
// or shadow secrets), cleans known scratch files, and reports SD
// health. Evidence files are never deleted by this program.
//
// Input = family standard: one grouped menu, arrows + Enter, Esc.
// =====================================================================
#include <M5Cardputer.h>
#include <SD.h>
#include <SPI.h>
#include <mbedtls/sha256.h>
#include <Preferences.h>
#include <vector>

static const char* kVer = "0.1.3";
static const char* kFw = "AEGIS KEEPER 0.1.2";
static M5Canvas cv(&M5Cardputer.Display);
static const uint16_t C_BG = 0x0000, C_BAR = 0x0020, C_ACC = 0x07FF,
                      C_DIM = 0x8410, C_TXT = 0xFFFF, C_SEL = 0x0015,
                      C_OK = 0x07E0, C_WARN = 0xFFE0, C_BAD = 0xF800;

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
  if (fill > 0) d.fillRect(x + 2, y + 2, fill, 6, g_batt.charging ? C_ACC : C_OK);
  if (g_batt.charging) { d.fillRect(x + 9, y + 2, 3, 4, 0x0000); d.fillRect(x + 8, y + 5, 3, 3, 0x0000); }
}

// Family pixel mark (16x16, full-bleed glyph - PHOENIX/TALON style).
static const char* kMark[16] = {
  "....CCCC........",
  "..CC....CC......",
  "..C......C......",
  "..C......C......",
  "..CC....CC......",
  "....CCCC........",
  "......CC........",
  "......CC........",
  "......CCC.......",
  "......CC........",
  "......CC........",
  "......CCCC......",
  "......CC........",
  "................",
  "................",
  "................"
};
static void drawMark(int cx, int cy, int sc) {
  for (int y = 0; y < 16; y++) for (int x = 0; x < 16; x++) {
    const char c = kMark[y][x];
    if (c == '.') continue;
    uint16_t col = C_ACC;
    col=C_ACC;
    cv.fillRect(cx - 8 * sc + x * sc, cy - 8 * sc + y * sc, sc, sc, col);
  }
}


// Shared home profile (family "aegisnet" NVS - set once in PHOENIX).
static String g_homeSsid, g_homePsk;
static void loadHomeProfile() {
  Preferences np;
  if (np.begin("aegisnet", true)) { g_homeSsid = np.getString("ssid", ""); g_homePsk = np.getString("psk", ""); np.end(); }
}
static bool g_sdOk = false;
static String g_status = "boot";
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

// ------------------------------------------------------------- files
struct FEntry { String path; uint32_t size; };
static std::vector<FEntry> g_files;
static int g_fileSel = 0;
static void scanDir(const char* dir) {
  File d = SD.open(dir);
  if (!d) return;
  while (true) {
    File f = d.openNextFile();
    if (!f) break;
    if (!f.isDirectory() && g_files.size() < 48) {
      String p = String(dir) + (String(dir) == "/" ? "" : "/") + f.name();
      g_files.push_back({p, (uint32_t)f.size()});
    }
    f.close();
  }
  d.close();
}
static void scanFiles() {
  g_files.clear();
  if (!g_sdOk) return;
  scanDir("/"); scanDir("/AEGIS"); scanDir("/AEGIS_RT");
  scanDir("/AEGIS_SCRIBE"); scanDir("/AEGIS_BENCH"); scanDir("/AEGIS_LANTERN");
  if (g_fileSel >= (int)g_files.size()) g_fileSel = 0;
}
static bool isBin(const String& p) { return p.endsWith(".bin"); }

// ------------------------------------------------------------- sha256
static bool g_hashBusy = false; static int g_hashPct = 0;
static String g_hashPath, g_hashHex;
static File g_hashFile; static mbedtls_sha256_context g_sha;
static uint32_t g_hashTotal = 0;
static void hashStart(const String& path) {
  g_hashFile = SD.open(path, FILE_READ);
  if (!g_hashFile) { g_status = "open failed"; return; }
  mbedtls_sha256_init(&g_sha); mbedtls_sha256_starts(&g_sha, 0);
  g_hashTotal = g_hashFile.size(); g_hashPath = path; g_hashHex = "";
  g_hashBusy = true; g_hashPct = 0;
}
static void hashStep() {
  if (!g_hashBusy) return;
  uint8_t buf[2048];
  const size_t n = g_hashFile.read(buf, sizeof(buf));
  if (n) mbedtls_sha256_update(&g_sha, buf, n);
  const uint32_t pos = g_hashFile.position();
  g_hashPct = g_hashTotal ? (int)(pos * 100 / g_hashTotal) : 100;
  if (!g_hashFile.available() || n == 0) {
    uint8_t out[32]; mbedtls_sha256_finish(&g_sha, out); mbedtls_sha256_free(&g_sha);
    g_hashFile.close(); g_hashBusy = false;
    char hex[65]; for (int i = 0; i < 32; i++) snprintf(hex + i * 2, 3, "%02x", out[i]);
    g_hashHex = hex;
    Serial.printf("[KEEPER] sha256 %s = %s\n", g_hashPath.c_str(), g_hashHex.c_str());
    g_status = "hash done (full on serial)";
  }
}

// ------------------------------------------------------------- backup
static const char* kBackupSrc[] = {
  "/AEGIS/config.json", "/AEGIS/rules/disabled.txt",
  "/AEGIS/data/event_index.jsonl", "/AEGIS_SCRIBE/notes.jsonl",
};
static String doBackup() {
  if (!g_sdOk) return "no SD";
  const String dir = "/AEGIS_KEEPER/backup-" + String(millis() / 1000);
  SD.mkdir("/AEGIS_KEEPER"); SD.mkdir(dir);
  int copied = 0;
  for (size_t i = 0; i < sizeof(kBackupSrc) / sizeof(kBackupSrc[0]); i++) {
    if (!SD.exists(kBackupSrc[i])) continue;
    File in = SD.open(kBackupSrc[i], FILE_READ);
    if (!in) continue;
    String name = String(kBackupSrc[i]); name.replace('/', '_');
    File out = SD.open(dir + "/" + name, FILE_WRITE);
    if (!out) { in.close(); continue; }
    uint8_t buf[1024]; size_t n;
    uint32_t guard = 0;
    while ((n = in.read(buf, sizeof(buf))) > 0 && guard < 256) { out.write(buf, n); guard++; }
    in.close(); out.close(); copied++;
    hdr("BACKUP CONFIGS"); line(40, "copying " + String(kBackupSrc[i]), C_ACC);
    progressBar(4, 56, 180, (int)((i + 1) * 100 / 4), C_OK); cv.pushSprite(0, 0);
  }
  return "backed up " + String(copied) + " file(s) to " + dir;
}

enum Screen { SCR_SPLASH, SCR_MENU, SCR_SD, SCR_FILES, SCR_HASH, SCR_BACKUP, SCR_CLEAN, SCR_ABOUT };
static Screen g_screen = SCR_SPLASH;
static uint32_t g_bootMs = 0;
struct MenuItem { const char* group; const char* label; Screen target; };
static const MenuItem kMenu[] = {
  { "STORAGE", "SD overview",          SCR_SD },
  { "STORAGE", "File browser",         SCR_FILES },
  { "VERIFY",  "SHA-256 a .bin file",  SCR_FILES },
  { "CARE",    "Backup family configs",SCR_BACKUP },
  { "CARE",    "Clean scratch files",  SCR_CLEAN },
  { "SYSTEM",  "About KEEPER",         SCR_ABOUT },
};
static const int kMenuCount = sizeof(kMenu) / sizeof(kMenu[0]);
static int g_menuSel = 0;
static bool g_cleanDone = false;

static void draw() {
  switch (g_screen) {
    case SCR_SPLASH: {
      cv.fillScreen(C_BG);
      drawMark(120, 34, 4);
      cv.setTextSize(2); cv.setTextColor(C_TXT, C_BG); cv.setCursor(52, 70); cv.print("AEGIS");
      cv.setTextColor(C_ACC, C_BG); cv.setCursor(124, 70); cv.print("KEEPER");
      line(96, "hashes - backups - tidy SD", C_DIM);
      line(110, "evidence is never deleted here", C_DIM);
      break;
    }
    case SCR_MENU: {
      cv.fillScreen(C_BG);
      cv.fillRect(0, 0, 240, 24, C_BAR);
      cv.setTextSize(2); cv.setTextColor(C_TXT, C_BAR); cv.setCursor(8, 5); cv.print("KEEPER");
      drawBatteryIcon(cv, 218, 8, C_BAR);
      line(30, "one menu - grouped: storage/verify/care", C_DIM);
      int y = 48;
      for (int i = 0; i < kMenuCount; i++) {
        const bool sel = i == g_menuSel;
        if (sel) cv.fillRect(0, y - 2, 240, 11, C_SEL);
        cv.setTextSize(1);
        cv.setTextColor(kMenu[i].group[0] == 'S' ? C_ACC : kMenu[i].group[0] == 'V' ? C_WARN : 0x87FF,
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
    case SCR_SD: {
      hdr("SD OVERVIEW");
      line(30, g_sdOk ? "SD mounted" : "SD NOT FOUND", g_sdOk ? C_OK : C_BAD);
      if (g_sdOk) {
        const double tot = SD.totalBytes() / 1073741824.0, used = SD.usedBytes() / 1073741824.0;
        line(46, "size " + String(tot, 1) + " GB  used " + String(used, 1), C_TXT);
        progressBar(4, 60, 180, (int)(used * 100 / max(0.1, tot)), C_WARN);
        line(76, "files known: " + String((unsigned)g_files.size()), C_DIM);
        line(90, "family dirs stay separate:", C_DIM);
        line(102, "/AEGIS /AEGIS_RT /AEGIS_*", C_DIM);
        line(112, "home WiFi profile: " + String(g_homeSsid.length() ? g_homeSsid : "not set"), C_DIM);
      }
      foot("ESC menu");
      break;
    }
    case SCR_FILES: {
      hdr("FILE BROWSER (.bin = ENTER hashes)");
      if (g_files.empty()) line(36, "no files found", C_WARN);
      const int rows = 7;
      int first = g_fileSel - rows / 2; if (first < 0) first = 0;
      if (first + rows > (int)g_files.size()) first = max(0, (int)g_files.size() - rows);
      int y = 26;
      for (int i = first; i < (int)g_files.size() && i < first + rows; i++) {
        const bool sel = i == g_fileSel;
        if (sel) cv.fillRect(0, y - 1, 240, 11, C_SEL);
        cv.setTextSize(1); cv.setTextColor(isBin(g_files[i].path) ? C_WARN : C_TXT, sel ? C_SEL : C_BG);
        cv.setCursor(4, y); cv.print(fit(g_files[i].path, 30));
        cv.setTextColor(C_DIM, sel ? C_SEL : C_BG);
        cv.setCursor(196, y); cv.print(String(g_files[i].size / 1024) + "K");
        y += 11;
      }
      foot("UP/DN browse - ENTER hash .bin - ESC");
      break;
    }
    case SCR_HASH: {
      hdr("SHA-256 VERIFY");
      line(30, fit(g_hashPath, 39), C_DIM);
      if (g_hashBusy) { progressBar(4, 48, 180, g_hashPct, C_OK); line(66, "hashing...", C_ACC); }
      else if (g_hashHex.length()) {
        line(48, g_hashHex.substring(0, 32), C_OK);
        line(60, g_hashHex.substring(32), C_OK);
        line(78, "compare vs SHA256SUMS.txt", C_DIM);
        line(90, "in the program ZIP (or serial)", C_DIM);
      } else line(48, g_status, C_WARN);
      foot("ESC menu");
      break;
    }
    case SCR_BACKUP: {
      hdr("BACKUP FAMILY CONFIGS");
      line(34, g_status, C_ACC);
      line(52, "copies configs + notes + index", C_TXT);
      line(66, "NEVER gate/shadow/scope secrets", C_WARN);
      line(82, "to /AEGIS_KEEPER/backup-*/", C_DIM);
      foot("ENTER run backup - ESC menu");
      break;
    }
    case SCR_CLEAN: {
      hdr("CLEAN SCRATCH FILES");
      line(34, "deletes ONLY known scratch:", C_TXT);
      line(50, "/AEGIS_BENCH/sdtest.bin", C_DIM);
      line(64, "evidence + logs are untouched", C_OK);
      line(80, g_cleanDone ? "scratch cleaned" : "ENTER cleans scratch", g_cleanDone ? C_OK : C_WARN);
      foot("ENTER clean - ESC menu");
      break;
    }
    case SCR_ABOUT: {
      hdr("ABOUT AEGIS KEEPER");
      line(30, "The family caretaker.", C_ACC);
      line(44, "Hashes install files, backs up", C_DIM);
      line(56, "configs, and keeps the SD tidy -", C_DIM);
      line(68, "without ever touching evidence.", C_DIM);
      line(84, "No keybinds: arrows + ENTER.", C_TXT);
      foot("ESC menu");
      break;
    }
  }
  cv.pushSprite(0, 0);
}

static void onKey(const String& word, bool enter, bool del, bool tab, bool fn) {
  (void)del; (void)tab;
  if (g_screen == SCR_SPLASH) { g_screen = SCR_MENU; draw(); return; }
  const bool esc = word.indexOf('`') >= 0 || (fn && word.indexOf('~') >= 0);
  if (esc) { if (g_hashBusy) { g_hashFile.close(); mbedtls_sha256_free(&g_sha); g_hashBusy = false; } if (g_screen != SCR_MENU) g_screen = SCR_MENU; draw(); return; }
  char c = word.length() ? word[0] : 0;
  const bool up = (c == ';' || c == ':'), down = (c == '.' || c == '>');
  switch (g_screen) {
    case SCR_MENU:
      if (up) g_menuSel = (g_menuSel + kMenuCount - 1) % kMenuCount;
      else if (down) g_menuSel = (g_menuSel + 1) % kMenuCount;
      else if (enter) {
        g_screen = kMenu[g_menuSel].target;
        if (g_screen == SCR_FILES) scanFiles();
        if (g_screen == SCR_HASH) g_screen = SCR_FILES;
      }
      break;
    case SCR_FILES:
      if (up && g_fileSel > 0) g_fileSel--;
      else if (down && g_fileSel + 1 < (int)g_files.size()) g_fileSel++;
      else if (enter && !g_files.empty() && isBin(g_files[g_fileSel].path)) {
        hashStart(g_files[g_fileSel].path); g_screen = SCR_HASH;
      } else if (enter) g_status = "only .bin files hash here";
      break;
    case SCR_BACKUP:
      if (enter) { g_status = doBackup(); }
      break;
    case SCR_CLEAN:
      if (enter) { SD.remove("/AEGIS_BENCH/sdtest.bin"); g_cleanDone = true; }
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
  if (g_sdOk) scanFiles();
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
  if (g_hashBusy) { hashStep(); if (g_screen == SCR_HASH) draw(); }
  delay(5);
}
