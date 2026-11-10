// =====================================================================
// AEGIS BENCH v0.1.0 - hardware lab + diagnostics for M5Stack
// Cardputer ADV. Family utility: when a hat, cable, SD card or radio
// acts strange, BENCH is the known-good tester.
//
// Tools: keyboard test, display test, SD speed test, battery logger,
// I2C scanner, Grove UART console (G1/G2), GNSS NMEA viewer, Wi-Fi
// scan/info. Input = family standard: one grouped menu, arrows +
// Enter, Esc back. No letter keybinds (typing only in the UART
// console, where every key is data).
// =====================================================================
#include <M5Cardputer.h>
#include <SD.h>
#include <SPI.h>
#include <Wire.h>
#include <WiFi.h>
#include <Preferences.h>
#include <vector>

static const char* kVer = "0.1.2";
static const char* kFw = "AEGIS BENCH 0.1.2";
static M5Canvas cv(&M5Cardputer.Display);
static const uint16_t C_BG = 0x0000, C_BAR = 0x0210, C_ACC = 0x07FF,
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
  "................",
  "..CCC...........",
  ".C...C..........",
  ".C..............",
  ".C...C..........",
  "..CCC...........",
  "....CC..........",
  ".....CC.........",
  "......CC........",
  ".......CC.......",
  "........CC......",
  ".........CC.....",
  "..........CCCC..",
  "...........CCC..",
  "............C...",
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

enum Screen { SCR_SPLASH, SCR_MENU, SCR_KEYS, SCR_DISPLAY, SCR_SDTEST, SCR_BATT,
              SCR_I2C, SCR_UART, SCR_GNSS, SCR_WIFI, SCR_ABOUT };
static Screen g_screen = SCR_SPLASH;
static uint32_t g_bootMs = 0;

struct MenuItem { const char* group; const char* label; Screen target; };
static const MenuItem kMenu[] = {
  { "TESTS",  "Keyboard test",     SCR_KEYS },
  { "TESTS",  "Display test",      SCR_DISPLAY },
  { "TESTS",  "SD speed test",     SCR_SDTEST },
  { "TESTS",  "Battery + logger",  SCR_BATT },
  { "BUSES",  "I2C scanner",       SCR_I2C },
  { "BUSES",  "Grove UART console",SCR_UART },
  { "RADIOS", "GNSS NMEA viewer",  SCR_GNSS },
  { "RADIOS", "Wi-Fi scan + info", SCR_WIFI },
  { "SYSTEM", "About BENCH",       SCR_ABOUT },
};
static const int kMenuCount = sizeof(kMenu) / sizeof(kMenu[0]);
static int g_menuSel = 0;

// tool state
static String g_lastKey = "-";
static int g_dispMode = 0;
static String g_sdResult = "not run";
static bool g_sdBusy = false; static int g_sdPct = 0;
static std::vector<int> g_i2cFound; static bool g_i2cDone = false; static int g_i2cPct = 0;
static String g_uartLines[6]; static int g_uartHead = 0;
static bool g_uartOpen = false;
static String g_nmeaLast = "-"; static int g_nmeaCount = 0, g_ggaFix = 0, g_ggaSats = 0;
static String g_lat = "-", g_lon = "-";
static bool g_wifiBusy = false; static uint32_t g_wifiStart = 0; static int g_wifiCount = -1;
static String g_wifiTop = "-";

static void uartPush(const String& s) {
  g_uartLines[g_uartHead] = fit(s, 39); g_uartHead = (g_uartHead + 1) % 6;
}
static void runSdTestStep() {
  // One-shot chunked test driven from loop() so the UI stays alive.
  static int phase = 0; static uint32_t t0 = 0; static File f;
  if (!g_sdBusy) { phase = 0; t0 = millis(); g_sdBusy = true; }
  if (!g_sdOk) { g_sdResult = "no SD"; g_sdBusy = false; return; }
  if (phase == 0) {
    SD.remove("/AEGIS_BENCH/sdtest.bin");
    if (!SD.exists("/AEGIS_BENCH")) SD.mkdir("/AEGIS_BENCH");
    f = SD.open("/AEGIS_BENCH/sdtest.bin", FILE_WRITE);
    if (!f) { g_sdResult = "open failed"; g_sdBusy = false; return; }
    phase = 1;
  }
  if (phase == 1) {
    uint8_t buf[512]; memset(buf, 0xA5, sizeof(buf));
    for (int i = 0; i < 8; i++) f.write(buf, sizeof(buf));   // 4 KB per step
    g_sdPct = min(50, g_sdPct + 2);
    if (g_sdPct >= 50) { f.close(); phase = 2; }
    return;
  }
  if (phase == 2) {
    const uint32_t wms = millis() - t0;
    f = SD.open("/AEGIS_BENCH/sdtest.bin", FILE_READ);
    uint8_t buf[512]; uint32_t t1 = millis();
    while (f.available()) { f.read(buf, sizeof(buf)); g_sdPct = min(100, g_sdPct + 2); }
    f.close(); SD.remove("/AEGIS_BENCH/sdtest.bin");
    const uint32_t rms = max<uint32_t>(1, millis() - t1);
    g_sdResult = "W " + String(128.0 / max<uint32_t>(1, wms) * 1000.0, 1) + " KB/s  R " +
                 String(128.0 / rms * 1000.0, 1) + " KB/s";
    g_sdBusy = false; g_sdPct = 0; phase = 0;
  }
}
static void runI2cScan() {
  g_i2cFound.clear();
  Wire.begin(8, 9);
  for (int a = 8; a < 120; a++) {
    Wire.beginTransmission(a);
    if (Wire.endTransmission() == 0) g_i2cFound.push_back(a);
    g_i2cPct = (a - 8) * 100 / 112;
    if ((a % 16) == 0) {   // keep the bar honest mid-scan
      hdr("I2C SCANNER"); line(34, "scanning bus G8/G9...", C_ACC);
      progressBar(4, 48, 180, g_i2cPct, C_OK); cv.pushSprite(0, 0);
    }
  }
  g_i2cDone = true; g_i2cPct = 100;
}

static void draw() {
  switch (g_screen) {
    case SCR_SPLASH: {
      cv.fillScreen(C_BG);
      drawMark(120, 42, 4);
      cv.setTextSize(2); cv.setTextColor(C_TXT, C_BG); cv.setCursor(52, 82); cv.print("AEGIS");
      cv.setTextColor(C_ACC, C_BG); cv.setCursor(124, 82); cv.print("BENCH");
      line(108, "hardware lab - known-good tester", C_DIM);
      line(121, "keyboard - buses - radios - storage", C_DIM);
      break;
    }
    case SCR_MENU: {
      cv.fillScreen(C_BG);
      cv.fillRect(0, 0, 240, 24, C_BAR);
      cv.setTextSize(2); cv.setTextColor(C_TXT, C_BAR); cv.setCursor(8, 5); cv.print("BENCH");
      drawBatteryIcon(cv, 218, 8, C_BAR);
      line(30, "one menu - grouped: tests/buses/radios", C_DIM);
      int y = 44;
      for (int i = 0; i < kMenuCount; i++) {
        const bool sel = i == g_menuSel;
        if (sel) cv.fillRect(0, y - 2, 240, 11, C_SEL);
        cv.setTextSize(1);
        cv.setTextColor(kMenu[i].group[0] == 'T' ? C_ACC : kMenu[i].group[0] == 'B' ? C_WARN : 0x87FF,
                        sel ? C_SEL : C_BG);
        cv.setCursor(6, y); cv.print(kMenu[i].group);
        cv.setTextColor(sel ? C_WARN : C_TXT, sel ? C_SEL : C_BG);
        cv.setCursor(66, y); cv.print(fit(String(kMenu[i].label), 26));
        if (sel) { cv.setCursor(228, y); cv.print(">"); }
        y += 11;
      }
      foot(String("v") + kVer + " - arrows + ENTER - ESC");
      break;
    }
    case SCR_KEYS: {
      hdr("KEYBOARD TEST");
      line(36, "last key event:", C_DIM);
      cv.setTextSize(2); cv.setTextColor(C_ACC, C_BG); cv.setCursor(4, 50); cv.print(fit(g_lastKey, 18));
      line(84, "arrows should read UP/DN/L/R", C_TXT);
      line(98, "Fn+` should read ESC (exits)", C_DIM);
      foot("ESC back to menu");
      break;
    }
    case SCR_DISPLAY: {
      if (g_dispMode == 0) {
        const uint16_t cols[6] = {0xF800, 0x07E0, 0x001F, 0xFFE0, 0x07FF, 0xF81F};
        for (int i = 0; i < 6; i++) cv.fillRect(i * 40, 0, 40, 90, cols[i]);
        cv.fillRect(0, 90, 240, 45, 0x0000);
        line(100, "color bars - no dead stripes?", C_TXT);
      } else if (g_dispMode == 1) { cv.fillScreen(0xFFFF); line(60, "full white - check uniformity", 0x0000); }
      else { cv.fillScreen(0x0000); line(60, "full black - check backlight bleed", C_DIM); }
      foot("LEFT/RIGHT pattern - ESC menu");
      break;
    }
    case SCR_SDTEST: {
      hdr("SD SPEED TEST");
      line(36, g_sdOk ? "SD mounted" : "SD NOT FOUND", g_sdOk ? C_OK : C_BAD);
      if (g_sdBusy) { progressBar(4, 52, 180, g_sdPct, C_OK); line(70, "testing 128 KB write + read...", C_ACC); }
      else line(52, g_sdResult, C_TXT);
      line(88, "creates then deletes sdtest.bin", C_DIM);
      foot("ENTER run - ESC menu");
      break;
    }
    case SCR_BATT: {
      hdr("BATTERY + LOGGER");
      g_batt.update();
      line(34, String(g_batt.mv) + " mV   " + g_batt.pct + "%" + (g_batt.charging ? "  CHARGING" : ""), C_TXT);
      progressBar(4, 48, 180, g_batt.pct, g_batt.charging ? C_ACC : C_OK);
      line(66, "voltage curve (family standard)", C_DIM);
      line(80, "ENTER appends a row to", C_TXT);
      line(92, "/AEGIS_BENCH/battlog.csv", C_DIM);
      foot("ENTER log row - ESC menu");
      break;
    }
    case SCR_I2C: {
      hdr("I2C SCANNER");
      if (!g_i2cDone) line(36, "ENTER scans G8/G9 0x08-0x77", C_WARN);
      else {
        line(32, String((unsigned)g_i2cFound.size()) + " device(s) ACKed", C_OK);
        String s; for (size_t i = 0; i < g_i2cFound.size() && i < 14; i++) { s += "0x"; s += String(g_i2cFound[i], HEX); s += " "; }
        line(48, s, C_TXT);
        line(64, "0x43/0x20-ish = Cap expander", C_DIM);
        line(76, "nothing found = bus/hat issue", C_DIM);
      }
      foot("ENTER scan - ESC menu");
      break;
    }
    case SCR_UART: {
      hdr("GROVE UART CONSOLE 115200");
      int y = 26;
      for (int i = 0; i < 6; i++) {
        const String& s = g_uartLines[(g_uartHead + i) % 6];
        if (s.length()) { line(y, s, C_TXT); y += 11; }
      }
      line(100, g_uartOpen ? "G1 RX / G2 TX - typing sends" : "ENTER opens the port", g_uartOpen ? C_OK : C_WARN);
      foot("ESC closes + menu");
      break;
    }
    case SCR_GNSS: {
      hdr("GNSS NMEA VIEWER");
      line(32, "sentences: " + String(g_nmeaCount), C_TXT);
      line(46, "fix " + String(g_ggaFix) + "  sats " + String(g_ggaSats), g_ggaFix ? C_OK : C_WARN);
      line(60, "lat " + g_lat, C_TXT);
      line(72, "lon " + g_lon, C_TXT);
      line(90, fit(g_nmeaLast, 39), C_DIM);
      line(104, "Cap GNSS RX G15 / TX G13 115200", C_DIM);
      foot("ESC menu");
      break;
    }
    case SCR_WIFI: {
      hdr("WI-FI SCAN + INFO");
      if (g_wifiBusy) {
        const int pct = min(99, (int)((millis() - g_wifiStart) * 100 / 5000));
        line(36, "scanning (5 s window)...", C_ACC);
        progressBar(4, 50, 180, pct, C_OK);
      } else if (g_wifiCount >= 0) {
        line(36, String(g_wifiCount) + " APs seen", C_OK);
        line(52, "strongest: " + g_wifiTop, C_TXT);
        line(68, "MAC " + WiFi.macAddress(), C_DIM);
      } else line(36, "ENTER starts a scan", C_WARN);
      line(84, "home: " + String(g_homeSsid.length() ? g_homeSsid : "not set") + (WiFi.status() == WL_CONNECTED ? " " + WiFi.localIP().toString() : ""), C_DIM);
      foot("ENTER scan - ESC menu (stops)");
      break;
    }
    case SCR_ABOUT: {
      hdr("ABOUT AEGIS BENCH");
      line(30, "The family workshop.", C_ACC);
      line(44, "If BENCH says a bus, card or", C_DIM);
      line(56, "radio works, the hardware is", C_DIM);
      line(68, "fine and the fault is elsewhere.", C_DIM);
      line(84, "No keybinds: arrows + ENTER.", C_TXT);
      foot("ESC menu");
      break;
    }
  }
  cv.pushSprite(0, 0);
}

static void stopTools() {
  if (g_wifiBusy) { WiFi.scanDelete(); g_wifiBusy = false; }
  if (g_uartOpen) { Serial1.end(); g_uartOpen = false; }
  g_sdBusy = false;
}

static void onKey(const String& word, bool enter, bool del, bool tab, bool fn) {
  if (g_screen == SCR_SPLASH) { g_screen = SCR_MENU; draw(); return; }
  const bool esc = word.indexOf('`') >= 0 || (fn && word.indexOf('~') >= 0);
  // UART console: every key is data, Esc alone leaves.
  if (g_screen == SCR_UART && g_uartOpen && !esc) {
    if (enter) { Serial1.print("\r\n"); uartPush("> (enter)"); }
    else if (del) Serial1.write(0x7F);
    else if (word.length()) { Serial1.print(word); uartPush("> " + word); }
    draw(); return;
  }
  if (esc) { stopTools(); if (g_screen != SCR_MENU) g_screen = SCR_MENU; draw(); return; }
  char c = word.length() ? word[0] : 0;
  const bool up = (c == ';' || c == ':'), down = (c == '.' || c == '>'),
             left = (c == ',' || c == '<'), right = (c == '/' || c == '?');
  if (g_screen == SCR_KEYS) {
    g_lastKey = up ? "UP" : down ? "DOWN" : left ? "LEFT" : right ? "RIGHT" :
                enter ? "ENTER" : del ? "BACKSPACE" : tab ? "TAB" : (word.length() ? word : g_lastKey);
    draw(); return;
  }
  switch (g_screen) {
    case SCR_MENU:
      if (up) g_menuSel = (g_menuSel + kMenuCount - 1) % kMenuCount;
      else if (down) g_menuSel = (g_menuSel + 1) % kMenuCount;
      else if (enter) {
        g_screen = kMenu[g_menuSel].target;
        if (g_screen == SCR_UART && !g_uartOpen) { /* opened on ENTER inside */ }
      }
      break;
    case SCR_DISPLAY:
      if (left) g_dispMode = (g_dispMode + 2) % 3;
      if (right) g_dispMode = (g_dispMode + 1) % 3;
      break;
    case SCR_SDTEST:
      if (enter && !g_sdBusy) { g_sdPct = 0; runSdTestStep(); }
      break;
    case SCR_BATT:
      if (enter && g_sdOk) {
        if (!SD.exists("/AEGIS_BENCH")) SD.mkdir("/AEGIS_BENCH");
        const bool fresh = !SD.exists("/AEGIS_BENCH/battlog.csv");
        File f = SD.open("/AEGIS_BENCH/battlog.csv", FILE_APPEND);
        if (f) {
          if (fresh) f.println("uptime_s,mv,pct,charging");
          f.printf("%lu,%d,%d,%d\n", millis() / 1000, g_batt.mv, g_batt.pct, g_batt.charging ? 1 : 0);
          f.close(); g_status = "logged";
        }
      }
      break;
    case SCR_I2C:
      if (enter) { runI2cScan(); }
      break;
    case SCR_UART:
      if (enter && !g_uartOpen) { Serial1.begin(115200, SERIAL_8N1, 1, 2); g_uartOpen = true; uartPush("(port open 115200 G1/G2)"); }
      break;
    case SCR_WIFI:
      if (enter && !g_wifiBusy) {
        WiFi.mode(WIFI_STA); WiFi.disconnect(true);
        WiFi.scanNetworks(true, false); g_wifiBusy = true; g_wifiStart = millis();
      }
      break;
    default: break;
  }
  draw();
}

static String g_serLine;
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
  Serial2.begin(115200, SERIAL_8N1, 15, 13);   // GNSS cap, receive only
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
  homeJoinTick(g_wifiBusy);
  if (g_sdBusy && g_screen == SCR_SDTEST) { runSdTestStep(); draw(); }
  if (g_wifiBusy) {
    const int n = WiFi.scanComplete();
    if (n >= 0) {
      g_wifiBusy = false; g_wifiCount = n; g_wifiTop = "-";
      int best = -999;
      for (int i = 0; i < n; i++) if (WiFi.RSSI(i) > best) { best = WiFi.RSSI(i); g_wifiTop = fit(WiFi.SSID(i), 20) + " " + best + "dBm"; }
      WiFi.scanDelete(); g_netNext = millis() + 2000;
      if (g_screen == SCR_WIFI) draw();
    } else if (millis() - g_wifiStart > 500 && g_screen == SCR_WIFI) draw();
  }
  if (g_uartOpen) {
    static String lb;
    while (Serial1.available()) {
      char ch = (char)Serial1.read();
      if (ch == '\n') { uartPush(lb); lb = ""; if (g_screen == SCR_UART) draw(); }
      else if (ch != '\r' && lb.length() < 39) lb += ch;
    }
  }
  while (Serial2.available()) {   // GNSS NMEA (viewer screen only needs it, keep always parsed)
    String s = Serial2.readStringUntil('\n'); s.trim();
    if (!s.length()) continue;
    g_nmeaCount++; g_nmeaLast = s;
    if (s.startsWith("$GPGGA") || s.startsWith("$GNGGA")) {
      // $GPGGA,time,lat,N,lon,E,fix,sats,...
      int p1 = s.indexOf(','); int idx = 0; String f[8];
      String rest = s;
      while (idx < 8 && rest.indexOf(',') >= 0) { int p = rest.indexOf(','); f[idx++] = rest.substring(0, p); rest = rest.substring(p + 1); }
      if (idx >= 7) { g_lat = f[2] + f[3]; g_lon = f[4] + f[5]; g_ggaFix = f[6].toInt(); g_ggaSats = f[7].toInt(); }
    }
    if (g_screen == SCR_GNSS && (g_nmeaCount % 5) == 0) draw();
  }
  while (Serial.available()) {
    char ch = (char)Serial.read();
    if (ch == '\n') {
      g_serLine.trim();
      if (g_serLine == "STATUS")
        Serial.printf("+BENCH: {\"sd\":%s,\"batt_pct\":%d,\"mv\":%d,\"ver\":\"%s\"}\n",
                      g_sdOk ? "true" : "false", g_batt.pct, g_batt.mv, kVer);
      else if (g_serLine == "I2C") { runI2cScan(); Serial.printf("+BENCH: i2c_found=%u\n", (unsigned)g_i2cFound.size()); }
      else if (g_serLine.length()) Serial.println("+BENCH: commands STATUS I2C");
      g_serLine = "";
    } else if (ch != '\r' && g_serLine.length() < 64) g_serLine += ch;
  }
  delay(5);
}
