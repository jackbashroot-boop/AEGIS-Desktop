// AEGIS PHOENIX - on-device 240x135 UI v2 (Cardputer ADV ST7789V2)
// Flicker-free: everything renders into an off-screen sprite, then one
// blit. Splash -> dashboard after 5 s (any stable key skips).
// Views: 1 DASH | 2 MODULES | 3 EVENTS | 4 CAP | 5 NET | 6 DIAG | 7 HELP | 8 SETUP | 9 REMOTE
// Every view has a bottom menu: Up/Down (Fn+; / Fn+.) selects, Enter runs,
// Left/Right (Fn+, / Fn+/) changes view. The plain ; , . / keys also work
// because Aegis has no text-entry fields.
#pragma once
#include <Arduino.h>
#include <M5Cardputer.h>

// AEGIS PHOENIX mark: front phoenix silhouette (raised wing arcs,
// turned head with beak, diamond tail) - pixel interpretation of a
// tribal phoenix composition, hand-cleaned at 16x16. C=cyan, W=face.
static const char* kPhoenix16[16] = {
  "................",
  ".C............C.",
  "CCC....CC....CCC",
  "CCCC...CCWC.CCCC",
  "CC.CC..CC..CC.CC",
  "CC...C....C...CC",
  "CC.....CC.....CC",
  ".CC....CC....CC.",
  ".CC..CC..CC..CC.",
  "..CC..C..C..CC..",
  "...CCCC..CCCC...",
  "......C..C......",
  ".....CC..CC.....",
  ".....CC..CC.....",
  ".......CC.......",
  "................",
};

static LGFX_Sprite g_canvas(&M5Cardputer.Display);
static bool g_canvasReady = false;
static bool uiEnsureCanvas() {
  if (g_canvasReady) return true;
  g_canvas.setColorDepth(16);
  g_canvasReady = g_canvas.createSprite(240, 135) != nullptr;
  return g_canvasReady;
}

static void drawPhoenix(LGFX_Sprite& d, int x, int y, int scale) {
  for (int row = 0; row < 16; row++) {
    for (int col = 0; col < 16; col++) {
      char px = kPhoenix16[row][col];
      if (px == '.') continue;
      d.fillRect(x + col * scale, y + row * scale, scale, scale,
                 px == 'C' ? 0x07FF : px == 'S' ? 0xC618 : 0xFFFF);
    }
  }
}

enum AegisView { VIEW_DASH = 0, VIEW_MODULES, VIEW_EVENTS, VIEW_CAP, VIEW_NET, VIEW_DIAG, VIEW_HELP, VIEW_SETUP, VIEW_REMOTE, VIEW_LOGS, VIEW_ATLAS, VIEW_RULES, VIEW_MENU, VIEW_BINDS };
static AegisView g_view = VIEW_DASH;
static uint32_t g_lastUiDraw = 0;
static uint32_t g_ghostIgnored = 0;
static uint32_t g_splashUntil = 0;

// ---- Per-view menu model -------------------------------------------------
// action: a normal Aegis key ('s','w','n','r','t','q'), 0 for view-only,
// 3 = page NET results, 4 = manual keyboard resync. target: view to show
// before/with the action, or -1 to remain on the current view.
struct AegisMenuItem { const char* label; char action; int target; };
static uint8_t g_menuSel[12] = {0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0};
static int g_menuListSel = 0;   // unified MENU selection (item index)
// Custom key binds (v0.9.0): user-assigned key -> unified menu item.
// No binds exist by default; letters do nothing until bound here.
static char g_bindKey[24] = {0};
static uint8_t g_bindItem[24] = {0};
static int g_bindCount = 0;
static int g_bindSel = 0;
static bool g_bindCapture = false;
static int g_remoteAction = 0;  // 0 auto-find, 1 learn (REMOTE action row)
static int g_menuListTop = 0;   // unified MENU window (display rows)

// ONE menu for the whole program (v0.8.1, user request): opened with M
// from anywhere, similar items grouped together (all scans adjacent),
// selection stays centred. act: 0 = go to target view; otherwise a
// dispatch code handled by activateUnifiedItem() in main.cpp.
struct UnifiedItem { const char* group; const char* label; int act; int target; };
static const UnifiedItem kUnifiedMenu[] = {
  {"SCANS",   "Scan WiFi + BLE now",      's', -1},
  {"SCANS",   "LAN scan (live hosts)",    'n', -1},
  {"SCANS",   "LoRa channel sweep",       'r', -1},
  {"SCANS",   "Sniffer channel hop 1-13", 'H', -1},
  {"SCANS",   "Wardrive logging",         'w', -1},
  {"SCANS",   "Background scans",         'B', -1},
  {"SCANS",   "Passive offline mode",     'O', -1},
  {"DETECT",  "Events",                   0, VIEW_EVENTS},
  {"DETECT",  "Atlas / WiGLE map",        0, VIEW_ATLAS},
  {"DETECT",  "Export maps (Atlas)",      'M', -1},
  {"DETECT",  "Rules manager",            0, VIEW_RULES},
  {"DETECT",  "Reload rule packs",        'Y', -1},
  {"DETECT",  "Quarantine (mute radios)", 'Q', -1},
  {"DEVICES", "Devices & hats",           0, VIEW_MODULES},
  {"DEVICES", "Shadow vault (AES-256)",   'U', -1},
  {"DEVICES", "TV remote",                0, VIEW_REMOTE},
  {"DEVICES", "RFID2 reader",             'F', -1},
  {"DEVICES", "ESP32-C5 hot-swap",        'C', -1},
  {"SYSTEM",  "Dashboard",                0, VIEW_DASH},
  {"SYSTEM",  "Setup",                    0, VIEW_SETUP},
  {"SYSTEM",  "Full integrity sweep",     't', -1},
  {"SYSTEM",  "Brightness",               'G', -1},
  {"SYSTEM",  "Screen sleep",             'Z', -1},
  {"SYSTEM",  "Desktop live link",        'L', -1},
  {"SYSTEM",  "Desktop auto-sync",        'D', -1},
  {"SYSTEM",  "Log viewer",               0, VIEW_LOGS},
  {"SYSTEM",  "Help",                     0, VIEW_HELP},
  {"SYSTEM",  "Key binds (custom)",       0, VIEW_BINDS},
  {"SETUP",   "Home WiFi setup",          'H', -1},
};
static uint8_t g_netPage = 0;
static int g_eventSel = 0;              // 0 = newest event
static int g_eventTop = 0;              // first visible row (follows selection)
static bool g_eventExpanded = false;
static int g_eventDetailScroll = 0;
static int g_remoteSel = 0;
static int g_remoteTop = 0;   // TV-remote list window (follows selection)
static int g_eventFilter = 0;   // 0 all, 1 wifi, 2 ble, 3 lora/gnss, 4 unreviewed
static const char* kEventFilterNames[5] = {"ALL", "WIFI", "BLE", "LORA/GNSS", "UNREVIEWED"};
static int g_logFileSel = 0;
static int g_logScroll = 0;
static const char* kLogFiles[4] = {"aegis_boot.log", "integrity.log", "net_scan.log", "esp32c5_5ghz.log"};
static int g_atlasSel = 0;
static int g_atlasTop = 0;
static int g_ruleSel = 0;
static int g_ruleTop = 0;
static bool g_atlasExpanded = false;

static bool eventMatchesFilter(const AegisEventRecord& e) {
  switch (g_eventFilter) {
    case 1: return e.channel.indexOf("wifi") >= 0;
    case 2: return e.channel.indexOf("ble") >= 0;
    case 3: return e.channel.indexOf("gnss") >= 0 || e.channel.indexOf("lora") >= 0 ||
                   e.kind.indexOf("gnss") >= 0 || e.kind.indexOf("lora") >= 0;
    case 4: return !e.reviewed;
    default: return true;
  }
}
// Record indices into g_eventHistory, newest first, honoring the filter.

// Center-locked list window: the list scrolls so the SELECTED row stays
// in the middle of the window (v0.7.9, user request) - you always read
// the selection with context above and below. Near the ends of the list
// the window clamps, so the selection rests at the top/bottom edge only
// when there is nothing more to show on that side.
static inline int followWindow(int sel, int count, int visible, int& top) {
  if (count <= visible) { top = 0; return 0; }
  if (sel < 0) sel = 0;
  if (sel > count - 1) sel = count - 1;
  top = sel - visible / 2;
  const int maxTop = count - visible;
  if (top > maxTop) top = maxTop;
  if (top < 0) top = 0;
  return top;
}
// STANDARD SCROLL BAR (v0.9.7, user request): every scrollable list
// that overflows its viewport gets a real scroll bar at the right
// edge - track + thumb sized/positioned by the visible fraction -
// on top of the follow-the-cursor window. If a list fits, no bar.
static inline void drawScrollBar(int first, int visible, int count, int yTop, int yBot) {
  if (count <= visible || visible <= 0) return;
  auto& d = g_canvas;
  const int h = yBot - yTop;
  d.drawFastVLine(238, yTop, h, 0x4208);
  int th = h * visible / count; if (th < 6) th = 6; if (th > h) th = h;
  const int span = count - visible;
  int ty = yTop + (span > 0 ? (h - th) * first / span : 0);
  d.fillRect(237, ty, 3, th, 0x07FF);
}
// Small ^ / v hints when a list continues above/below the window.
static inline void scrollHints(int first, int shownEnd, int count, int yTop, int yBot) {
  auto& d = g_canvas;
  d.setTextSize(1); d.setTextColor(0xFFE0, 0x0000);
  if (first > 0) { d.setCursor(231, yTop); d.print("^"); }
  if (shownEnd < count) { d.setCursor(231, yBot); d.print("v"); }
}

static std::vector<int> visibleEventIndices() {
  std::vector<int> out;
  for (int i = (int)g_eventHistory.size() - 1; i >= 0; i--) {
    if (eventMatchesFilter(g_eventHistory[i])) out.push_back(i);
  }
  return out;
}
static String eventClock(const AegisEventRecord& e) {
  if (e.epoch) {
    time_t t = (time_t)e.epoch;
    struct tm tmv;
    gmtime_r(&t, &tmv);
    char buf[20];
    snprintf(buf, sizeof(buf), "%02d:%02d:%02d UTC", tmv.tm_hour, tmv.tm_min, tmv.tm_sec);
    return String(buf);
  }
  if (e.previousBoot) return "previous boot";
  const uint32_t sec = e.atMs / 1000;
  char buf[20];
  snprintf(buf, sizeof(buf), "boot +%02lu:%02lu", (unsigned long)(sec / 60), (unsigned long)(sec % 60));
  return String(buf);
}
static std::vector<String> readLogTail(const char* filename, size_t maxBytes = 5200) {
  std::vector<String> lines;
  String path = String("/AEGIS/logs/") + filename;
  File f = SD.open(path.c_str(), FILE_READ);
  if (!f) { lines.push_back("(no such log yet)"); return lines; }
  if (f.size() > maxBytes) f.seek(f.size() - maxBytes);
  String all = f.readString();
  f.close();
  int start = 0;
  while (start < (int)all.length()) {
    int nl = all.indexOf('\n', start);
    if (nl < 0) nl = all.length();
    String line = all.substring(start, nl);
    line.trim();
    if (line.length()) lines.push_back(line);
    start = nl + 1;
  }
  if (lines.empty()) lines.push_back("(log is empty)");
  return lines;
}
static const AegisMenuItem kAegisMenus[12][8] = {
  { {"Scan WiFi + BLE", 's', -1}, {"Wardrive", 'w', -1}, {"LAN scan", 'n', VIEW_NET}, {"LoRa sweep", 'r', VIEW_CAP}, {"Integrity sweep", 't', VIEW_DIAG}, {"Quarantine", 'q', -1}, {"Setup", 0, VIEW_SETUP}, {"TV remote", 0, VIEW_REMOTE} },
  { {"Run AP/BLE scan", 's', VIEW_DASH}, {"Event details", 0, VIEW_EVENTS}, {"Rule manager", 0, VIEW_RULES}, {"SD event archive", 0, VIEW_EVENTS}, {"Cap radios", 0, VIEW_CAP}, {"Network scan", 0, VIEW_NET} },
  { {"Shadow vault", '\x14', -1}, {"Re-run census", '\x15', -1}, {"RFID2 toggle", '\x16', -1}, {"Devices & hats", 0, VIEW_MODULES}, {"Dashboard", 0, VIEW_DASH} },
  { {"LoRa sweep", 'r', VIEW_CAP}, {"WiFi + BLE scan", 's', VIEW_DASH}, {"Devices & hats", 0, VIEW_MODULES}, {"Diagnostics", 0, VIEW_DIAG}, {"Dashboard", 0, VIEW_DASH}, {"LoRa sweep", 'r', VIEW_CAP} },
  { {"LAN scan", 'n', VIEW_NET}, {"Next results page", '\x05', -1}, {"WiFi + BLE scan", 's', VIEW_DASH}, {"Wardrive", 'w', VIEW_DASH}, {"Atlas explorer", 0, VIEW_ATLAS}, {"LAN scan", 'n', VIEW_NET} },
  { {"Full integrity", 't', VIEW_DIAG}, {"Resync keyboard", '\x06', VIEW_DIAG}, {"Event details", 0, VIEW_EVENTS}, {"Devices & hats", 0, VIEW_MODULES}, {"Dashboard", 0, VIEW_DASH}, {"Log viewer", 0, VIEW_LOGS} },
  { {"Scan WiFi + BLE", 's', VIEW_DASH}, {"Wardrive", 'w', VIEW_DASH}, {"LAN scan", 'n', VIEW_NET}, {"LoRa sweep", 'r', VIEW_CAP}, {"Integrity sweep", 't', VIEW_DIAG}, {"Quarantine", 'q', VIEW_DASH}, {"Setup", 0, VIEW_SETUP}, {"TV remote", 0, VIEW_REMOTE} },
  { {"Passive offline", '\x07', -1}, {"Background scans", '\x0B', -1}, {"Reload rule packs", '\x0F', -1}, {"Brightness", '\x11', -1}, {"Screen sleep", '\x12', -1}, {"C5 hot-swap", '\x0E', -1}, {"Alert beeps", '\x13', -1}, {"TV remote", 0, VIEW_REMOTE} },
  { {"TV button", 0, -1}, {"TV button", 0, -1}, {"TV button", 0, -1}, {"TV button", 0, -1}, {"TV button", 0, -1}, {"TV button", 0, -1}, {"TV button", 0, -1}, {"TV button", 0, -1} },
  { {"Logs", 0, -1}, {"Logs", 0, -1}, {"Logs", 0, -1}, {"Logs", 0, -1}, {"Logs", 0, -1}, {"Logs", 0, -1}, {"Logs", 0, -1}, {"Logs", 0, -1} },
  { {"Atlas", 0, -1}, {"Atlas", 0, -1}, {"Atlas", 0, -1}, {"Atlas", 0, -1}, {"Atlas", 0, -1}, {"Atlas", 0, -1}, {"Atlas", 0, -1}, {"Atlas", 0, -1} },
  { {"Toggle rule", 0, -1}, {"Toggle rule", 0, -1}, {"Toggle rule", 0, -1}, {"Toggle rule", 0, -1}, {"Toggle rule", 0, -1}, {"Toggle rule", 0, -1}, {"Toggle rule", 0, -1}, {"Toggle rule", 0, -1} },
};
static const uint8_t kAegisMenuCounts[12] = {8, 6, 5, 5, 5, 6, 8, 8, 1, 1, 1, 1};
static int aegisMenuCount(AegisView v) { return kAegisMenuCounts[(int)v]; }
static const AegisMenuItem& aegisMenuItem(AegisView v) {
  uint8_t& sel = g_menuSel[(int)v];
  if (sel >= kAegisMenuCounts[(int)v]) sel = 0;
  return kAegisMenus[(int)v][sel];
}

static String eventAgeString(uint32_t atMs) {
  uint32_t sec = (millis() - atMs) / 1000;
  if (sec < 60) return String(sec) + "s ago";
  if (sec < 3600) return String(sec / 60) + "m ago";
  return String(sec / 3600) + "h ago";
}

static void appendWrappedLines(std::vector<String>& lines, const String& text, size_t width = 38) {
  if (!text.length()) { lines.push_back("-"); return; }
  size_t pos = 0;
  while (pos < text.length()) {
    size_t remaining = text.length() - pos;
    size_t take = remaining > width ? width : remaining;
    if (remaining > width) {
      size_t breakAt = text.lastIndexOf(' ', pos + take);
      if (breakAt > pos + 12) take = breakAt - pos;
    }
    lines.push_back(text.substring(pos, pos + take));
    pos += take;
    while (pos < text.length() && text[pos] == ' ') pos++;
  }
}

static String fitLine(const String& s, size_t maxChars = 40) {
  if (s.length() <= (int)maxChars) return s;
  return s.substring(0, maxChars - 1) + "~";
}

// Detailed phoenix for the splash (vector shapes at ~96px tall):
// layered raised wings, head with beak, fanned tail flames.
static void drawPhoenixBig(LGFX_Sprite& d, int cx, int cy) {
  const uint16_t DEEP = 0x001F, CYAN = 0x07FF, SKY = 0x87FF, WHITE = 0xFFFF;
  // AEGIS family shield outline (same frame as the TALON splash).
  const uint16_t STEEL = 0xC618;
  d.drawLine(cx - 56, cy - 42, cx + 56, cy - 42, STEEL);
  d.drawLine(cx - 56, cy - 42, cx - 56, cy + 8, STEEL);
  d.drawLine(cx + 56, cy - 42, cx + 56, cy + 8, STEEL);
  d.drawLine(cx - 56, cy + 8, cx, cy + 50, STEEL);
  d.drawLine(cx + 56, cy + 8, cx, cy + 50, STEEL);
  d.drawLine(cx - 54, cy - 40, cx + 54, cy - 40, STEEL);
  d.drawLine(cx - 54, cy - 40, cx - 54, cy + 8, STEEL);
  d.drawLine(cx + 54, cy - 40, cx + 54, cy + 8, STEEL);
  d.drawLine(cx - 54, cy + 8, cx, cy + 47, STEEL);
  d.drawLine(cx + 54, cy + 8, cx, cy + 47, STEEL);
  // back wings (deep blue, widest)
  d.fillTriangle(cx, cy, cx - 52, cy - 34, cx - 40, cy + 6, DEEP);
  d.fillTriangle(cx, cy, cx + 52, cy - 34, cx + 40, cy + 6, DEEP);
  // mid wings (cyan)
  d.fillTriangle(cx, cy - 2, cx - 40, cy - 24, cx - 28, cy + 8, CYAN);
  d.fillTriangle(cx, cy - 2, cx + 40, cy - 24, cx + 28, cy + 8, CYAN);
  // front wings (sky)
  d.fillTriangle(cx, cy - 4, cx - 26, cy - 14, cx - 16, cy + 10, SKY);
  d.fillTriangle(cx, cy - 4, cx + 26, cy - 14, cx + 16, cy + 10, SKY);
  // tail flames (three layered Vs)
  d.fillTriangle(cx - 6, cy + 10, cx + 6, cy + 10, cx, cy + 34, DEEP);
  d.fillTriangle(cx - 4, cy + 10, cx + 4, cy + 10, cx, cy + 27, CYAN);
  d.fillTriangle(cx - 14, cy + 8, cx - 6, cy + 10, cx - 12, cy + 24, CYAN);
  d.fillTriangle(cx + 14, cy + 8, cx + 6, cy + 10, cx + 12, cy + 24, CYAN);
  // body: elongated diamond
  d.fillTriangle(cx - 7, cy - 6, cx + 7, cy - 6, cx, cy + 14, CYAN);
  d.fillTriangle(cx - 7, cy - 6, cx + 7, cy - 6, cx, cy - 14, SKY);
  // wing feather scallops (three rows of arcs per wing)
  for (int i = 0; i < 5; i++) {
    d.drawCircle(cx - 16 - i * 6, cy - 10 + i * 4, 3, SKY);
    d.drawCircle(cx + 16 + i * 6, cy - 10 + i * 4, 3, SKY);
    d.drawCircle(cx - 12 - i * 5, cy - 2 + i * 4, 2, WHITE);
    d.drawCircle(cx + 12 + i * 5, cy - 2 + i * 4, 2, WHITE);
  }
  // body scale lines
  for (int i = 0; i < 3; i++) d.drawFastHLine(cx - 4 + i, cy + 2 + i * 4, 8 - i * 2, DEEP);
  // tail fire: hot core cooling outward (yellow -> orange -> red)
  d.fillTriangle(cx - 3, cy + 26, cx + 3, cy + 26, cx, cy + 38, 0xF800);
  d.fillTriangle(cx - 2, cy + 26, cx + 2, cy + 26, cx, cy + 34, 0xFD20);
  d.fillTriangle(cx - 1, cy + 26, cx + 1, cy + 26, cx, cy + 31, 0xFFE0);
  // head + crest flames + beak + eye
  d.fillTriangle(cx - 5, cy - 22, cx - 1, cy - 22, cx - 4, cy - 31, 0xF800);
  d.fillTriangle(cx + 1, cy - 23, cx + 5, cy - 23, cx + 4, cy - 30, 0xFD20);
  d.fillCircle(cx, cy - 18, 7, SKY);
  d.fillTriangle(cx - 3, cy - 24, cx + 3, cy - 24, cx, cy - 32, CYAN);   // crest
  d.fillTriangle(cx - 2, cy - 15, cx + 2, cy - 15, cx, cy - 10, WHITE);  // beak
  d.fillCircle(cx, cy - 19, 2, 0x0000);                                  // eye
  d.fillCircle(cx + 1, cy - 20, 1, WHITE);                               // glint
}

static uint32_t splashHash(uint32_t x) { x *= 2654435761u; x ^= x >> 13; return x; }

static void drawSnowflake(LGFX_Sprite& d, int x, int y, int r, uint16_t color) {
  d.drawLine(x - r, y, x + r, y, color);
  d.drawLine(x, y - r, x, y + r, color);
  const int q = max(1, r * 2 / 3);
  d.drawLine(x - q, y - q, x + q, y + q, color);
  d.drawLine(x - q, y + q, x + q, y - q, color);
  if (r >= 3) {  // little branch ticks
    d.drawPixel(x - r, y - 1, color); d.drawPixel(x + r, y + 1, color);
    d.drawPixel(x - 1, y - r, color); d.drawPixel(x + 1, y + r, color);
  }
}

// One splash frame. Embers leave the tail/wing flames hot (yellow ->
// orange -> red), cool as they climb (pale cyan), and a separate field
// of snowflakes drifts down around the bird - fire becoming snow.
static void drawSplashFrame(uint32_t frame) {
  if (!uiEnsureCanvas()) return;
  auto& d = g_canvas;
  d.fillScreen(0x0010);
  d.fillRect(0, 30, 240, 1, 0x0015);
  d.fillRect(0, 62, 240, 1, 0x0015);
  const int cx = 120;

  // Rising embers (behind the bird at the bottom, in front near top).
  for (int i = 0; i < 22; i++) {
    const uint32_t h = splashHash((uint32_t)i * 7u + 1u);
    const int life = (int)((frame * 2 + (h % 47)) % 58);        // 0..57 climb
    const int x = cx - 46 + (int)(h % 92) + (life / 9) * ((i & 1) ? 1 : -1);
    const int y = 74 - life;
    if (y < 12) continue;
    uint16_t color;
    if (life < 14) color = 0xFFE0;         // white-yellow hot
    else if (life < 28) color = 0xFD20;    // orange
    else if (life < 42) color = 0xF800;    // cooling red
    else color = 0x87FF;                   // cooled to ice blue
    if (life >= 46) drawSnowflake(d, x, y, 2, 0xBFFF);  // ember -> flake
    else d.fillCircle(x, y, life < 20 ? 2 : 1, color);
  }

  // Rebirth: the rising embers ignite the phoenix - it grows from
  // the ember bed (frame ticks every 120 ms), flares once, settles.
  if (frame >= 4) {
    const int sc = frame < 7 ? 2 : frame < 10 ? 3 : frame < 13 ? 4 : 5;
    drawPhoenix(d, cx - 8 * sc, 80 - 16 * sc, sc);
    if (frame == 14 || frame == 15)
      d.drawCircle(cx, 42, 32 + (int)(frame - 14) * 9, 0xFFFF);
  }

  // Falling snowflakes around the bird.
  for (int i = 0; i < 14; i++) {
    const uint32_t h = splashHash((uint32_t)i * 13u + 5u);
    const int y = 4 + (int)((frame + (h % 89)) % 76);
    const int x = 6 + (int)(h % 228) + ((y / 12) & 1 ? 1 : -1);
    if (y > 76) continue;                                // keep text clear
    drawSnowflake(d, x, y, (i % 3) + 1, (i & 1) ? 0xFFFF : 0x87FF);
  }

  d.setTextSize(2);
  d.setTextColor(0x07FF, 0x0010);
  const char* name = "AEGIS PHOENIX";
  d.setCursor((240 - (int)strlen(name) * 12) / 2, 84);
  d.print(name);
  d.setTextSize(1);
  d.setTextColor(0xFFFF, 0x0010);
  const char* sub = "BLUE-TEAM DEFENSE SUITE";
  d.setCursor((240 - (int)strlen(sub) * 6) / 2, 100);
  d.print(sub);
  if (!g_diagMode) {
    d.setTextColor(0x8410, 0x0010);
    d.setCursor((240 - 13 * 6) / 2, 116);
    d.print("Cardputer ADV");
  }
  if (g_diagMode) {
    // Shown ONLY after a detected crash + restart (v0.7.9, user request):
    // normal boots stay clean. The stage log is always recorded silently
    // in NVS; after a crash the device says so, names where it died, and
    // applies its self-healing (below) automatically.
    d.setTextColor(0xFFE0, 0x0010);
    String prev = "crash logged: prev=" + g_prevStage;
    d.setCursor((240 - (int)prev.length() * 6) / 2, 117);
    d.print(prev);
    if (g_healNote.length()) {
      d.setTextColor(0x07FF, 0x0010);
      String heal = "self-heal: " + g_healNote;
      d.setCursor((240 - (int)heal.length() * 6) / 2, 127);
      d.print(heal);
    }
  }
  d.pushSprite(0, 0);
}

static void drawSplash() { drawSplashFrame(0); }

// Battery gauge (v0.8.0): the PMIC's own % is voltage-derived and jumps
// when USB is plugged in (63% -> 83% on the bench unit) because charge
// voltage inflates the reading, and isCharging() does not report on the
// ADV. So: percent comes from a Li-ion voltage curve, is slew-limited
// to 1%/s, and FREEZES while charging is detected (PMIC flag OR a
// >=50 mV voltage step/rise = plug-in event), creeping up at most
// 1% per 2 min. The bolt shows only while charging is detected.
struct BatteryGauge {
  int pct = -1;
  bool charging = false;
  int mv = 0;
  void update() {
    const uint32_t now = millis();
    if (pct >= 0 && now - lastMs_ < 1000) return;
    const int v = M5Cardputer.Power.getBatteryVoltage();
    if (v <= 0) { if (pct < 0) pct = M5Cardputer.Power.getBatteryLevel(); return; }
    const bool pmicCharging = M5Cardputer.Power.isCharging();
    if (!charging && (pmicCharging || (lastMv_ > 0 && v - lastMv_ >= 50))) {
      charging = true; chargeStartPct_ = pct; chargeStartMs_ = now;
    } else if (charging && !pmicCharging && lastMv_ > 0 && lastMv_ - v >= 60) {
      charging = false; chargeStartPct_ = -1;
    }
    lastMv_ = v; mv = v; lastMs_ = now;
    int target = pctFromMv(v);
    if (charging) {
      if (chargeStartPct_ < 0) { chargeStartPct_ = pct < 0 ? target : pct; chargeStartMs_ = now; }
      const int allowed = chargeStartPct_ + (int)((now - chargeStartMs_) / 120000);
      if (target > allowed) target = allowed;
      if (pct >= 0 && target < pct) target = pct;
    }
    if (pct < 0) pct = target;
    else if (target > pct) pct = min(target, pct + 1);
    else if (target < pct) pct = max(target, pct - 1);
  }
 private:
  uint32_t lastMs_ = 0, chargeStartMs_ = 0;
  int lastMv_ = 0, chargeStartPct_ = -1;
  static int pctFromMv(int v) {
    static const int pts[][2] = {{3270,0},{3610,10},{3690,20},{3730,30},{3780,40},
                                 {3830,50},{3880,60},{3940,70},{4000,80},{4070,90},{4200,100}};
    if (v <= pts[0][0]) return 0;
    for (int i = 1; i < 11; i++) {
      if (v <= pts[i][0]) {
        const int v0 = pts[i-1][0], v1 = pts[i][0], p0 = pts[i-1][1], p1 = pts[i][1];
        return p0 + (v - v0) * (p1 - p0) / (v1 - v0);
      }
    }
    return 100;
  }
};
static BatteryGauge g_batt;

template <typename T>
static inline void drawBatteryIcon(T& d, int x, int y, uint16_t bg) {
  g_batt.update();
  const int lvl = g_batt.pct;
  const bool charging = g_batt.charging;
  String pct = String(lvl) + "%";
  d.setTextSize(1);
  d.setTextColor(lvl <= 15 ? 0xF800 : (lvl <= 35 ? 0xFFE0 : 0x07E0), bg);
  d.setCursor(x - (int)pct.length() * 6 - 3, y + 2);
  d.print(pct);
  d.drawRect(x, y, 20, 10, 0xC618);
  d.fillRect(x + 20, y + 3, 2, 4, 0xC618);
  const int fill = (lvl * 16) / 100;
  const uint16_t col = charging ? 0x07FF : (lvl <= 15 ? 0xF800 : (lvl <= 35 ? 0xFFE0 : 0x07E0));
  if (fill > 0) d.fillRect(x + 2, y + 2, fill, 6, col);
  if (charging) {
    if ((millis() / 600) & 1) d.fillRect(x + 2, y + 2, 3, 6, 0x07FF);   // pulse
    d.drawLine(x + 11, y + 1, x + 8, y + 5, 0x0000);   // black bolt: contrast
    d.drawLine(x + 8, y + 5, x + 11, y + 5, 0x0000);   // over the bright fill
    d.drawLine(x + 11, y + 5, x + 9, y + 9, 0x0000);
  }
}

static void uiHeader(const char* title) {
  auto& d = g_canvas;
  d.fillScreen(0x0000);
  d.fillRect(0, 0, 240, 19, 0x0015);
  drawPhoenix(d, 2, 1, 1);
  d.setTextSize(1);
  d.setTextColor(0x07FF, 0x0015);
  d.setCursor(22, 2);
  d.print("AEGIS ");
  d.setTextColor(0xFFFF, 0x0015);
  d.print(title);
  if (millis() < 45000 && g_bootStage.length()) {
    // Boot/loading stage lives HERE now (v0.9.8, user request): in the
    // blue header, between the screen title and the battery - never
    // as stray white text at the bottom of the screen. It is only a
    // loading breadcrumb, so it vanishes after 45 s like before.
    d.setTextSize(1); d.setTextColor(0xFFE0, 0x0015); d.setCursor(130, 2);
    d.print(fitLine("boot:" + g_bootStage, 13));
  }
  drawBatteryIcon(d, 218, 1, 0x0015);
  d.setTextColor(quarantined ? 0xF800 : 0x07E0, 0x0015);
  d.setCursor(22, 11);
  d.print(quarantined ? "QUARANTINED" : "ARMED");
  d.setTextColor(0xFFFF, 0x0015);
  d.printf(" evt:%u %lus", (unsigned)g_eventsLogged, (millis() - bootMillis) / 1000);
  if (g_scanActive) {
    d.setTextColor(((millis() / 500) & 1) ? 0x07FF : 0x87FF, 0x0015);
    d.setCursor(190, 11); d.print("SCAN");
  }
  d.drawFastHLine(0, 20, 240, 0x07FF);
}

static void uiFooter() {
  // The menu bar IS the bottom border now (v0.8.0, user request): a blue
  // band on the very bottom edge of the screen with its words inside it.
  // Nothing extra to draw here.
}

static void drawMenuBar() {
  auto& d = g_canvas;
  if (g_view == VIEW_MENU) {
    d.fillRect(0, 122, 240, 13, 0x0015);
    d.drawFastHLine(0, 121, 240, 0x07FF);
    d.setTextSize(1); d.setTextColor(0xFFFF, 0x0015); d.setCursor(3, 125);
    d.print("MENU - all options, grouped");
    d.setTextColor(0x07FF, 0x0015); d.setCursor(196, 125); d.print("ESC=back");
    return;
  }
  if (g_view == VIEW_LOGS || g_view == VIEW_ATLAS) {
    String line = g_view == VIEW_LOGS
        ? (String("LOG ") + kLogFiles[g_logFileSel] + " - arrows scroll")
        : (g_atlasExpanded ? String("ATLAS DETAIL") : String("ATLAS ") + String(g_atlasSel + 1) + "/" +
           String((unsigned)g_wifiAtlas.entries.size()) + " - select");
    d.fillRect(0, 122, 240, 13, 0x0015);
    d.drawFastHLine(0, 121, 240, 0x07FF);
    d.setTextSize(1); d.setTextColor(0xFFFF, 0x0015); d.setCursor(3, 125);
    d.print(fitLine(line, 33));
    d.setTextColor(0x07FF, 0x0015); d.setCursor(207, 125); d.print(g_atlasExpanded ? "BACK" : "OPEN");
    return;
  }
  if (g_view == VIEW_EVENTS || g_view == VIEW_REMOTE) {
    String line;
    String action = "ENTER";
    if (g_view == VIEW_EVENTS) {
      if (g_eventHistory.empty()) line = "No events yet - passive RX still runs";
      else if (g_eventExpanded) {
        const std::vector<int> vis = visibleEventIndices();
        const int idx = vis.empty() ? 0 : vis[min(g_eventSel, (int)vis.size() - 1)];
        line = "EVENT #" + String(g_eventHistory[idx].id) + " - ENTER=reviewed";
        action = "DONE?";
      } else {
        line = "EVENT " + String(g_eventSel + 1) + "/" + String((unsigned)visibleEventIndices().size()) +
               " [" + kEventFilterNames[g_eventFilter] + "]";
        action = "OPEN";
      }
    } else {
      line = "> " + String(IrRemote::buttonLabel(g_remoteSel));
      line += g_irRemote.learning ? " LEARNING" : " SEND";
      action = "SEND";
    }
    d.fillRect(0, 122, 240, 13, 0x0015);
    d.drawFastHLine(0, 121, 240, 0x07FF);
    d.setTextSize(1); d.setTextColor(0xFFFF, 0x0015); d.setCursor(3, 125);
    d.print(fitLine(line, 33));
    d.setTextColor(0x07FF, 0x0015); d.setCursor(207, 125); d.print(action);
    return;
  }
  const AegisMenuItem& item = aegisMenuItem(g_view);
  String label = item.label;
  if (item.action == 'w') label += g_wardriveOn ? " ON" : " OFF";
  if (item.action == 'q') label += quarantined ? " ON" : " OFF";
  if (item.action == '\x07') label += g_passiveOffline ? " ON" : " OFF";
  if (item.action == '\x0B') label += g_backgroundScansEnabled ? " ON" : " OFF";
  if (item.action == '\x0E') label += g_c5HotSwapEnabled ? " ON" : " OFF";
  String line = "> " + String((unsigned)g_menuSel[(int)g_view] + 1) + "/" +
                String(aegisMenuCount(g_view)) + " " + label;
  d.fillRect(0, 122, 240, 13, 0x0015);
  d.drawFastHLine(0, 121, 240, 0x07FF);
  d.setTextSize(1);
  d.setTextColor(0xFFFF, 0x0015);
  d.setCursor(3, 125);
  d.print(fitLine(line, 33));
  d.setTextColor(0x07FF, 0x0015);
  d.setCursor(207, 125);
  d.print("ENTER");
}

static void drawCurrentView() {
  if (!uiEnsureCanvas()) return;
  auto& d = g_canvas;
  switch (g_view) {
    case VIEW_DASH: {
      uiHeader("DASHBOARD");
      int y = 25;
      auto big = [&](const char* label, const String& val, uint16_t c = 0xFFFF) {
        d.setTextSize(1); d.setTextColor(0x8410, 0x0000); d.setCursor(4, y); d.print(label);
        d.setTextSize(2); d.setTextColor(c, 0x0000); d.setCursor(4, y + 9); d.print(fitLine(val, 20)); y += 27;
      };
      big("WIFI", WiFi.status() == WL_CONNECTED ? WiFi.SSID() : "OFFLINE", WiFi.status() == WL_CONNECTED ? 0x07E0 : 0xFFE0);
      big("EVENTS", String(g_eventsLogged) + (g_wardriveOn ? "  WARDRIVE" : ""), g_eventsLogged ? 0xFFE0 : 0xFFFF);
      if (g_wifiScanRunning || g_bleScanRunning || g_lanScan.active) {
        int pct; const char* lbl;
        if (g_lanScan.active) { pct = (g_lanScan.nextHost * 100) / 254; lbl = "LAN SCAN"; }
        else if (g_bleScanRunning && !g_wifiScanRunning) {
          const uint32_t rem = (int32_t)(g_bleScanEndsMs - millis()) > 0 ? g_bleScanEndsMs - millis() : 0;
          pct = 100 - (int)(rem * 100 / 3000); if (pct > 99) pct = 99; lbl = "BLE SCAN";
        } else { pct = (int)((millis() - g_wifiScanStartedMs) * 100 / 6000); if (pct > 99) pct = 99; lbl = "WIFI SCAN"; }
        d.setTextSize(1); d.setTextColor(0x07FF, 0x0000); d.setCursor(4, y); d.print(lbl);
        d.drawRect(64, y, 122, 7, 0x07FF); d.fillRect(65, y + 1, 120 * pct / 100, 5, 0x07E0);
        d.setTextColor(0xFFFF, 0x0000); d.setCursor(192, y); d.print(String(pct) + "%");
        y += 12;
      }
      d.setTextSize(1); d.setTextColor(0x8410, 0x0000); d.setCursor(4, y); d.print("LAST EVENT");
      d.setTextColor(0xFFE0, 0x0000); d.setCursor(4, y + 9);
      d.print(fitLine(g_lastEventTitle.length() ? g_lastEventTitle : String("none yet"), 40));
      break;
    }
    case VIEW_MODULES: {
      // DEVICES: verified census of everything attached/stored, taken
      // once at boot (20 s window). Absent hats get their polling
      // switched off (g_gnssOff / g_c5Off / loraHat.active==false) so
      // they cost no power and claim no screen space. D re-checks.
      uiHeader("DEVICES");
      int y = 24;
      auto dev = [&](const char* name, const String& st, uint16_t col) {
        d.setTextSize(1);
        d.setTextColor(0xFFFF, 0x0000); d.setCursor(4, y); d.print(name);
        d.setTextColor(col, 0x0000); d.setCursor(104, y); d.print(fitLine(st, 22));
        y += 10;
      };
      dev("SD card", sdLogMgr.mounted ? ("mounted - " + String((SD.totalBytes() - SD.usedBytes()) / 1048576.0 / 1024.0, 1) + " GB free") : "NOT FOUND",
          sdLogMgr.mounted ? 0x07E0 : 0xF800);
      dev("Battery", String(g_batt.mv) + " mV - " + String(g_batt.pct) + "%" + (g_batt.charging ? " CHARGING" : ""),
          g_batt.charging ? 0x07FF : 0x07E0);
      dev("Shadow vault", g_shadowState == 2 ? "UNLOCKED (creds+baseline)" :
          g_shadowState == 1 ? "locked - menu opens it" : "not set - menu creates it",
          g_shadowState == 2 ? 0x07E0 : g_shadowState == 1 ? 0xFFE0 : 0x8410);
      dev("LoRa Cap SX1262", loraHat.active ? ("ACTIVE " + String((unsigned)loraHat.packetsCaptured) + " pkts") : "not detected (off)",
          loraHat.active ? 0x07E0 : 0x8410);
      dev("GNSS on Cap", g_gnssOff ? "not detected (off)" :
          gnssHat.active ? (gnssHat.currentFix.valid ? "ACTIVE - fix" : "ACTIVE - no fix yet") : "listening...",
          g_gnssOff ? 0x8410 : 0x07E0);
      dev("ESP32-C5 (Grove)", g_c5Off ? "not detected (off)" :
          esp32c5.active ? "LINKED" : "probing...",
          g_c5Off ? 0x8410 : esp32c5.active ? 0x07E0 : 0xFFE0);
      dev("RFID2 reader", !rfid2.enabled ? "OFF (opt-in)" :
          rfid2.present ? "ATTACHED" : "ON - NOT attached",
          !rfid2.enabled ? 0x8410 : rfid2.present ? 0x07E0 : 0xFFE0);
      dev("WiFi", (WiFi.status() == WL_CONNECTED ? ("joined " + WiFi.SSID()) :
          g_wifiSsid.length() ? ("stored " + g_wifiSsid + ", offline") : "none stored") +
          " - sniff ch" + String(g_sniffChannel) + (g_chanHop ? " hop" : ""),
          WiFi.status() == WL_CONNECTED ? 0x07E0 : 0xFFE0);
      dev("Bluetooth LE", "ready - " + String((unsigned)bleTable.size()) + " advertisers seen", 0x07E0);
      d.setTextColor(0x8410, 0x0000); d.setCursor(4, 114);
      if (g_shadowTyping == 4) {
        // SSID is not a secret: show exactly what was typed (v0.9.8).
        d.setTextColor(0xFFE0, 0x0000); d.setCursor(4, 104);
        d.print(fitLine(g_shadowMsg, 40));
        d.setTextColor(0xFFFF, 0x0000); d.setCursor(4, 114);
        d.print(fitLine("SSID: " + g_shadowBuf + "_", 40));
      } else if (g_shadowTyping == 5) {
        String stars; for (size_t i = 0; i < g_shadowBuf.length(); i++) stars += "*";
        d.setTextColor(0x07FF, 0x0000); d.setCursor(4, 104);
        d.print(fitLine("Network: " + g_netSsidTemp, 40));
        d.setTextColor(0xFFE0, 0x0000); d.setCursor(4, 114);
        d.print(fitLine("Password: " + stars + " (" + String((unsigned)g_shadowBuf.length()) + " chars)", 40));
      } else if (g_shadowTyping) {
        String stars; for (size_t i = 0; i < g_shadowBuf.length(); i++) stars += "*";
        d.setTextColor(0xFFE0, 0x0000);
        d.print(fitLine(g_shadowMsg + " " + stars, 40));
      } else if (g_shadowMsg.length()) {
        d.print(fitLine("shadow: " + g_shadowMsg, 40));
      } else {
        d.print(fitLine(g_censusDone ? "boot census done - absent hats powered down. D=re-check"
                                     : "verifying devices (20 s window)...", 40));
      }
      break;
    }
    case VIEW_EVENTS: {
      uiHeader("EVENTS");
      const std::vector<int> vis = visibleEventIndices();
      if (vis.empty()) {
        int y = 27;
        auto row = [&](const char* label, const String& val) {
          d.setTextSize(1); d.setTextColor(0x8410, 0x0000); d.setCursor(4, y); d.print(label);
          d.setTextColor(0xFFFF, 0x0000); d.setCursor(120, y); d.print(fitLine(val, 20)); y += 14;
        };
        row("Events logged", String(g_eventsLogged));
        row("Stored summaries", String((unsigned)g_eventHistory.size()));
        row("Deauth frames", String(g_deauthFramesTotal));
        row("Filter [F]", kEventFilterNames[g_eventFilter]);
        row("LoRa packets", String(loraHat.packetsCaptured));
        d.setTextColor(0x07FF, 0x0000); d.setCursor(4, 101);
        d.print("Nothing matches - detector still listening.");
      } else {
        if (g_eventSel < 0) g_eventSel = 0;
        if (g_eventSel >= (int)vis.size()) g_eventSel = (int)vis.size() - 1;
        const AegisEventRecord& rec = g_eventHistory[vis[g_eventSel]];
        if (!g_eventExpanded) {
          d.setTextSize(1); d.setTextColor(0x8410, 0x0000); d.setCursor(4, 24);
          d.print(String((unsigned)vis.size()) + " ev [" + kEventFilterNames[g_eventFilter] + "] </> filt");
          d.setTextColor(0x07FF, 0x0000); d.setCursor(196, 24); d.print("ENTER>");
          const int first = followWindow(g_eventSel, (int)vis.size(), 4, g_eventTop);
          int y = 36;
          for (int shown = 0; shown < 4 && first + shown < (int)vis.size(); shown++) {
            const int displayIndex = first + shown;
            const AegisEventRecord& e = g_eventHistory[vis[displayIndex]];
            const bool selected = displayIndex == g_eventSel;
            if (selected) d.fillRect(0, y - 2, 240, 17, 0x0015);
            d.setTextSize(1);
            d.setTextColor(selected ? 0x07FF : 0x8410, selected ? 0x0015 : 0x0000);
            d.setCursor(3, y);
            d.print((selected ? ">" : " ") + String("#") + String(e.id) + " " + (e.previousBoot ? String("prev") : eventAgeString(e.atMs)) + " " + e.kind);
            if (e.confidence) d.print(" " + String(e.confidence) + "%");
            if (e.reviewed) { d.setTextColor(0x07E0, selected ? 0x0015 : 0x0000); d.print(" [done]"); }
            d.setTextColor(0xFFFF, selected ? 0x0015 : 0x0000);
            d.setCursor(5, y + 8);
            d.print(fitLine(e.title, 39));
            y += 18;
          }
          scrollHints(first, min((int)vis.size(), first + 4), (int)vis.size(), 36, 90);
          drawScrollBar(first, 4, (int)vis.size(), 36, 90);
        } else {
          std::vector<String> lines;
          lines.push_back("#" + String(rec.id) + " " + rec.title);
          lines.push_back("kind: " + rec.kind + " / " + rec.channel);
          lines.push_back("when: " + eventClock(rec) + (rec.previousBoot ? String("") : " (" + eventAgeString(rec.atMs) + ")"));
          lines.push_back("confidence: " + String(rec.confidence) + "%  reviewed: " + (rec.reviewed ? "yes" : "no"));
          lines.push_back("source:");
          appendWrappedLines(lines, rec.source.length() ? rec.source : String("-"));
          if (rec.fieldsJson.length()) {
            lines.push_back("details:");
            appendWrappedLines(lines, rec.fieldsJson);
          } else {
            lines.push_back("details: in encrypted SD vault (prior boot)");
          }
          lines.push_back(sdLogMgr.mounted ? "archive: SD vault + event index" : "archive: RAM only (no SD)");
          const int visible = 7;
          const int maxScroll = max(0, (int)lines.size() - visible);
          if (g_eventDetailScroll < 0) g_eventDetailScroll = 0;
          if (g_eventDetailScroll > maxScroll) g_eventDetailScroll = maxScroll;
          int y = 25;
          for (int i = g_eventDetailScroll; i < (int)lines.size() && i < g_eventDetailScroll + visible; i++) {
            d.setTextSize(1);
            d.setTextColor(i == 0 ? 0x07FF : 0xFFFF, 0x0000);
            d.setCursor(3, y);
            d.print(fitLine(lines[i], 39));
            y += 11;
          }
          if ((int)lines.size() > visible) {
            d.setTextColor(0x07FF, 0x0000); d.setCursor(218, 98);
            d.print(String(g_eventDetailScroll + 1) + "/" + String(maxScroll + 1));
          }
        }
      }
      break;
    }
    case VIEW_CAP: {
      uiHeader("CAP RADIO");
      int y = 23;
      auto row = [&](const char* label, const String& val, uint16_t c = 0xFFFF) {
        d.setTextSize(1); d.setTextColor(0x8410, 0x0000); d.setCursor(4, y); d.print(label);
        d.setTextColor(c, 0x0000); d.setCursor(96, y); d.print(fitLine(val, 24)); y += 12;
      };
      row("LoRa SX1262", loraHat.active ? "LISTENING 915" : "not detected", loraHat.active ? 0x07E0 : 0xFFFF);
      row("Packets/RSSI", String(loraHat.packetsCaptured) + " / " + String(loraHat.lastRssi) + "dBm");
      row("Last packet", loraHat.lastPacketKind.length() ? (loraHat.lastPacketKind + " " + loraHat.lastPacketDetail) : "-",
          loraHat.lastPacketKind == "LoRaWAN" ? 0x07FF : 0xFFFF);
      if (loraHat.scanBusy) row("Ch sweep", "running (stepped)...", 0x07FF);
      row("Noise floor", String(loraHat.channelRssi) + " dBm" + (loraHat.jammingDetected ? " JAM?" : ""), loraHat.jammingDetected ? 0xF800 : 0xFFFF);
      if (loraHat.scanCount > 0) {
        String top = "";
        for (int i = 0; i < loraHat.scanCount && i < 3; i++) top += String(loraHat.scanFreq[i], 1) + ":" + String(loraHat.scanRssi[i]) + " ";
        row("Ch scan", top, 0x07FF);
      } else {
        row("Ch scan", loraHat.active ? "menu: Start ch scan" : "--");
      }
      row("C5 5GHz link", esp32c5.active ? ("ONLINE " + esp32c5.firmware) :
          (g_c5HotSwapEnabled ? "waiting Grove G1/G2" : "disabled in SETUP"),
          esp32c5.active ? 0x07E0 : 0xFFE0);
      row("5G APs/deauth", String(esp32c5.apCount5G) + " / " + String(esp32c5.deauth5GCount) +
          (esp32c5.connected5G ? " joined" : ""), esp32c5.active ? 0x07FF : 0x8410);
      row("RFID2 [R toggles]", !rfid2.enabled ? "OFF - only if reader attached" : rfid2.present ? (rfid2.lastUid.length() ? (rfid2.lastUid + " x" + String(rfid2.seenCount)) : "ready, no tag yet") : "ON - NOT attached (Grove I2C)",
          !rfid2.enabled ? 0x8410 : rfid2.present ? 0x07E0 : 0xFFE0);
      row("Position", gnssHat.currentFix.valid ? (String(gnssHat.currentFix.latitude, 5) + "," + String(gnssHat.currentFix.longitude, 5)) : "no fix", gnssHat.currentFix.valid ? 0x07E0 : 0xFFFF);
      row("Sats/HDOP", String(gnssHat.currentFix.satellites) + " / " + String(gnssHat.currentFix.hdop, 1));
      break;
    }
    case VIEW_NET: {
      uiHeader("NET SCAN");
      int y = 24;
      d.setTextSize(1);
      d.setTextColor(0x8410, 0x0000); d.setCursor(4, y);
      d.print(WiFi.status() == WL_CONNECTED ? ("LAN " + WiFi.localIP().toString()) : "NOT ON WIFI");
      if (g_lanScan.active) {
        d.setTextColor(0x07FF, 0x0000); d.setCursor(120, y);
        d.print("scanning " + String(g_lanScan.nextHost) + "/254 - ESC/menu stops");
        const int bw = 180; const int pct = (g_lanScan.nextHost * 100) / 254;
        d.drawRect(4, y + 8, bw + 2, 6, 0x07FF);
        d.fillRect(5, y + 9, (bw * pct) / 100, 4, 0x07E0);
        d.setCursor(190, y + 7); d.setTextColor(0xFFFF, 0x0000); d.print(String(pct) + "%");
      }
      y += 14;
      d.setTextColor(0xFFFF, 0x0000);
      if (!g_lanHosts.size()) {
        d.setCursor(4, y); d.print("Menu > LAN scan: scan YOUR subnet");
        d.setCursor(4, y + 11); d.print("live hosts + open services.");
        d.setCursor(4, y + 22); d.setTextColor(0xFFE0, 0x0000); d.print("Active probing - own nets only!");
      } else {
        const size_t pages = (g_lanHosts.size() + 4) / 5;
        if (g_netPage >= pages) g_netPage = 0;
        const size_t first = (size_t)g_netPage * 5;
        for (size_t i = first; i < g_lanHosts.size() && i < first + 5; i++) {
          d.setCursor(4, y); d.print(fitLine(g_lanHosts[i], 40)); y += 11;
        }
        d.setTextColor(0x07FF, 0x0000); d.setCursor(4, 99);
        d.print("hosts page " + String((unsigned)g_netPage + 1) + "/" + String((unsigned)pages));
      }
      break;
    }
    case VIEW_DIAG: {
      uiHeader("INTEGRITY");
      int y = 24;
      auto row = [&](const char* label, const String& val, uint16_t c = 0xFFFF) {
        d.setTextSize(1); d.setTextColor(0x8410, 0x0000); d.setCursor(4, y); d.print(label);
        d.setTextColor(c, 0x0000); d.setCursor(100, y); d.print(fitLine(val, 23)); y += 9;
      };
      row("Firmware", g_integrity.appVersion.length() ? g_integrity.appVersion : FIRMWARE_VER);
      row("Partition", g_integrity.partitionLabel + " " + g_integrity.partitionSha.substring(0, 10),
          g_integrity.partitionSha.length() ? 0x07E0 : 0xF800);
      row("SD programs", String(g_integrity.binsValid) + "/" + String(g_integrity.binsFound) + " valid " +
          String(g_integrity.binsQuarantined) + " quarantined", g_integrity.binsQuarantined ? 0xFFE0 : 0x07E0);
      row("SD probe", g_integrity.sdProbeOk ? "OK" : "FAIL", g_integrity.sdProbeOk ? 0x07E0 : 0xF800);
      row("Vault live", g_integrity.vaultOk ? "AES OK" : "n/a", g_integrity.vaultOk ? 0x07E0 : 0xFFFF);
      row("Parser fuzz", "R" + String(g_integrity.rulesFuzzCases) + " G" + String(g_integrity.gnssFuzzCases));
      row("Heap free", String(g_integrity.freeHeap / 1024) + " KB");
      row("Boot streak", String(g_bootStreak) + (g_safeMode ? " SAFE" : ""), g_safeMode ? 0xF800 : 0xFFFF);
      row("Loop worst", String(g_loopWatch.worstGapMs()) + " ms",
          g_loopWatch.worstGapMs() > 1000 ? 0xF800 : 0xFFFF);
      row("WiFi check", g_homeJoinStatus,
          g_homeJoinStatus.startsWith("FAIL") ? 0xF800 : (g_homeJoinStatus.startsWith("CONNECTED") ? 0x07E0 : 0xFFFF));
      row("PC link", g_linkApprovedHost.length() ? ("approved " + g_linkApprovedHost) : "not confirmed (ENTER prompt)",
          g_linkApprovedHost.length() ? 0x07E0 : 0xFFE0);
      break;
    }
    case VIEW_HELP: {
      uiHeader("MENU / HELP");
      int y = 23;
      auto line = [&](const String& txt, uint16_t c = 0xFFFF) {
        d.setTextSize(1); d.setCursor(3, y); d.setTextColor(c, 0x0000); d.print(fitLine(txt, 40)); y += 10;
      };
      line("Fn+;,. / = arrow keys (navigation)", 0x07FF);
      line("M = the ONE menu - everything lives there");
      line("UP/DN choose  LEFT/RIGHT adjust  ENTER run");
      line("ESC back - no letter keys do anything");
      line("...unless YOU bind them: menu > Key binds");
      line("Scans: WiFi/BLE probes, wardrive, LAN,");
      line("LoRa sweep, integrity - all in the menu");
      line("REMOTE: pick button, ENTER sends; the");
      line("ACTION row auto-finds or learns codes");
      line("Rule manager: ENTER toggles a rule");
      line("USB serial: AEGIS EVENTS / EXPORT MAP");
      line("No deauth / jamming / injection.", 0x07E0);
      break;
    }
    case VIEW_SETUP: {
      uiHeader("SETUP");
      int y = 24;
      auto row = [&](const char* label, const String& val, uint16_t c = 0xFFFF) {
        d.setTextSize(1); d.setTextColor(0x8410, 0x0000); d.setCursor(4, y); d.print(label);
        d.setTextColor(c, 0x0000); d.setCursor(92, y); d.print(fitLine(val, 25)); y += 10;
      };
      row("Mode", g_passiveOffline ? "PASSIVE OFFLINE" : "NORMAL", g_passiveOffline ? 0x07FF : 0x07E0);
      row("WiFi", WiFi.status() == WL_CONNECTED ? WiFi.SSID() : (g_wifiSsid.length() ? "not joined" : "no SSID set"), WiFi.status() == WL_CONNECTED ? 0x07E0 : 0xFFE0);
      row("BG scans", String(g_backgroundScansEnabled ? "ON " : "OFF ") + g_scanStatus, g_backgroundScansEnabled ? 0x07E0 : 0xFFE0);
      row("Rules", String((unsigned)localRules.size()) + " armed, " + String(g_rulePackRejected) + " rejected");
      row("SD / Atlas", String(sdLogMgr.mounted ? "mounted " : "NO SD ") + String((unsigned)g_wifiAtlas.entries.size()) + " nets");
      row("Wardrive", g_wardriveOn ? "ON" : "OFF", g_wardriveOn ? 0x07FF : 0xFFFF);
      row("Bright/Sleep", String((unsigned)g_brightnessPct) + "% / " +
          (g_screenSleepSec ? (String((unsigned)g_screenSleepSec) + "s") : String("never")));
      row("Alert beeps", g_alertBeeps ? "beep + banner" : "banner only", g_alertBeeps ? 0x07E0 : 0xFFFF);
      row("Home WiFi", g_wifiSsid.length() ? g_wifiSsid : "not set - menu: Home WiFi", g_wifiSsid.length() ? 0x07E0 : 0xFFE0);
      row("Home IP", WiFi.status() == WL_CONNECTED ? WiFi.localIP().toString() : "-", WiFi.status() == WL_CONNECTED ? 0x07E0 : 0x8410);
      row("C5 hot-swap", !g_c5HotSwapEnabled ? "OFF (G1 free for IR RX)" :
          (esp32c5.active ? "ONLINE 5GHz active" : "ON - waiting on Grove"),
          esp32c5.active ? 0x07E0 : 0xFFE0);
      break;
    }
    case VIEW_REMOTE: {
      uiHeader("TV REMOTE");
      d.setTextSize(1);
      d.setTextColor(0x8410, 0x0000); d.setCursor(4, 23); d.print("Profile");
      d.setTextColor(0xFFFF, 0x0000); d.setCursor(58, 23); d.print(fitLine(g_irRemote.profileName, 30));
      d.setTextColor(g_irRemote.learning ? 0xFFE0 : 0x07FF, 0x0000); d.setCursor(4, 32);
      d.print(fitLine(g_irRemote.status, 40));
      d.setTextColor(0x8410, 0x0000); d.setCursor(4, 41);
      d.print("TX G44 built in - RX needs module on G1");
      {
        // Buttons + ACTION are one list; the window follows the
        // cursor (v0.9.7 standard) instead of running off-screen.
        const int total = IrRemote::kButtonCount + 1;
        const int visR = 7;
        const int first = followWindow(g_remoteSel, total, visR, g_remoteTop);
        int y = 49;
        for (int i = first; i < total && i < first + visR; i++, y += 10) {
          const bool selected = i == g_remoteSel;
          if (selected) d.fillRect(0, y - 1, 240, 10, 0x0015);
          d.setTextSize(1);
          d.setTextColor(selected ? 0x07FF : 0x8410, selected ? 0x0015 : 0x0000);
          d.setCursor(4, y); d.print(selected ? ">" : " ");
          d.setTextColor(0xFFFF, selected ? 0x0015 : 0x0000);
          if (i < IrRemote::kButtonCount) {
            d.print(IrRemote::buttonLabel(i));
            const bool learned = g_irRemote.hasButton(i);
            d.setTextColor(learned ? 0x07E0 : 0x8410, selected ? 0x0015 : 0x0000);
            d.setCursor(150, y); d.print(learned ? "CODE READY" : "NO CODE");
          } else {
            d.print(String("ACTION: ") + (g_remoteAction == 0 ? "Auto-find brand" : "Learn button") + " (</>)");
          }
        }
        scrollHints(first, min(total, first + visR), total, 49, 109);
        drawScrollBar(first, visR, total, 49, 109);
      }
      break;
    }
    case VIEW_BINDS: {
      uiHeader("KEY BINDS");
      const int n = (int)(sizeof(kUnifiedMenu) / sizeof(kUnifiedMenu[0]));
      if (g_bindSel >= n) g_bindSel = 0;
      const int vis = 7;
      int first = g_bindSel - vis / 2; if (first < 0) first = 0;
      if (first + vis > n) first = max(0, n - vis);
      int y = 26;
      for (int i = first; i < n && i < first + vis; i++) {
        const bool sel = i == g_bindSel;
        if (sel) d.fillRect(0, y - 1, 240, 11, 0x0015);
        char kb = 0;
        for (int b = 0; b < g_bindCount; b++) if (g_bindItem[b] == i) kb = g_bindKey[b];
        d.setTextSize(1);
        d.setTextColor(sel ? 0x07FF : 0x8410, sel ? 0x0015 : 0x0000); d.setCursor(4, y);
        d.print(sel ? ">" : " ");
        d.setTextColor(0xFFFF, sel ? 0x0015 : 0x0000); d.print(fitLine(kUnifiedMenu[i].label, 26));
        d.setTextColor(kb ? 0xFFE0 : 0x8410, sel ? 0x0015 : 0x0000); d.setCursor(196, y);
        d.print(kb ? String("[") + kb + "]" : String("  -"));
        y += 11;
      }
      drawScrollBar(first, vis, n, 26, 92);
      d.setTextColor(0x07FF, 0x0000); d.setCursor(4, 106);
      d.print(g_bindCapture ? ("press a key for: " + fitLine(kUnifiedMenu[g_bindSel].label, 22))
                            : "ENTER bind - LEFT clear - ESC back");
      d.setTextColor(0x8410, 0x0000); d.setCursor(4, 116);
      d.print("binds are yours only - none by default");
      break;
    }
    case VIEW_MENU: {
      uiHeader("MENU");
      struct Row { const UnifiedItem* it; const char* group; };
      std::vector<Row> rows;
      const char* lastGroup = nullptr;
      int selDisp = 0;
      for (size_t i = 0; i < sizeof(kUnifiedMenu) / sizeof(kUnifiedMenu[0]); i++) {
        if (!lastGroup || strcmp(lastGroup, kUnifiedMenu[i].group) != 0) {
          rows.push_back({nullptr, kUnifiedMenu[i].group});
          lastGroup = kUnifiedMenu[i].group;
        }
        if ((int)i == g_menuListSel) selDisp = (int)rows.size();
        rows.push_back({&kUnifiedMenu[i], nullptr});
      }
      const int vis = 8;
      const int first = followWindow(selDisp, (int)rows.size(), vis, g_menuListTop);
      int y = 26;
      for (int i = first; i < (int)rows.size() && i < first + vis; i++, y += 11) {
        const Row& r = rows[i];
        d.setTextSize(1);
        if (!r.it) {
          d.setTextColor(0x07FF, 0x0000); d.setCursor(4, y); d.print(r.group);
          continue;
        }
        const bool sel = (int)(r.it - kUnifiedMenu) == g_menuListSel;
        if (sel) d.fillRect(0, y - 1, 240, 10, 0x0015);
        d.setTextColor(sel ? 0x07FF : 0x8410, sel ? 0x0015 : 0x0000);
        d.setCursor(6, y); d.print(sel ? ">" : " ");
        d.setTextColor(0xFFFF, sel ? 0x0015 : 0x0000); d.print(r.it->label);
        String state;
        switch (r.it->act) {
          case 'w': state = g_wardriveOn ? "ON" : "OFF"; break;
          case 'B': state = g_backgroundScansEnabled ? "ON" : "OFF"; break;
          case 'O': state = g_passiveOffline ? "ON" : "OFF"; break;
          case 'C': state = g_c5HotSwapEnabled ? "ON" : "OFF"; break;
          case 'F': state = rfid2.enabled ? "ON" : "OFF"; break;
          case 'L': state = g_liveEnabled ? "ON" : "OFF"; break;
          case 'D': state = g_autoSync ? "ON" : "OFF"; break;
          case 'H': state = g_chanHop ? "ON" : "OFF"; break;
          case 'U': state = g_shadowState == 2 ? "OPEN" : g_shadowState == 1 ? "LOCK" : "NEW"; break;
          case 'Q': state = quarantined ? "ON" : "OFF"; break;
          default: break;
        }
        if (state.length()) {
          d.setTextColor(state == "ON" ? 0x07E0 : 0x8410, sel ? 0x0015 : 0x0000);
          d.setCursor(212, y); d.print(state);
        }
      }
      scrollHints(first, min((int)rows.size(), first + vis), (int)rows.size(), 26, 92);
      drawScrollBar(first, vis, (int)rows.size(), 26, 113);
      break;
    }
    case VIEW_LOGS: {
      uiHeader("LOG VIEWER");
      const std::vector<String> lines = readLogTail(kLogFiles[g_logFileSel]);
      d.setTextSize(1); d.setTextColor(0x8410, 0x0000); d.setCursor(4, 24);
      d.print(kLogFiles[g_logFileSel]);
      d.setTextColor(0x07FF, 0x0000); d.setCursor(150, 24); d.print("</> next file");
      const int visibleLines = 7;
      const int maxScroll = max(0, (int)lines.size() - visibleLines);
      if (g_logScroll < 0) g_logScroll = 0;
      if (g_logScroll > maxScroll) g_logScroll = maxScroll;
      // Default to the tail (newest at the bottom of the file).
      const int first = (int)lines.size() <= visibleLines ? 0 :
          max(0, (int)lines.size() - visibleLines - g_logScroll);
      int y = 36;
      for (int i = first; i < (int)lines.size() && i < first + visibleLines; i++) {
        d.setTextColor(0xFFFF, 0x0000); d.setCursor(3, y);
        d.print(fitLine(lines[i], 39));
        y += 11;
      }
      drawScrollBar(first, visibleLines, (int)lines.size(), 36, 113);
      break;
    }
    case VIEW_ATLAS: {
      uiHeader("ATLAS / WARDRIVE");
      d.setTextSize(1); d.setTextColor(0x8410, 0x0000); d.setCursor(4, 24);
      d.print(g_mapStatus.length() ? fitLine(g_mapStatus + " -> /AEGIS/data", 20)
                                   : String((unsigned)g_wifiAtlas.entries.size()) + " saved networks");
      d.setTextColor(0x07FF, 0x0000); d.setCursor(140, 24);
      d.print("kept " + String(g_wardriveStore.kept) + (g_wardriveOn ? " WD ON" : ""));
      if (g_wifiAtlas.entries.empty()) {
        d.setTextColor(0xFFFF, 0x0000); d.setCursor(4, 44);
        d.print("No networks remembered yet.");
        d.setCursor(4, 56); d.print("Run a scan (S) or enable wardrive (W).");
      } else {
        if (g_atlasSel < 0) g_atlasSel = 0;
        if (g_atlasSel >= (int)g_wifiAtlas.entries.size()) g_atlasSel = (int)g_wifiAtlas.entries.size() - 1;
        auto entryAt = [&](int displayIndex) -> const AtlasEntry& {
          return g_wifiAtlas.entries[g_wifiAtlas.entries.size() - 1 - (size_t)displayIndex];
        };
        if (!g_atlasExpanded) {
          const int first = followWindow(g_atlasSel, (int)g_wifiAtlas.entries.size(), 3, g_atlasTop);
          int y = 38;
          for (int shown = 0; shown < 3 && first + shown < (int)g_wifiAtlas.entries.size(); shown++) {
            const int di = first + shown;
            const AtlasEntry& e = entryAt(di);
            const bool selected = di == g_atlasSel;
            if (selected) d.fillRect(0, y - 2, 240, 21, 0x0015);
            d.setTextSize(1);
            d.setTextColor(selected ? 0x07FF : 0xFFFF, selected ? 0x0015 : 0x0000);
            d.setCursor(3, y); d.print((selected ? ">" : " ") + fitLine(e.ssid.length() ? e.ssid : String("(hidden)"), 24));
            d.setTextColor(0x8410, selected ? 0x0015 : 0x0000);
            d.setCursor(5, y + 9);
            String chans;
            for (size_t ci = 0; ci < e.channels.size(); ci++) { if (ci) chans += ","; chans += String(e.channels[ci]); }
            d.print("ch " + chans + "  best " + String(e.bestRssi) + "  x" + String(e.seenCount));
            y += 23;
          }
          scrollHints(first, min((int)g_wifiAtlas.entries.size(), first + 3), (int)g_wifiAtlas.entries.size(), 38, 84);
          drawScrollBar(first, 3, (int)g_wifiAtlas.entries.size(), 38, 84);
        } else {
          const AtlasEntry& e = entryAt(g_atlasSel);
          std::vector<String> lines;
          lines.push_back(e.ssid.length() ? e.ssid : String("(hidden SSID)"));
          lines.push_back("bssid: " + e.bssid);
          String chans;
          for (size_t ci = 0; ci < e.channels.size(); ci++) { if (ci) chans += ","; chans += String(e.channels[ci]); }
          lines.push_back("channels seen: " + chans);
          lines.push_back("last ch " + String(e.lastChannel) + "  best " + String(e.bestRssi) + " dBm");
          lines.push_back("sightings: " + String(e.seenCount));
          lines.push_back(e.lat != 0 || e.lon != 0 ?
              ("best-sighting GPS: " + String(e.lat, 5) + "," + String(e.lon, 5)) :
              String("best-sighting GPS: none (no fix)"));
          lines.push_back("ENTER/BACK returns to list");
          int y = 38;
          for (auto& ln : lines) {
            d.setTextSize(1); d.setTextColor(0xFFFF, 0x0000); d.setCursor(3, y);
            d.print(fitLine(ln, 39)); y += 11;
          }
        }
      }
      break;
    }
    case VIEW_RULES: {
      uiHeader("RULE MANAGER");
      const auto& rules = localRules.all();
      d.setTextSize(1); d.setTextColor(0x8410, 0x0000); d.setCursor(4, 24);
      d.print(String((unsigned)rules.size()) + " rules - ENTER toggles, saved to SD");
      if (rules.empty()) {
        d.setTextColor(0xFFFF, 0x0000); d.setCursor(4, 44);
        d.print("No rules armed. Add packs under /AEGIS/rules");
        d.setCursor(4, 56); d.print("then SETUP > Reload rule packs.");
      } else {
        if (g_ruleSel < 0) g_ruleSel = 0;
        if (g_ruleSel >= (int)rules.size()) g_ruleSel = (int)rules.size() - 1;
        const int first = followWindow(g_ruleSel, (int)rules.size(), 4, g_ruleTop);
        int y = 38;
        for (int i = first; i < min((int)rules.size(), first + 4); i++) {
          const Rule& r = rules[i];
          const bool off = localRules.isDisabled(r.code);
          const bool sel = i == g_ruleSel;
          if (sel) d.fillRect(0, y - 2, 240, 20, 0x0015);
          d.setTextSize(1);
          d.setTextColor(off ? 0x8410 : 0x07E0, sel ? 0x0015 : 0x0000);
          d.setCursor(3, y);
          d.print(fitLine((sel ? ">" : " ") + r.code + (off ? "  [OFF]" : "  [ON]"), 38));
          d.setTextColor(0x8410, sel ? 0x0015 : 0x0000);
          d.setCursor(5, y + 9);
          d.print(fitLine(r.kind + " / " + r.severity + " / " + r.action, 38));
          y += 22;
        }
        scrollHints(first, min((int)rules.size(), first + 4), (int)rules.size(), 38, 104);
        drawScrollBar(first, 4, (int)rules.size(), 38, 104);
      }
      break;
    }
  }
  if (g_linkPromptActive) {
    d.fillRect(10, 40, 220, 56, 0x0015);
    d.drawRect(10, 40, 220, 56, 0x07FF);
    d.setTextSize(1); d.setTextColor(0x07FF, 0x0015); d.setCursor(16, 46);
    d.print("PC LINK REQUEST");
    d.setTextColor(0xFFFF, 0x0015); d.setCursor(16, 58);
    d.print(fitLine("Connect to " + String(g_linkPromptHost) + ":" + String(g_linkPromptPort) + "?", 36));
    d.setTextColor(0x8410, 0x0015); d.setCursor(16, 70);
    d.print("Live status + auto-sync to this PC");
    d.setTextColor(0xFFE0, 0x0015); d.setCursor(16, 82);
    d.print("ENTER connect    ESC not now");
  }
  if (millis() < g_alertUntilMs && g_alertText.length()) {
    d.fillRect(0, 21, 240, 13, 0xF800);
    d.setTextSize(1); d.setTextColor(0xFFFF, 0xF800); d.setCursor(3, 24);
    d.print("ALERT: " + fitLine(g_alertText, 33));
  }
  drawMenuBar();
  uiFooter();
  g_canvas.pushSprite(0, 0);
  g_lastUiDraw = millis();
}
