// AEGIS PHOENIX // Cardputer ADV Blue-Team Sensor Firmware
// -------------------------------------------------------------------
// Phase 0/1: autonomous on-device operation evolved from the PHOENIX agent.
//   * Primary outputs are LOCAL: 240x135 UI + forensic logs on SD (/AEGIS/logs)
//   * The PHOENIX console link (enrol/heartbeat/telemetry) is OPTIONAL and
//     only activates when /AEGIS/config.json (or legacy /phoenix/config.json)
//     supplies a console_url. Detection runs fully offline without it.
//   * Expansion hardware (ESP32-C5, LoRa hat, GNSS hat) is feature-flagged
//     OFF until the EXT-header pin/UART plan is resolved; the historical
//     drivers all contended for G1/G2.
//
// Globals below are the reconstruction of the missing phoenix-agent.ino:
// every symbol agent_core.h references is defined here, before inclusion.

#include <Arduino.h>
#include <M5Cardputer.h>
#include <WiFi.h>
#include <WiFiClientSecure.h>
#include <WiFiUdp.h>
#include <HTTPClient.h>
#include <HTTPUpdate.h>
#include <Update.h>
#include <Preferences.h>
#include <ArduinoJson.h>
#include <NimBLEDevice.h>
#include <esp_wifi.h>
#include <esp_task_wdt.h>
#include <mbedtls/sha256.h>
#include <mbedtls/md.h>
#include <SD.h>
#include <SPI.h>
#include <FS.h>
#include <algorithm>
#include <vector>
#include <map>
#include <time.h>

#define AGENT_VERSION "aegis-0.8.3"
#define FIRMWARE_VER "AEGIS-PHX-v0.12.7-ADV"
#define FW_VER_NUM "0.12.7"

// ---------------------------------------------------------- shared state
static String g_wifiSsid = "";
static String g_wifiPsk = "";
// Saved-network auto-join guard (v0.9.9): while a join attempt is in
// flight, background Wi-Fi scans must not steal the radio - they were
// resetting association attempts every ~20 s so a saved network could
// sit "in range" forever without ever connecting.
static bool g_wifiJoinGuard = false;
static uint32_t g_wifiJoinGuardUntilMs = 0;
static bool g_autoJoinActive = false;
static uint32_t g_autoJoinStartMs = 0, g_autoJoinNextMs = 0;
// Self-update from the repo (v0.10.0): checks the release feed over
// Wi-Fi, downloads to SD, SHA-256-verifies, then flashes - only
// after an on-device ENTER confirm. Feed is anonymous, so the repo
// must be public for this to see anything (404 says so on screen).
static String g_updateFeed = "https://github.com/jackbashroot-boop/AEGIS-Desktop/releases/latest/download/phoenix-latest.json";
static String g_updateStatus = "not checked yet";
static String g_openWifiStatus = "not used yet";
static String g_updateAvailVer = "";
static uint32_t g_updateLastCheckMs = 0;
static bool g_updateChecked = false;
static bool g_csiOn = false;          // menu toggle (persisted "csiOn")
static int g_csiScore = 0;
static bool g_csiPresent = false;
static String g_consoleUrl = "";      // empty = autonomous offline mode
static String g_enrollToken = "";
static String g_hmacSecret = "";
static bool g_tlsInsecure = false;
static String g_vaultPass = "";       // AES-256-GCM vault passphrase (from config)

static String deviceKey = "";
static String deviceId = "";
static int heartbeatSec = 30;

static Preferences prefs;

struct InstalledModule {
  String slug;
  String version;
  String sha256;
};
static std::vector<InstalledModule> installedModules;

struct CommandAck {
  String id;
  bool ok;
  String result;
};
static std::vector<CommandAck> acks;

static std::vector<String> corpSsids;
static std::vector<String> knownBssids;
static std::vector<String> blockedSources;
static bool g_baselineLearned = false;

static bool quarantined = false;
static volatile uint32_t deauthFrames1s = 0;
static volatile uint32_t eapolMsg3Repeats = 0;
static String lastDeauthSource = "";
static uint32_t bootMillis = 0;
static uint32_t g_eventsLogged = 0;
static volatile uint32_t g_deauthFramesTotal = 0;
static String g_lastEventTitle = "";
static bool g_wardriveOn = false;
static uint32_t g_lastKeyActivityMs = 0;
static String g_scanStatus = "idle";
static bool g_scanActive = false;
static uint32_t g_keyboardResyncs = 0;
static bool g_safeMode = false;
static bool g_bootSafeMute = false;  // boot safe mode: scans/hop paused, STA join still allowed (v0.12.2)
static uint32_t g_bootStreak = 0;
static bool g_streakCleared = false;
static std::vector<String> g_lanHosts;
struct LanScanState {
  bool active = false;
  IPAddress base;
  int nextHost = 1;
  uint32_t startedMs = 0;
};
static LanScanState g_lanScan;

static String g_lastEventKind = "";
static uint32_t g_lastEventAt = 0;

// Recent detector events kept in RAM for the expandable EVENTS page.
// The permanent archive remains the encrypted forensic vault on SD; this
// list is deliberately capped so a noisy RF environment cannot exhaust
// the ESP32-S3's internal RAM.
struct AegisEventRecord {
  uint32_t id = 0;
  uint32_t atMs = 0;
  uint32_t epoch = 0;        // NTP seconds when known, else 0
  uint32_t bootId = 0;
  String kind;
  String channel;
  String source;
  String title;
  String fieldsJson;         // this-boot only; older detail lives in the vault
  int confidence = 0;
  bool reviewed = false;
  bool previousBoot = false;
};
static std::vector<AegisEventRecord> g_eventHistory;
static uint32_t g_nextEventId = 1;
static bool g_passiveOffline = false;
static bool g_backgroundScansEnabled = true;
static bool g_liveEnabled = true;   // desktop live link (SYSTEM menu, NVS liveLink)
static bool g_autoSync = true;      // desktop auto-sync of the event index (NVS autoSync)
static int g_liveAps = 0;           // APs in last completed Wi-Fi scan
static bool g_c5HotSwapEnabled = true;
static int g_irSavedBrand = -1;
static int g_menuPrevView = 0;   // where M-menu returns to (VIEW_DASH)
static int g_shadowTyping = 0;   // 0 off, 1 set-new, 2 confirm-new, 3 unlock
static String g_shadowBuf, g_shadowFirst;
static bool shadowCreate(const String& pass);    // defined in agent_core.h
static bool shadowUnlock(const String& pass);
static void shadowLock();
static bool shadowSave();
static int shadowState();
static bool shadowIsUnlocked();
static void shadowSetMsg(const String& m);
// Home WiFi profile (v0.9.2): stored in the SHARED "aegisnet" NVS
// namespace, so every Cardputer family program pulls the same home
// network. Set it here once. Also mirrored into the shadow vault
// (encrypted) whenever the vault is unlocked. Never on SD plaintext.
static String g_netSsidTemp;
// Wi-Fi join attempt (v0.9.8): the candidate is TRIED first and only
// saved when the join actually works, so a typo can no longer poison
// the stored profile. Status strings are shown in SETUP/INTEGRITY.
static String g_homeCandidateSsid, g_homeCandidatePsk;
static String g_homeJoinStatus = "not checked yet";
static bool g_homeJoinActive = false;
static uint8_t g_wifiDiscReason = 0;   // last STA disconnect reason (v0.12.2)
static String wifiDiscWhy() {
  switch (g_wifiDiscReason) {
    case 201: return "network not in range";
    case 202: case 15: case 204: case 2: case 23:
      return "router rejected it - password likely wrong (SETUP > Home WiFi setup)";
    case 200: return "signal lost - too far / weak signal";
    case 0: return "join timed out";
    default: return "router ended it (reason " + String(g_wifiDiscReason) + ")";
  }
}
static uint32_t g_homeJoinStartMs = 0;
// PC link confirmation (v0.9.8): a discovered Companion host must be
// approved on THIS device (ENTER) before live/sync connects to it.
// Approval is remembered per host+port; denial lasts 10 minutes.
static char g_linkPromptHost[48] = "";
static int g_linkPromptPort = 0;
static volatile bool g_linkPromptActive = false;
// Companion-pushed firmware (v0.12.4): the PC offers a verified newer
// image over the live link; ENTER here accepts, then this device pulls
// it from the PC over the LAN, SHA-256-verifies, and flashes itself.
static String g_fwOfferVer, g_fwOfferSha;
static uint32_t g_fwOfferSize = 0;
static uint32_t g_fwOfferAtMs = 0;
static volatile bool g_fwPromptActive = false;
static volatile bool g_fwJobPending = false;
static String g_linkApprovedHost;
static int g_linkApprovedPort = 0;
static String g_linkDeniedHost;
static uint32_t g_linkDeniedUntilMs = 0;
static Preferences g_netPrefs;
static void loadHomeProfile() {
  if (g_netPrefs.begin("aegisnet", true)) {
    const String ssid = g_netPrefs.getString("ssid", "");
    if (ssid.length()) { g_wifiSsid = ssid; g_wifiPsk = g_netPrefs.getString("psk", ""); }
    g_netPrefs.end();
  }
}
static void saveHomeProfile(const String& ssid, const String& psk) {
  if (g_netPrefs.begin("aegisnet", false)) {
    g_netPrefs.putString("ssid", ssid);
    g_netPrefs.putString("psk", psk);
    g_netPrefs.putBool("home", true);
    g_netPrefs.end();
  }
  g_wifiSsid = ssid; g_wifiPsk = psk;
  if (shadowIsUnlocked()) shadowSave();   // encrypted mirror
}
static void startHomeJoinAttempt(const String& ssid, const String& psk) {
  g_homeCandidateSsid = ssid; g_homeCandidatePsk = psk;
  g_homeJoinActive = true; g_homeJoinStartMs = millis();
  g_wifiJoinGuard = true; g_wifiJoinGuardUntilMs = millis() + 20000;
  g_homeJoinStatus = "JOINING " + ssid;
  WiFi.mode(WIFI_STA);
  WiFi.disconnect(false);
  WiFi.begin(ssid.c_str(), psk.c_str());
  shadowSetMsg("Joining " + ssid + " - please wait...");
}
static void startShadowFlow() {
  if (shadowIsUnlocked()) { shadowLock(); return; }
  g_shadowBuf = "";
  g_shadowTyping = shadowState() == 1 ? 3 : 1;
  shadowSetMsg(g_shadowTyping == 1 ? "SET shadow password (8+):" : "shadow password:");
}
static bool g_irFindActive = false;
static int g_irFindBrand = -1;
static uint32_t g_irFindNextMs = 0;
static String g_mapStatus = "";
struct TrackPoint { float lat, lon; uint32_t atMs; };
static std::vector<TrackPoint> g_track;
static int g_rulePackFiles = 0;
static int g_rulePackRejected = 0;
static uint8_t g_brightnessPct = 80;
static uint16_t g_screenSleepSec = 0;   // 0 = never (no surprise "freezes")
static bool g_alertBeeps = true;
static bool g_screenAsleep = false;
static String g_alertText = "";
static uint32_t g_alertUntilMs = 0;
static uint32_t lastHeartbeat = 0;

// Rule engine + detectors + (optional) expansion drivers + SD vault.
#include "board_config.h"
#include "rules.h"
static RuleSet localRules;

#include "key_fsm.h"
#include "adv_guard.h"
static LoopStallWatchdog g_loopWatch;
#include "wifi_atlas.h"
#include "wardrive_store.h"
static WifiAtlas g_wifiAtlas;
static WardriveStore g_wardriveStore;
#include "aes_vault.h"
static AesVault aegisVault;
#include "ir_remote.h"
static IrRemote g_irRemote;

#include "agent_core.h"
#include "integrity.h"
#include "aegis_ui.h"

// ------------------------------------------------- key handling (anti-ghost)
// The ADV keyboard is a 4x14 matrix behind a TCA8418. Chords can ghost, and
// the old handler re-fired on every change event, so holding Q machine-gunned
// esp_wifi_set_promiscuous() + SD writes. Now: one action per physical press,
// chords discarded, and quarantine needs a confirming second press.
static uint32_t g_quarantineArmMs = 0;
static KeyFSM g_keys;
static StuckKeyWatchdog g_kbWatch;
// Logical navigation keys. The ADV's printed arrows are Fn+; , . /
// In the pinned M5Cardputer 1.1.1 library, that layer is exposed as
// fn=true plus the base punctuation in word; the same punctuation keys
// without Fn are accepted too because Aegis has no text fields.
static constexpr char NAV_UP = '\x01';
static constexpr char NAV_LEFT = '\x02';
static constexpr char NAV_DOWN = '\x03';
static constexpr char NAV_RIGHT = '\x04';
static constexpr char NAV_ENTER = '\n';
static constexpr char NAV_BACK = '\x08';
static constexpr char NAV_DELETE = '\x7f';   // physical Backspace/Delete key (text editing)
static constexpr char MENU_PAGE_NET = '\x05';
static constexpr char MENU_RESYNC_KB = '\x06';
static constexpr char MENU_TOGGLE_PASSIVE = '\x07';
static constexpr char MENU_TOGGLE_BGSCAN = '\x0B';
static constexpr char MENU_RECONNECT_WIFI = '\x0C';
static constexpr char MENU_TOGGLE_C5 = '\x0E';
static constexpr char MENU_RELOAD_RULES = '\x0F';
static constexpr char MENU_BRIGHTNESS = '\x11';
static constexpr char MENU_SCREEN_SLEEP = '\x12';
static constexpr char MENU_ALERT_BEEPS = '\x13';
static constexpr char MENU_SHADOW = '\x14';
static constexpr char MENU_CENSUS = '\x15';
static constexpr char MENU_RFID = '\x16';
static char g_repeatKey = 0;
static uint32_t g_repeatNextMs = 0;

static bool isNavKey(char c) {
  return c == NAV_UP || c == NAV_DOWN || c == NAV_LEFT || c == NAV_RIGHT;
}

static void moveMenuSelection(int delta) {
  const int count = aegisMenuCount(g_view);
  int sel = (int)g_menuSel[(int)g_view] + delta;
  sel %= count;
  if (sel < 0) sel += count;
  g_menuSel[(int)g_view] = (uint8_t)sel;
  drawCurrentView();
}

static void changeViewBy(int delta) {
  int v = ((int)g_view + delta) % 12;
  if (v < 0) v += 7;
  g_view = (AegisView)v;
  drawCurrentView();
}

// ---- Async LAN scan -----------------------------------------------------
// The old N handler probed 254 hosts x 6 ports inside the key handler
// (up to 45 s with the keyboard dead). Now it advances one host per
// main-loop pass; Back/N cancels, and results stream into the NET view.
static void startLanScan() {
  if (WiFi.status() != WL_CONNECTED) return;
  g_lanHosts.clear();
  g_netPage = 0;
  g_lanScan.base = WiFi.localIP();
  g_lanScan.nextHost = 1;
  g_lanScan.startedMs = millis();
  g_lanScan.active = true;
  Serial.println("[LAN] async scan started (Back/N cancels)");
}

static void serviceLanScan() {
  if (!g_lanScan.active) return;
  if (WiFi.status() != WL_CONNECTED || g_lanScan.nextHost > 254 ||
      millis() - g_lanScan.startedMs > 60000 || g_lanHosts.size() >= 12) {
    g_lanScan.active = false;
    Serial.printf("[LAN] scan finished: %u hosts\n", (unsigned)g_lanHosts.size());
    drawCurrentView();
    return;
  }
  const uint16_t ports[6] = {80, 443, 22, 53, 8080, 8443};
  const char* names[6] = {"http", "https", "ssh", "dns", "alt-http", "alt-https"};
  const IPAddress me = WiFi.localIP();
  int host = g_lanScan.nextHost++;
  if (host == me[3]) return;
  IPAddress ip(g_lanScan.base[0], g_lanScan.base[1], g_lanScan.base[2], (uint8_t)host);
  for (int pi = 0; pi < 6; pi++) {
    WiFiClient client;
    if (client.connect(ip, ports[pi], 40)) {
      String entry = ip.toString() + " " + names[pi];
      g_lanHosts.push_back(entry);
      if (sdLogMgr.mounted) sdLogMgr.appendLog("net_scan.log", "[LAN] " + entry);
      client.stop();
      break;
    }
    client.stop();
  }
}

// After an unavoidably long synchronous operation (full integrity
// sweep), any keys mashed during it are stale. Resync the keyboard
// reader and settle the FSM so they cannot chain-fire N/T/R next.
static void flushStaleKeys() {
  M5Cardputer.Keyboard.begin();
  g_kbWatch.noteResync();
  g_keys.forceSettle(millis());
  g_repeatKey = 0;
}

static void handleKey(char c);
static void runFirmwareUpdate();
static bool fwVerNewer(const String& a, const String& b);
static void applyStagedUpdate(const String& ver, const String& sha, uint32_t size);
static void runOpenWifi();
static void csiToggle();
static void setPassiveOffline(bool on);
static void activateUnifiedItem(const UnifiedItem& it) {
  if (it.act == 0) { g_view = (AegisView)it.target; drawCurrentView(); return; }
  if (it.act == 'H') {
    g_view = VIEW_MODULES;              // typing prompt lives there
    g_shadowBuf = ""; g_shadowTyping = 4;
    shadowSetMsg("home SSID:");
    drawCurrentView(); return;
  }
  if (it.act == 'M') {
    g_view = VIEW_ATLAS;
    const int n = exportMaps();
    g_mapStatus = n < 0 ? "export failed: no SD" : String("exported ") + n + " GPS nets";
    drawCurrentView(); return;
  }
  switch (it.act) {
    case 's': case 'n': case 'r': case 'w': case 't':
      handleKey((char)it.act); return;   // existing handlers (scan/lan/lora/wardrive/integrity)
    case 'D':
      g_autoSync = !g_autoSync;
      prefs.putBool("autoSync", g_autoSync);
      if (sdLogMgr.mounted) sdLogMgr.appendLog("aegis_boot.log", String("[MENU] autoSync=") + (g_autoSync ? "on" : "off"));
      drawCurrentView(); return;
    case 'L':
      g_liveEnabled = !g_liveEnabled;
      prefs.putBool("liveLink", g_liveEnabled);
      if (sdLogMgr.mounted) sdLogMgr.appendLog("aegis_boot.log", String("[MENU] liveLink=") + (g_liveEnabled ? "on" : "off"));
      drawCurrentView(); return;
    case 'B':
      g_backgroundScansEnabled = !g_backgroundScansEnabled;
      prefs.putBool("bgScans", g_backgroundScansEnabled);
      if (!g_backgroundScansEnabled) g_scanStatus = "background scans off";
      drawCurrentView(); return;
    case 'O': setPassiveOffline(!g_passiveOffline); drawCurrentView(); return;
    case 'C':
      g_c5HotSwapEnabled = !g_c5HotSwapEnabled;
      prefs.putBool("c5HotSwap", g_c5HotSwapEnabled);
#if AEGIS_ENABLE_C5
      if (g_c5HotSwapEnabled) { if (!g_safeMode) esp32c5.beginHotSwap(AEGIS_C5_RX_PIN, AEGIS_C5_TX_PIN, AEGIS_C5_BAUD); }
      else esp32c5.endHotSwap();
#endif
      drawCurrentView(); return;
    case 'i':
      if (g_wifiSsid.length()) {
        g_autoJoinActive = false; g_autoJoinNextMs = 0;   // homeNetTick starts the join this tick
        g_homeJoinStatus = "JOINING " + g_wifiSsid + " ...";
      } else {
        g_homeJoinStatus = "no saved WiFi yet (WIFI > Home WiFi setup)";
      }
      g_view = VIEW_SETUP; drawCurrentView(); return;
    case 'y': runFirmwareUpdate(); drawCurrentView(); return;
    case 'o': runOpenWifi(); drawCurrentView(); return;
    case 'e': csiToggle(); drawCurrentView(); return;
    case 'J':
      g_chanHop = !g_chanHop;
      prefs.putBool("chanHop", g_chanHop);
      drawCurrentView(); return;
    case 'U': g_view = VIEW_MODULES; startShadowFlow(); drawCurrentView(); return;
    case 'F':
      rfid2.enabled = !rfid2.enabled;
      prefs.putBool("rfidOn", rfid2.enabled);
      if (rfid2.enabled) rfid2.begin();
      drawCurrentView(); return;
    case 'Q': {
      // same two-press arm/confirm logic as the Q key
      if (millis() - g_quarantineArmMs > 3000) {
        g_quarantineArmMs = millis();
        screen("QUARANTINE?", "ENTER again (select item) to mute radios", TFT_YELLOW);
      } else {
        g_quarantineArmMs = 0;
        quarantined = !quarantined;
        if (quarantined) {
          esp_wifi_set_promiscuous(false);
          NimBLEDevice::getScan()->stop();
          esp32c5.setSniffing(false);
        } else {
          esp_wifi_set_promiscuous(true);
          if (esp32c5.active) esp32c5.setSniffing(true);
        }
        if (sdLogMgr.mounted) sdLogMgr.appendLog("aegis_boot.log", String("[MENU] quarantine=") + (quarantined ? "on" : "off"));
        drawCurrentView();
      }
      return; }
    case 'G':
      g_brightnessPct = g_brightnessPct >= 100 ? 20 : (uint8_t)(g_brightnessPct + 20);
      prefs.putUInt("bright", g_brightnessPct);
      M5Cardputer.Display.setBrightness((uint8_t)(g_brightnessPct * 255 / 100));
      drawCurrentView(); return;
    case 'Z':
      g_screenSleepSec = g_screenSleepSec == 0 ? 30 : g_screenSleepSec == 30 ? 60 :
                         g_screenSleepSec == 60 ? 120 : 0;
      prefs.putUInt("sleepSec", g_screenSleepSec);
      drawCurrentView(); return;
    case 'Y': loadSdRulePacks(); loadDisabledRules(); drawCurrentView(); return;
    default: return;
  }
}

static void setPassiveOffline(bool on) {
  g_passiveOffline = on;
  prefs.putBool("passiveOff", on);
  if (on) {
    // Disconnect the station association but leave the STA radio powered:
    // promiscuous reception and passive channel scans continue.
    WiFi.disconnect(false, false);
    g_scanStatus = "passive offline";
    Serial.println("[SETUP] passive offline ON");
  } else {
    g_scanStatus = "WiFi reconnecting";
    if (g_wifiSsid.length()) WiFi.begin(g_wifiSsid.c_str(), g_wifiPsk.c_str());
    Serial.println("[SETUP] passive offline OFF");
  }
  if (sdLogMgr.mounted) {
    sdLogMgr.appendLog("aegis_boot.log", String("[SETUP] passiveOffline=") + (on ? "1" : "0"));
  }
}

static void activateMenuSelection() {
  const AegisMenuItem item = aegisMenuItem(g_view);
  if (item.action == MENU_PAGE_NET) {
    if (!g_lanHosts.empty()) g_netPage++;
    drawCurrentView();
    return;
  }
  if (item.action == MENU_TOGGLE_PASSIVE) {
    setPassiveOffline(!g_passiveOffline);
    drawCurrentView();
    return;
  }
  if (item.action == MENU_TOGGLE_BGSCAN) {
    g_backgroundScansEnabled = !g_backgroundScansEnabled;
    prefs.putBool("bgScans", g_backgroundScansEnabled);
    if (!g_backgroundScansEnabled) g_scanStatus = "background scans off";
    drawCurrentView();
    return;
  }
  if (item.action == MENU_RELOAD_RULES) {
    loadSdRulePacks();
    loadDisabledRules();
    drawCurrentView();
    return;
  }
  if (item.action == MENU_BRIGHTNESS) {
    g_brightnessPct = g_brightnessPct >= 100 ? 20 : (uint8_t)(g_brightnessPct + 20);
    prefs.putUInt("bright", g_brightnessPct);
    M5Cardputer.Display.setBrightness((uint8_t)(g_brightnessPct * 255 / 100));
    drawCurrentView();
    return;
  }
  if (item.action == MENU_SCREEN_SLEEP) {
    g_screenSleepSec = g_screenSleepSec == 0 ? 30 : g_screenSleepSec == 30 ? 60 :
                       g_screenSleepSec == 60 ? 120 : 0;
    prefs.putUInt("sleepSec", g_screenSleepSec);
    drawCurrentView();
    return;
  }
  if (item.action == MENU_TOGGLE_C5) {
    g_c5HotSwapEnabled = !g_c5HotSwapEnabled;
    prefs.putBool("c5HotSwap", g_c5HotSwapEnabled);
#if AEGIS_ENABLE_C5
    if (g_c5HotSwapEnabled) {
      if (!g_safeMode) esp32c5.beginHotSwap(AEGIS_C5_RX_PIN, AEGIS_C5_TX_PIN, AEGIS_C5_BAUD);
    } else {
      esp32c5.endHotSwap();
    }
#endif
    drawCurrentView();
    return;
  }
  if (item.action == MENU_RECONNECT_WIFI) {
    if (g_passiveOffline) setPassiveOffline(false);
    else if (g_wifiSsid.length()) WiFi.begin(g_wifiSsid.c_str(), g_wifiPsk.c_str());
    drawCurrentView();
    return;
  }
  if (item.action == MENU_RESYNC_KB) {
    Serial.println("[KB] manual keyboard resync from DIAG menu");
    M5Cardputer.Keyboard.begin();
    g_kbWatch.noteResync();
    g_keys.forceSettle(millis());
    g_keyboardResyncs++;
    drawCurrentView();
    return;
  }
  if (item.action == MENU_ALERT_BEEPS) {
    g_alertBeeps = !g_alertBeeps;
    prefs.putBool("alertBeep", g_alertBeeps);
    drawCurrentView();
    return;
  }
  if (item.action == MENU_SHADOW) { g_view = VIEW_MODULES; startShadowFlow(); drawCurrentView(); return; }
  if (item.action == MENU_CENSUS) {
    g_view = VIEW_MODULES;
    g_gnssOff = false; g_c5Off = false; g_censusDone = false;
    g_censusStartMs = millis();
    if (!loraHat.active) loraHat.begin();
    drawCurrentView();
    return;
  }
  if (item.action == MENU_RFID) {
    g_view = VIEW_MODULES;
    rfid2.enabled = !rfid2.enabled;
    prefs.putBool("rfidOn", rfid2.enabled);
    if (rfid2.enabled) rfid2.begin();
    drawCurrentView();
    return;
  }
  if (item.target >= 0) g_view = (AegisView)item.target;
  if (item.action) handleKey(item.action);
  else drawCurrentView();
}


// Custom binds persistence: prefs "binds" as "key:item;key:item".
static void saveBinds() {
  String v;
  for (int i = 0; i < g_bindCount; i++) {
    if (i) v += ";";
    v += g_bindKey[i]; v += ":"; v += String((int)g_bindItem[i]);
  }
  prefs.putString("binds", v);
}
static void loadBinds() {
  g_bindCount = 0;
  const String v = prefs.getString("binds", "");
  int start = 0;
  while (start < (int)v.length() && g_bindCount < 24) {
    int semi = v.indexOf(';', start);
    if (semi < 0) semi = v.length();
    const String tok = v.substring(start, semi);
    const int colon = tok.indexOf(':');
    if (tok.length() >= 3 && colon == 1) {
      const int item = tok.substring(2).toInt();
      if (item < 0 || item >= (int)(sizeof(kUnifiedMenu) / sizeof(kUnifiedMenu[0]))) continue;
      if (tok[0] == 'm' || tok[0] == 'M' || tok[0] == '`') continue;   // reserved keys
      g_bindKey[g_bindCount] = tok[0];
      g_bindItem[g_bindCount] = (uint8_t)item;
      g_bindCount++;
    }
    start = semi + 1;
  }
}
static void assignBind(char key, int item) {
  for (int i = 0; i < g_bindCount; i++)           // key moves to one item only
    if (g_bindKey[i] == key) {                   // steal: remove old holder
      for (int j = i + 1; j < g_bindCount; j++) { g_bindKey[j-1] = g_bindKey[j]; g_bindItem[j-1] = g_bindItem[j]; }
      g_bindCount--; i--;
    }
  for (int i = 0; i < g_bindCount; i++)
    if (g_bindItem[i] == item) { g_bindKey[i] = key; saveBinds(); return; }
  if (g_bindCount < 24) { g_bindKey[g_bindCount] = key; g_bindItem[g_bindCount] = (uint8_t)item; g_bindCount++; }
  saveBinds();
}
static void clearBind(int item) {
  for (int i = 0; i < g_bindCount; i++)
    if (g_bindItem[i] == item) {
      for (int j = i + 1; j < g_bindCount; j++) { g_bindKey[j-1] = g_bindKey[j]; g_bindItem[j-1] = g_bindItem[j]; }
      g_bindCount--;
    }
  saveBinds();
}

// Physical-key gate (v0.9.0): the ONLY entry point for keyboard input.
// Menu / arrows / Enter / Esc / Backspace always work; letters and
// digits do NOTHING unless the user bound them (Key binds screen).
// Internal dispatch (menus, binds themselves) calls handleKey directly.
static void userKey(char c) {
  if (c == NAV_DELETE && !g_shadowTyping) c = NAV_BACK;   // outside text entry, Backspace = back
  if (g_view == VIEW_MENU || g_view == VIEW_BINDS || g_shadowTyping) { handleKey(c); return; }
  if (isNavKey(c) || c == NAV_ENTER || c == NAV_BACK || c == '\t' || c == 'm' || c == 'M') { handleKey(c); return; }
  if (c >= '!' && c <= '~') {
    for (int i = 0; i < g_bindCount; i++)
      if (g_bindKey[i] == c) { activateUnifiedItem(kUnifiedMenu[g_bindItem[i]]); return; }
    return;   // unbound: deliberately inert
  }
  handleKey(c);
}

static void handleKey(char c) {
  if (g_fwPromptActive && !g_shadowTyping) {
    if (c == NAV_ENTER) {
      g_fwPromptActive = false;
      g_fwJobPending = true;   // loop() runs the download+flash modally
    } else if (c == NAV_BACK) {
      g_fwPromptActive = false;
      g_updateStatus = "PC update postponed";
    }
    drawCurrentView(); return;   // the prompt swallows every other key
  }
  if (g_linkPromptActive) {
    if (c == NAV_ENTER) {
      g_linkApprovedHost = g_linkPromptHost; g_linkApprovedPort = g_linkPromptPort;
      prefs.begin("phoenix", false);
      prefs.putString("pcHost", g_linkApprovedHost); prefs.putInt("pcPort", g_linkApprovedPort);
      prefs.end();
      g_linkPromptActive = false;
      if (sdLogMgr.mounted) sdLogMgr.appendLog("aegis_boot.log", "[LINK] PC approved by user: " + g_linkApprovedHost);
    } else if (c == NAV_BACK) {
      g_linkDeniedHost = g_linkPromptHost; g_linkDeniedUntilMs = millis() + 600000;
      g_linkPromptActive = false;
    }
    drawCurrentView(); return;   // the prompt swallows every other key
  }
  if (g_lanScan.active && (c == NAV_BACK || c == 'n' || c == 'N')) {
    g_lanScan.active = false;
    Serial.println("[LAN] scan cancelled by user");
    drawCurrentView();
    return;
  }

  // EVENTS owns the arrows while browsing: Up/Down chooses or scrolls,
  // Left/Right pages, F cycles the type filter, Enter expands and, in
  // the expanded view, Enter marks the event reviewed (persisted).
  if (g_view == VIEW_EVENTS) {
    const std::vector<int> vis = visibleEventIndices();
    if (vis.empty()) {
      if (c == NAV_LEFT) { changeViewBy(-1); return; }
      if (c == NAV_RIGHT) { changeViewBy(1); return; }
      if (c == NAV_BACK) { g_view = VIEW_DASH; drawCurrentView(); return; }
      if (isNavKey(c) || c == NAV_ENTER || c == '\t') return;
    } else {
      if (g_eventSel < 0) g_eventSel = 0;
      if (g_eventSel >= (int)vis.size()) g_eventSel = (int)vis.size() - 1;
      if (g_eventExpanded) {
        if (c == NAV_UP) { g_eventDetailScroll--; drawCurrentView(); return; }
        if (c == NAV_DOWN || c == '\t') { g_eventDetailScroll++; drawCurrentView(); return; }
        if (c == NAV_LEFT) { g_eventDetailScroll -= 5; drawCurrentView(); return; }
        if (c == NAV_RIGHT) { g_eventDetailScroll += 5; drawCurrentView(); return; }
        if (c == NAV_ENTER) {
          AegisEventRecord& rec = g_eventHistory[vis[g_eventSel]];
          rec.reviewed = !rec.reviewed;
          saveEventIndex();
          drawCurrentView(); return;
        }
        if (c == NAV_BACK) { g_eventExpanded = false; drawCurrentView(); return; }
        return;
      }
      if (c == NAV_UP) { if (g_eventSel > 0) g_eventSel--; drawCurrentView(); return; }
      if (c == NAV_DOWN || c == '\t') {
        if (g_eventSel + 1 < (int)vis.size()) g_eventSel++;
        drawCurrentView(); return;
      }
      if (c == NAV_LEFT) { g_eventFilter = (g_eventFilter + 4) % 5; g_eventSel = 0; drawCurrentView(); return; }
      if (c == NAV_RIGHT) { g_eventFilter = (g_eventFilter + 1) % 5; g_eventSel = 0; drawCurrentView(); return; }
      if (c == NAV_ENTER) { g_eventExpanded = true; g_eventDetailScroll = 0; drawCurrentView(); return; }
      if (c == NAV_BACK) { g_view = VIEW_DASH; drawCurrentView(); return; }
      return;
    }
  }

  // LOGS owns Up/Down (scroll), F cycles the file.
  if (g_view == VIEW_LOGS) {
    if (c == NAV_UP) { g_logScroll++; drawCurrentView(); return; }
    if (c == NAV_DOWN || c == '\t') { if (g_logScroll > 0) g_logScroll--; drawCurrentView(); return; }
    if (c == NAV_LEFT) { g_logFileSel = (g_logFileSel + 3) % 4; g_logScroll = 0; drawCurrentView(); return; }
    if (c == NAV_RIGHT) { g_logFileSel = (g_logFileSel + 1) % 4; g_logScroll = 0; drawCurrentView(); return; }
    if (c == NAV_BACK) { g_view = VIEW_DASH; drawCurrentView(); return; }
    if (isNavKey(c) || c == NAV_ENTER) return;
  }

  // ATLAS owns Up/Down (select), Left/Right (page), Enter (expand).
  if (g_view == VIEW_ATLAS) {
    const int n = (int)g_wifiAtlas.entries.size();
    if (c == NAV_UP) { if (g_atlasSel > 0) g_atlasSel--; drawCurrentView(); return; }
    if (c == NAV_DOWN || c == '\t') { if (g_atlasSel + 1 < n) g_atlasSel++; drawCurrentView(); return; }
    if (c == NAV_LEFT) {
      if (g_atlasExpanded) { g_atlasExpanded = false; drawCurrentView(); }
      else { g_atlasSel = max(0, g_atlasSel - 3); drawCurrentView(); }
      return;
    }
    if (c == NAV_RIGHT) {
      if (!g_atlasExpanded) g_atlasSel = min(max(0, n - 1), g_atlasSel + 3);
      drawCurrentView(); return;
    }
    if (c == NAV_ENTER) { g_atlasExpanded = !g_atlasExpanded; drawCurrentView(); return; }
    if (c == NAV_BACK) {
      if (g_atlasExpanded) g_atlasExpanded = false; else g_view = VIEW_NET;
      drawCurrentView(); return;
    }
    if (isNavKey(c)) return;
  }

  // RULE MANAGER owns Up/Down; Enter toggles the rule on/off on SD.
  if (g_view == VIEW_RULES) {
    const int n = (int)localRules.all().size();
    if (c == NAV_UP) { if (g_ruleSel > 0) g_ruleSel--; drawCurrentView(); return; }
    if (c == NAV_DOWN || c == '\t') { if (g_ruleSel + 1 < n) g_ruleSel++; drawCurrentView(); return; }
    if (c == NAV_LEFT) { g_ruleSel = max(0, g_ruleSel - 4); drawCurrentView(); return; }
    if (c == NAV_RIGHT) { g_ruleSel = min(max(0, n - 1), g_ruleSel + 4); drawCurrentView(); return; }
    if (c == NAV_ENTER && n > 0) {
      const String code = localRules.all()[g_ruleSel].code;
      localRules.setDisabled(code, !localRules.isDisabled(code));
      saveDisabledRules();
      drawCurrentView(); return;
    }
    if (c == NAV_BACK) { g_view = VIEW_MODULES; drawCurrentView(); return; }
    if (isNavKey(c)) return;
  }

  // MODULES (sensors) screen: R toggles the optional RFID2 reader.
  // Default OFF; when ON with no reader attached the row says so.
  if (g_shadowTyping) {
    // Password entry owns the keyboard until ENTER/Esc.
    if (c == NAV_ENTER) {
      if (g_shadowTyping == 1) {
        if (g_shadowBuf.length() < 8) { shadowSetMsg("too short (8+):"); g_shadowBuf = ""; }
        else { g_shadowFirst = g_shadowBuf; g_shadowBuf = ""; g_shadowTyping = 2; shadowSetMsg("repeat password:"); }
      } else if (g_shadowTyping == 2) {
        if (g_shadowBuf == g_shadowFirst) { shadowCreate(g_shadowBuf); g_shadowTyping = 0; }
        else { g_shadowTyping = 1; shadowSetMsg("mismatch - SET password (8+):"); }
        g_shadowBuf = "";
      } else if (g_shadowTyping == 4) {
        if (!g_shadowBuf.length()) { shadowSetMsg("type the SSID first (ESC cancels):"); }
        else { g_netSsidTemp = g_shadowBuf; g_shadowBuf = ""; g_shadowTyping = 5; shadowSetMsg("home WiFi password:"); }
      } else if (g_shadowTyping == 5) {
        const String cand = g_shadowBuf;
        g_shadowTyping = 0; g_shadowBuf = "";
        startHomeJoinAttempt(g_netSsidTemp, cand);   // saved only if it connects
      } else {
        shadowUnlock(g_shadowBuf);
        g_shadowTyping = 0; g_shadowBuf = "";
      }
    } else if (c == NAV_DELETE) {
      if (g_shadowBuf.length()) g_shadowBuf.remove(g_shadowBuf.length() - 1);   // Backspace edits
    } else if (c == NAV_BACK) {
      g_shadowTyping = 0; g_shadowBuf = ""; shadowSetMsg("cancelled");
    } else if (c >= 32 && c <= 126 && g_shadowBuf.length() < 48) {
      g_shadowBuf += (char)c;   // spaces allowed: SSIDs/passwords can contain them
    }
    drawCurrentView(); return;
  }

  // KEY BINDS: arrows choose an action, ENTER captures the next key,
  // LEFT clears. Typing here is binding, not triggering.
  if (g_view == VIEW_BINDS) {
    const int n = (int)(sizeof(kUnifiedMenu) / sizeof(kUnifiedMenu[0]));
    if (g_bindCapture) {
      if (c == NAV_BACK) g_bindCapture = false;
      else if (c >= '!' && c <= '~' && !isNavKey(c) && c != 'm' && c != 'M' && c != '`') {
        assignBind(c, g_bindSel); g_bindCapture = false;
      }
      drawCurrentView(); return;
    }
    if (c == NAV_UP) { g_bindSel = (g_bindSel + n - 1) % n; drawCurrentView(); return; }
    if (c == NAV_DOWN || c == '\t') { g_bindSel = (g_bindSel + 1) % n; drawCurrentView(); return; }
    if (c == NAV_LEFT) { clearBind(g_bindSel); drawCurrentView(); return; }
    if (c == NAV_ENTER) { g_bindCapture = true; drawCurrentView(); return; }
    if (c == NAV_BACK) { g_view = VIEW_MENU; drawCurrentView(); return; }
    return;
  }

  // Unified MENU (M opens from anywhere; Esc/M returns). One menu for
  // the whole program, items grouped with similar actions adjacent.
  if (g_view == VIEW_MENU) {
    if (g_menuCat < 0) {
      const int cats = menuCatCount();
      if (c == NAV_UP) { g_menuCatSel = (g_menuCatSel + cats - 1) % cats; drawCurrentView(); return; }
      if (c == NAV_DOWN || c == '\t') { g_menuCatSel = (g_menuCatSel + 1) % cats; drawCurrentView(); return; }
      if (c == NAV_ENTER) { g_menuCat = g_menuCatSel; g_menuListSel = menuCatFirst(g_menuCat); drawCurrentView(); return; }
      if (c == NAV_BACK || c == 'm' || c == 'M') { g_view = (AegisView)g_menuPrevView; drawCurrentView(); return; }
      return;
    }
    const int first0 = menuCatFirst(g_menuCat);
    const int cnt = menuCatItems(g_menuCat);
    if (g_menuListSel < first0 || g_menuListSel >= first0 + cnt) g_menuListSel = first0;
    if (c == NAV_UP) { g_menuListSel = first0 + (g_menuListSel - first0 + cnt - 1) % cnt; drawCurrentView(); return; }
    if (c == NAV_DOWN || c == '\t') { g_menuListSel = first0 + (g_menuListSel - first0 + 1) % cnt; drawCurrentView(); return; }
    if (c == NAV_ENTER) { activateUnifiedItem(kUnifiedMenu[g_menuListSel]); return; }
    if (c == NAV_BACK) { g_menuCat = -1; drawCurrentView(); return; }
    if (c == 'm' || c == 'M') { g_view = (AegisView)g_menuPrevView; drawCurrentView(); return; }
    return;   // menu swallows other keys (no accidental global actions)
  }
  if (c == 'm' || c == 'M') {
    g_menuPrevView = (int)g_view; g_view = VIEW_MENU; g_menuCat = -1; drawCurrentView(); return;
  }

  // REMOTE owns Up/Down for TV buttons. Enter transmits through G44;
  // L arms raw learning on the optional external receiver at G1.
  if (g_view == VIEW_REMOTE) {
    if (g_irFindActive) {
      // Auto-find owns the keys while it cycles (this block used to sit
      // below Up/Down navigation, so "skip" silently changed button).
      if (c == NAV_ENTER) {
        if (g_irFindBrand >= 0) {
          g_irSavedBrand = g_irFindBrand;
          prefs.putInt("irBrand", g_irSavedBrand);
          g_irRemote.status = String("saved: ") + IrRemote::brandAt(g_irSavedBrand)->name +
                              " - ENTER sends, A rescans";
        }
        g_irFindActive = false;
        drawCurrentView(); return;
      }
      if (c == NAV_DOWN) { g_irFindNextMs = 0; drawCurrentView(); return; }  // next brand now
      if (c == NAV_UP) { g_irFindBrand = max(-1, g_irFindBrand - 2); g_irFindNextMs = 0; drawCurrentView(); return; }
      if (c == NAV_BACK) {
        g_irFindActive = false;
        g_irRemote.loadProfile();
        if (!g_irRemote.profileLoaded && g_irSavedBrand >= 0) g_irRemote.applyBrand(g_irSavedBrand);
        g_irRemote.status = "auto-find cancelled";
        drawCurrentView(); return;
      }
      return;   // swallow everything else while finding
    }
    const int total = IrRemote::kButtonCount + 1;   // buttons + ACTION row
    if (c == NAV_UP) {
      g_remoteSel = (g_remoteSel + total - 1) % total;
      drawCurrentView(); return;
    }
    if (c == NAV_DOWN || c == '\t') {
      g_remoteSel = (g_remoteSel + 1) % total;
      drawCurrentView(); return;
    }
    if (c == NAV_LEFT || c == NAV_RIGHT) {
      if (g_remoteSel >= IrRemote::kButtonCount) {
        g_remoteAction = (g_remoteAction + 1) % 2;
      } else {
        g_irSavedBrand = (g_irSavedBrand + (c == NAV_RIGHT ? 1 : IrRemote::kBrandCount - 1)) % IrRemote::kBrandCount;
        prefs.putInt("irBrand", g_irSavedBrand);
        g_irRemote.applyBrand(g_irSavedBrand);
      }
      drawCurrentView(); return;
    }
    if (c == NAV_ENTER) {
      if (g_remoteSel < IrRemote::kButtonCount) { g_irRemote.sendButton(g_remoteSel); }
      else if (g_remoteAction == 0) {
        g_irFindActive = true;
        g_irFindBrand = -1;
        g_irFindNextMs = 0;
        g_irRemote.status = "auto-find: watch the TV, ENTER when it reacts";
      } else {
        if (g_c5HotSwapEnabled) g_irRemote.status = "IR RX G1 reserved for C5 - disable C5 in SETUP";
        else g_irRemote.startLearning(min(g_remoteSel, IrRemote::kButtonCount - 1));
      }
      drawCurrentView(); return;
    }
    if (c == NAV_BACK) { g_view = VIEW_DASH; drawCurrentView(); return; }
    return;
  }

  if (c == NAV_UP) { moveMenuSelection(-1); return; }
  if (c == NAV_DOWN || c == '\t') { moveMenuSelection(1); return; }
  if (c == NAV_LEFT) { changeViewBy(-1); return; }
  if (c == NAV_RIGHT) { changeViewBy(1); return; }
  if (c == NAV_ENTER) { activateMenuSelection(); return; }
  if (c == NAV_BACK) { g_view = VIEW_DASH; drawCurrentView(); return; }
  switch (c) {
    case 's': case 'S':
      requestBackgroundScan();
      g_scanStatus = "scan queued";
      drawCurrentView();
      break;
    case 'b': case 'B':
      if (g_view == VIEW_SETUP) {
        g_alertBeeps = !g_alertBeeps;
        prefs.putBool("alertBeep", g_alertBeeps);
        drawCurrentView();
      }
      break;
    case 'p': case 'P':
      g_view = VIEW_SETUP;
      setPassiveOffline(!g_passiveOffline);
      drawCurrentView();
      break;
    case 'l': case 'L':
      g_view = VIEW_REMOTE;
      drawCurrentView();
      break;
    case 'w': case 'W':
      g_wardriveOn = !g_wardriveOn;
      if (!g_wardriveOn) exportMaps();   // fresh map files ready to sync/import
      if (sdLogMgr.mounted) sdLogMgr.appendLog("aegis_boot.log", String("[KEY] wardrive=") + (g_wardriveOn ? "on" : "off"));
      drawCurrentView();
      break;
    case 'n': case 'N':
      g_view = VIEW_NET;
      startLanScan();   // async; serviceLanScan() advances it per loop
      drawCurrentView();
      break;
    case 'r': case 'R':
      if (loraHat.active) loraHat.scanActivity();   // stepped, non-blocking
      g_view = VIEW_CAP;
      drawCurrentView();
      break;
    case 't': case 'T':
      g_view = VIEW_DIAG;
      g_loopWatch.expectStall();
      screen("integrity sweep...", "hashing SD bins + parser fuzz", TFT_CYAN);
      runIntegritySweep(true);
      flushStaleKeys();
      drawCurrentView();
      break;
    case 'q': case 'Q':
      if (millis() - g_quarantineArmMs > 3000) {
        g_quarantineArmMs = millis();
        screen("QUARANTINE?", "press Q again to mute radios", TFT_YELLOW);
        if (sdLogMgr.mounted) sdLogMgr.appendLog("aegis_boot.log", "[KEY] quarantine arm (awaiting confirm)");
      } else {
        g_quarantineArmMs = 0;
        quarantined = !quarantined;
        if (quarantined) {
          esp_wifi_set_promiscuous(false);
          NimBLEDevice::getScan()->stop();
          esp32c5.setSniffing(false);
        } else {
          esp_wifi_set_promiscuous(true);
          if (esp32c5.active) esp32c5.setSniffing(true);
        }
        if (sdLogMgr.mounted) sdLogMgr.appendLog("aegis_boot.log", String("[KEY] quarantine=") + (quarantined ? "on" : "off"));
        drawCurrentView();
      }
      break;
    default:
      break;
  }
}

// Translate M5 KeysState into the single logical key stream consumed by
// KeyFSM. Fn-layer arrows/specials set booleans and leave word empty, so
// reading word alone (the old code) made the printed arrows unusable.
static void readLogicalKeyboard(bool& pressed, int& wordCount, char& onlyKey, String& signature) {
  const auto st = M5Cardputer.Keyboard.keysState();
  pressed = false;
  wordCount = 0;
  onlyKey = 0;
  signature = "";

  // M5Cardputer 1.1.1 does not materialise an Fn layer in KeysState:
  // Fn+; is fn=true with ';' in word, so the punctuation mapping below
  // handles both the printed Fn arrows and the plain cluster keys.
  if (st.enter) { pressed = true; wordCount = 1; onlyKey = NAV_ENTER; signature = "ENTER"; return; }
  if (st.del) { pressed = true; wordCount = 1; onlyKey = NAV_DELETE; signature = "BACK"; return; }
  if (st.tab) { pressed = true; wordCount = 1; onlyKey = '\t'; signature = "TAB"; return; }

  wordCount = (int)st.word.size();
  pressed = wordCount > 0;
  if (!pressed) return;
  for (char c : st.word) signature += c;
  if (wordCount != 1) return;
  const char c = st.word[0];
  if (st.fn && (c == '`' || c == '~')) { onlyKey = NAV_BACK; signature = "ESC"; return; }
  // Printed arrow cluster, with or without Fn. Shifted forms are included
  // because KeysState.word reports the shifted punctuation character.
  if (c == ';' || c == ':') { onlyKey = NAV_UP; signature = "UP"; }
  else if (c == ',' || c == '<') { onlyKey = NAV_LEFT; signature = "LEFT"; }
  else if (c == '.' || c == '>') { onlyKey = NAV_DOWN; signature = "DOWN"; }
  else if (c == '/' || c == '?') { onlyKey = NAV_RIGHT; signature = "RIGHT"; }
  else onlyKey = c;
}

// ------------------------------------------------------------- lifecycle
// ------------------------------------------------------- desktop live link
// v0.9.3: streams live status to AEGIS COMPANION while joined to the
// home Wi-Fi, so the desktop dashboard shows this device scanning /
// wardriving in near-real time. Config: /AEGIS_LINK/desktop.txt
// (host=, port=, token=) - the file the desktop writes to the SD card.
// Auth is the family HMAC-SHA256 challenge-response; the token never
// crosses the wire. Frames are one-way {"type":"live",...} lines at a
// ~2 s cadence from a low-priority task on core 0: scanning always
// wins the radio, this just rides along and reconnects quietly.
static String g_deskHost, g_deskToken;
static int g_deskPort = 8791;
static bool g_liveLinked = false;
static uint32_t g_lastDiscoverMs = 0;
static void saveDesktopCfg() {
  if (!sdLogMgr.mounted) return;
  if (!SD.exists("/AEGIS_LINK")) SD.mkdir("/AEGIS_LINK");
  File f = SD.open("/AEGIS_LINK/desktop.txt", FILE_WRITE);
  if (!f) return;
  f.print("host=" + g_deskHost + "\nport=" + String(g_deskPort) + "\ntoken=" + g_deskToken + "\n");
  f.close();
}
static void loadDesktopCfg() {
  if (!sdLogMgr.mounted) return;
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
static String liveHmacHex(const String& key, const String& msg) {
  const mbedtls_md_info_t* info = mbedtls_md_info_from_type(MBEDTLS_MD_SHA256);
  mbedtls_md_context_t ctx; mbedtls_md_init(&ctx);
  if (mbedtls_md_setup(&ctx, info, 1) != 0) { mbedtls_md_free(&ctx); return ""; }
  mbedtls_md_hmac_starts(&ctx, (const uint8_t*)key.c_str(), key.length());
  mbedtls_md_hmac_update(&ctx, (const uint8_t*)msg.c_str(), msg.length());
  uint8_t out[32]; mbedtls_md_hmac_finish(&ctx, out); mbedtls_md_free(&ctx);
  char hex[65]; for (int i = 0; i < 32; i++) snprintf(hex + i * 2, 3, "%02x", out[i]);
  return String(hex);
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
        if (port > 0 && macT == liveHmacHex(g_deskToken, "AEGIS_DISCOVER:" + String(nonce))) {
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
static bool isLinkApproved(const String& host, int port) {
  return host.length() && host == g_linkApprovedHost && port == g_linkApprovedPort;
}
static void linkPromptTick() {
  if (g_linkPromptActive || g_shadowTyping || g_homeJoinActive || g_passiveOffline || quarantined) return;
  if (!(g_liveEnabled || g_autoSync) || WiFi.status() != WL_CONNECTED) return;
  if (!g_deskHost.length()) { loadDesktopCfg(); if (!g_deskHost.length()) return; }
  if (isLinkApproved(g_deskHost, g_deskPort)) return;
  if (g_deskHost == g_linkDeniedHost && (int32_t)(g_linkDeniedUntilMs - millis()) > 0) return;
  strlcpy(g_linkPromptHost, g_deskHost.c_str(), sizeof(g_linkPromptHost));
  g_linkPromptPort = g_deskPort;
  g_linkPromptActive = true;
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
struct LiveSnap { char state[16]; char detail[80]; char last[24]; int aps; int batt; int pct; uint32_t events; bool chg; };
static LiveSnap g_liveSnap = {"idle", "starting", "", 0, -1, -1, 0, false};
static String jsonEsc(const String& s) {
  String o; o.reserve(s.length() + 4);
  for (size_t i = 0; i < s.length(); i++) { const char c = s[i]; if (c == '"' || c == '\\') o += '\\'; if (c >= 32) o += c; }
  return o;
}
static void liveUpdate() {
  g_batt.update();
  LiveSnap s;
  const bool scanning = g_wifiScanRunning || g_bleScanRunning || g_scanActive || g_lanScan.active;
  if (scanning) strlcpy(s.state, "scanning", sizeof(s.state));
  else if (g_wardriveOn) strlcpy(s.state, "wardriving", sizeof(s.state));
  else strlcpy(s.state, "idle", sizeof(s.state));
  strlcpy(s.detail, g_scanStatus.c_str(), sizeof(s.detail));
  strlcpy(s.last, g_lastEventKind.c_str(), sizeof(s.last));
  s.aps = g_liveAps;
  s.batt = g_batt.pct; s.chg = g_batt.charging;
  s.pct = g_lanScan.active ? (int)(g_lanScan.nextHost * 100 / 254) : -1;
  s.events = g_eventsLogged;
  g_liveSnap = s;
}
// Companion active ping (v0.12.2): the PC broadcasts
// "AEGIS_PING 1 <nonce>" about every 15 s; a Cardputer holding the
// SD link file (token known) answers "AEGIS_CARD 1 PHOENIX <fw>
// <mac> <HMAC(token, AEGIS_PING:nonce)>" so the Companion can FIND
// and name it before any link. Finding is not linking - the
// on-device ENTER confirmation still gates the live connection.
static WiFiUDP g_pingUdp;
static bool g_pingUdpOn = false;
static void pingService() {
  if (!g_deskToken.length()) {
    static uint32_t lastTryMs = 0;
    if (millis() - lastTryMs < 30000) return;
    lastTryMs = millis();
    loadDesktopCfg();
    if (!g_deskToken.length()) return;
  }
  if (!g_pingUdpOn) { if (!g_pingUdp.begin(8791)) return; g_pingUdpOn = true; }
  int sz;
  while ((sz = g_pingUdp.parsePacket()) > 0) {
    char buf[96];
    const int n = g_pingUdp.read(buf, sizeof(buf) - 1);
    if (n <= 0) continue;
    buf[n] = 0;
    const String r(buf);
    if (r.startsWith("AEGIS_PING 1 ")) {
      String nonce = r.substring(13); nonce.trim();
      if (nonce.length() == 32) {
        String mac = WiFi.macAddress(); mac.replace(":", "");
        const String reply = "AEGIS_CARD 1 PHOENIX " FW_VER_NUM " " + mac + " " +
                             liveHmacHex(g_deskToken, "AEGIS_PING:" + nonce) + "\n";
        g_pingUdp.beginPacket(g_pingUdp.remoteIP(), g_pingUdp.remotePort());
        g_pingUdp.print(reply);
        g_pingUdp.endPacket();
      }
    }
  }
}
static void liveTaskFn(void*) {
  static char frame[384];
  for (;;) {
    if (WiFi.status() == WL_CONNECTED) pingService();
    if (!g_liveEnabled || WiFi.status() != WL_CONNECTED) { delay(2000); continue; }
    if (!g_deskHost.length()) { loadDesktopCfg(); delay(3000); continue; }
    if (!isLinkApproved(g_deskHost, g_deskPort)) { delay(2000); continue; }   // waits for ENTER on device
    WiFiClient c;
    if (!c.connect(g_deskHost.c_str(), g_deskPort, 4000)) {
      if (millis() - g_lastDiscoverMs > 30000) {
        g_lastDiscoverMs = millis();
        if (discoverDesktop()) { delay(500); continue; }
      }
      delay(5000); continue;
    }
    String ln;
    bool ok = liveReadLine(c, ln) && ln.indexOf("AEGIS/1") >= 0;
    String nonce;
    if (ok) { const int np = ln.indexOf("\"nonce\":\""); nonce = np >= 0 ? ln.substring(np + 9, ln.indexOf("\"", np + 9)) : ""; }
    if (ok) {
      c.print("{\"auth\":\"" + liveHmacHex(g_deskToken, nonce) + "\",\"prog\":\"PHOENIX\",\"ver\":\"" FW_VER_NUM "\"}\n");
      ok = liveReadLine(c, ln) && (ln.indexOf("\"ok\": true") >= 0 || ln.indexOf("\"ok\":true") >= 0);
    }
    if (ok) {
      c.print("{\"live_start\":true}\n");
      ok = liveReadLine(c, ln) && ln.indexOf("live_ok") >= 0;
    }
    g_liveLinked = ok;
    String rxLine;
    while (ok && c.connected() && g_liveEnabled && WiFi.status() == WL_CONNECTED) {
      while (c.available()) {
        const char ch = (char)c.read();
        if (ch == '\n') {
          if (rxLine.indexOf("\"fw_offer\"") >= 0) {
            JsonDocument od;
            if (!deserializeJson(od, rxLine)) {
              const String v = od["ver"].as<String>();
              const String sha = od["sha256"].as<String>();
              const uint32_t sz = od["size"].as<uint32_t>();
              if (v.length() && sz && fwVerNewer(v, FW_VER_NUM) && !g_fwPromptActive && !g_fwJobPending) {
                g_fwOfferVer = v; g_fwOfferSha = sha; g_fwOfferSize = sz;
                g_fwOfferAtMs = millis();
                g_fwPromptActive = true;
                Serial.printf("[FW] PC offers PHOENIX v%s (%u bytes) - waiting for ENTER\n", v.c_str(), (unsigned)sz);
              }
            }
          }
          rxLine = "";
        } else if (rxLine.length() < 512) {
          rxLine += ch;
        }
      }
      const LiveSnap s = g_liveSnap;
      int n = snprintf(frame, sizeof(frame),
        "{\"type\":\"live\",\"state\":\"%s\",\"detail\":\"%s\",\"aps\":%d,\"batt\":%d,\"chg\":%s,\"events\":%lu,\"last\":\"%s\"",
        s.state, jsonEsc(String(s.detail)).c_str(), s.aps, s.batt, s.chg ? "true" : "false",
        (unsigned long)s.events, s.last);
      if (n > 0 && n < (int)sizeof(frame) - 72) {
        // Location confidence for the Dashboard (v0.12.7): 0 means
        // "no logging-quality fix", never a fake precision claim.
        const float gpsAcc = gnssHat.positionForLog() ? gnssHat.estimatedAccuracyM() : 0;
        n += snprintf(frame + n, sizeof(frame) - n, ",\"gps_acc\":%.0f,\"gps_rej\":%lu",
                      (double)gpsAcc, (unsigned long)gnssHat.rejectedJumps);
      }
      if (s.pct >= 0 && n > 0 && n < (int)sizeof(frame) - 24) n += snprintf(frame + n, sizeof(frame) - n, ",\"pct\":%d", s.pct);
      if (n > 0 && n < (int)sizeof(frame) - 2) { frame[n++] = '}'; frame[n++] = '\n'; frame[n] = 0; }
      if (c.print(frame) == 0) break;
      pingService();
      delay(2000);
    }
    g_liveLinked = false;
    c.stop();
    delay(3000);
  }
}
static void liveBegin() { xTaskCreatePinnedToCore(liveTaskFn, "aegislive", 6144, nullptr, 1, nullptr, 0); }

// ------------------------------------------------- PHOENIX auto-sync
// The event index uploads itself when PHOENIX is home and idle: the
// desktop dedups by SHA-256, so re-sends of an unchanged file cost
// nothing. Watermark = last synced size in NVS. Only when the radio
// is free - scans and wardrive always win.
static uint32_t g_nextPhxSync = 0;
static String phxFileSha256(const char* path, uint32_t& sizeOut) {
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
// Files the desktop reports + wardrive map are built from. Sent in one
// session; the desktop dedupes by SHA-256, so unchanged files cost one
// header line each.
struct PhxSyncFile { const char* path; const char* name; };
// Self-diagnosis (v0.9.8): one JSON line per check, synced to the PC,
// which files it and turns it into plain-English report cards. This is
// the "tell me what's wrong" channel between the two programs.
static void writeDiagnosticsReport() {
  if (!sdLogMgr.mounted) return;
  if (!SD.exists("/AEGIS")) SD.mkdir("/AEGIS");
  if (!SD.exists("/AEGIS/data")) SD.mkdir("/AEGIS/data");
  const char* path = "/AEGIS/data/diagnostics.jsonl";
  SD.remove(path);
  File f = SD.open(path, FILE_WRITE);
  if (!f) return;
  auto chk = [&](const char* name, bool ok, const String& detail) {
    f.print("{\"kind\":\"diagnostic_check\",\"check\":\"" + String(name) + "\",\"ok\":" +
            (ok ? "true" : "false") + ",\"detail\":\"" + jsonEsc(detail) + "\"}\n");
  };
  const bool joined = WiFi.status() == WL_CONNECTED;
  chk("Firmware", g_integrity.partitionSha.length() > 0, String(FIRMWARE_VER) + " partition " + g_integrity.partitionLabel);
  chk("SD card", sdLogMgr.mounted && g_integrity.sdProbeOk, sdLogMgr.mounted ? "mounted, probe OK" : "NOT mounted or probe failed - logs/maps cannot save");
  chk("Shadow vault", g_integrity.vaultOk || shadowState() == 0, g_integrity.vaultOk ? "AES-256 vault live" : "no vault yet (first use sets it up)");
  chk("Wi-Fi", joined, joined ? ("joined " + WiFi.SSID() + " " + WiFi.localIP().toString()) : ("not joined - " + g_homeJoinStatus));
  chk("PC link", isLinkApproved(g_deskHost, g_deskPort), isLinkApproved(g_deskHost, g_deskPort) ? ("approved " + g_deskHost) : "not confirmed on this device yet (ENTER prompt)");
  chk("Memory", ESP.getFreeHeap() > 20000, String(ESP.getFreeHeap() / 1024) + " KB heap free");
  chk("Main loop", g_loopWatch.worstGapMs() <= 1000, "worst gap " + String(g_loopWatch.worstGapMs()) + " ms");
  chk("Keyboard", true, String(g_keyboardResyncs) + " reader resync(s) this boot");
  f.close();
}
static const PhxSyncFile kPhxSyncFiles[] = {
  {"/AEGIS/data/diagnostics.jsonl", "diagnostics.jsonl"},
  {"/AEGIS/data/event_index.jsonl", "event_index.jsonl"},
  {"/AEGIS/data/wifi_map.geojson", "wifi_map.geojson"},
  {"/AEGIS/data/wifi_map.csv", "wifi_map.csv"},
  {"/AEGIS/data/wifi_map_wigle.csv", "wifi_map_wigle.csv"},
  {"/AEGIS/data/wifi_map.kml", "wifi_map.kml"},
  {"/AEGIS/data/track.gpx", "track.gpx"},
  {"/AEGIS/logs/wardrive.jsonl", "wardrive.jsonl"},
};
static uint32_t phxSyncWatermark() {
  uint32_t sum = 0;
  for (const auto& sf : kPhxSyncFiles) {
    File f = SD.open(sf.path, FILE_READ);
    if (f) { sum += (uint32_t)f.size(); f.close(); }
  }
  return sum;
}
// Send one file over an authenticated session. Returns 1 stored/deduped,
// 0 skipped (missing/empty), -1 session error.
static int phxSyncSend(WiFiClient& c, const PhxSyncFile& sf) {
  if (!SD.exists(sf.path)) return 0;
  uint32_t sz; const String sha = phxFileSha256(sf.path, sz);
  if (!sz || !sha.length()) return 0;
  String ln;
  c.print("{\"file\":\"" + String(sf.name) + "\",\"size\":" + String(sz) + ",\"sha256\":\"" + sha + "\"}\n");
  if (!liveReadLine(c, ln)) return -1;
  if (!(ln.indexOf("\"send\": true") >= 0 || ln.indexOf("\"send\":true") >= 0))
    return 1;   // desktop already has this exact file (SHA match)
  File f = SD.open(sf.path, FILE_READ);
  if (!f) return -1;
  uint8_t buf[3072]; size_t n; uint32_t sent = 0;
  while ((n = f.read(buf, sizeof(buf))) > 0) {
    uint8_t b64[4200]; size_t olen = 0;
    mbedtls_base64_encode(b64, sizeof(b64), &olen, buf, n);
    b64[olen] = 0;
    c.print("{\"data\":\"" + String((const char*)b64) + "\"}\n");
    sent += n;
    screen("SYNC TO DESKTOP", String(sf.name) + " " + String(sz ? sent * 100 / sz : 100) + "%");
  }
  f.close();
  c.print("{\"eof\":true}\n");
  if (!liveReadLine(c, ln)) return -1;
  return (ln.indexOf("\"stored\": true") >= 0 || ln.indexOf("\"stored\":true") >= 0) ? 1 : -1;
}
static bool runPhxSync() {
  if (!sdLogMgr.mounted) return false;
  writeDiagnosticsReport();   // self-diagnosis rides along for the PC to file + explain
  exportMaps();   // refresh geojson/csv/kml/gpx from the atlas first
  if (!g_deskHost.length()) { loadDesktopCfg(); if (!g_deskHost.length()) return false; }
  if (!isLinkApproved(g_deskHost, g_deskPort)) { drawCurrentView(); return false; }
  WiFiClient c;
  screen("SYNC TO DESKTOP", "connecting...");
  if (!c.connect(g_deskHost.c_str(), g_deskPort, 4000)) { drawCurrentView(); return false; }
  String ln;
  bool ok = liveReadLine(c, ln) && ln.indexOf("AEGIS/1") >= 0;
  String nonce;
  if (ok) { const int np = ln.indexOf("\"nonce\":\""); nonce = np >= 0 ? ln.substring(np + 9, ln.indexOf("\"", np + 9)) : ""; }
  if (ok) {
    c.print("{\"auth\":\"" + liveHmacHex(g_deskToken, nonce) + "\",\"prog\":\"PHOENIX\",\"ver\":\"" FW_VER_NUM "\"}\n");
    ok = liveReadLine(c, ln) && (ln.indexOf("\"ok\": true") >= 0 || ln.indexOf("\"ok\":true") >= 0);
  }
  int storedCount = 0;
  if (ok) {
    for (const auto& sf : kPhxSyncFiles) {
      const int r = phxSyncSend(c, sf);
      if (r < 0) { ok = false; break; }
      storedCount += r;
    }
    c.print("{\"done\":true}\n");
    liveReadLine(c, ln);
  }
  c.stop();
  const bool stored = ok && storedCount > 0;
  if (stored) {
    prefs.begin("phoenix", false); prefs.putUInt("syncSz", phxSyncWatermark()); prefs.end();
    if (sdLogMgr.mounted) sdLogMgr.appendLog("aegis_boot.log", String("[SYNC] ") + storedCount + " file(s) to desktop (index+maps+wardrive)");
  }
  drawCurrentView();
  return stored;
}
static void autoSyncPhxTick() {
  if (!g_autoSync || !sdLogMgr.mounted || WiFi.status() != WL_CONNECTED) return;
  if (g_wifiScanRunning || g_bleScanRunning || g_lanScan.active || g_wardriveOn || quarantined) return;
  if ((int32_t)(millis() - g_nextPhxSync) < 0) return;
  const uint32_t wm = phxSyncWatermark();
  prefs.begin("phoenix", true); const uint32_t seen = prefs.getUInt("syncSz", 0); prefs.end();
  if (!wm || wm == seen) { g_nextPhxSync = millis() + 60000; return; }
  if (!g_deskHost.length()) loadDesktopCfg();
  if (!g_deskHost.length() || !isLinkApproved(g_deskHost, g_deskPort)) { g_nextPhxSync = millis() + 30000; return; }
  if (!g_liveLinked) {
    WiFiClient probe;
    if (!g_deskHost.length() || !probe.connect(g_deskHost.c_str(), g_deskPort, 1200)) {
      g_nextPhxSync = millis() + 180000;
      return;
    }
    probe.stop();
  }
  g_nextPhxSync = millis() + (runPhxSync() ? 900000 : 180000);
}


void setup() {
  loadHomeProfile();
  WiFi.onEvent([](WiFiEvent_t e, WiFiEventInfo_t info) {
    if (e == ARDUINO_EVENT_WIFI_STA_DISCONNECTED) {
      g_wifiDiscReason = info.wifi_sta_disconnected.reason;
    }
  });
  agentSetup();
  loadBinds();
  prefs.begin("phoenix", true);
  g_linkApprovedHost = prefs.getString("pcHost", "");
  g_linkApprovedPort = prefs.getInt("pcPort", 0);
  prefs.end();
  M5Cardputer.Display.setBrightness((uint8_t)(g_brightnessPct * 255 / 100));
  g_irRemote.loadProfile();
  if (!g_irRemote.profileLoaded && g_irSavedBrand >= 0) g_irRemote.applyBrand(g_irSavedBrand);
  drawSplash();                 // splash first: if the sweep hangs, you see it
  runIntegritySweep(false);
  g_splashUntil = millis() + 2500;   // 2.5 s splash (was 5 s - felt like a hang)

  // Local forensic-vault self test: prove SD logging works offline.
  if (sdLogMgr.mounted) {
    sdLogMgr.appendLog("aegis_boot.log",
                       String("[BOOT] ") + FIRMWARE_VER + " mac=" + WiFi.macAddress());
  }
  // Last line of defence (v0.7.7): if any single call ever wedges the
  // loop for 30 s (as the RFID I2C probe did), the task watchdog reboots
  // instead of leaving a frozen screen; the splash then reports the
  // stage it died in, and repeat offenders get quarantined at boot.
  esp_task_wdt_init(30, true);
  esp_task_wdt_add(NULL);
  liveBegin();
}

// ---- USB serial console (export / diagnostics) -------------------------
// Line protocol on the USB CDC port, prefix "AEGIS". Lets a laptop pull
// the event index, rule state, and map exports without touching the SD.
static String g_serialLine;
static void handleSerialLine(String line) {
  line.trim();
  if (line == "AEGIS HELP") {
    Serial.println("+AEGIS: AEGIS HELP | STATUS | EVENTS | RULES | EXPORT MAP | PCAP ON/OFF | CHAN | RFID ON/OFF");
  } else if (line == "AEGIS STATUS") {
    Serial.printf("+AEGIS: {\"fw\":\"" FIRMWARE_VER "\",\"uptime_s\":%u,\"events\":%u,"
                  "\"rules\":%u,\"rules_off\":%u,\"atlas\":%u,\"gnss_fix\":%d,\"track\":%u,\"view\":%d}\n",
                  (unsigned)(millis() / 1000), (unsigned)g_eventHistory.size(),
                  (unsigned)localRules.size(), (unsigned)localRules.disabled.size(),
                  (unsigned)g_wifiAtlas.entries.size(), gnssHat.currentFix.valid ? 1 : 0,
                  (unsigned)g_track.size(), (int)g_view);
  } else if (line == "AEGIS EVENTS") {
    Serial.printf("+AEGIS: EVENTS %u BEGIN\n", (unsigned)g_eventHistory.size());
    for (int i = (int)g_eventHistory.size() - 1; i >= 0; i--) {
      const AegisEventRecord& r = g_eventHistory[i];
      String t = r.title; t.replace("\"", "'");
      Serial.printf("+AEGIS: {\"id\":%u,\"epoch\":%u,\"kind\":\"%s\",\"chan\":\"%s\",\"src\":\"%s\","
                    "\"title\":\"%s\",\"conf\":%d,\"rev\":%d,\"prev\":%d}\n",
                    r.id, r.epoch, r.kind.c_str(), r.channel.c_str(), r.source.c_str(),
                    t.c_str(), r.confidence, r.reviewed ? 1 : 0, r.previousBoot ? 1 : 0);
    }
    Serial.println("+AEGIS: END");
  } else if (line == "AEGIS RULES") {
    for (const auto& r : localRules.all())
      Serial.printf("+AEGIS: RULE %s kind=%s %s\n", r.code.c_str(), r.kind.c_str(),
                    localRules.isDisabled(r.code) ? "OFF" : "ON");
    Serial.println("+AEGIS: END");
  } else if (line == "AEGIS RFID ON") {
    rfid2.enabled = true;
    prefs.putBool("rfidOn", true);
    rfid2.begin();
    Serial.printf("+AEGIS: RFID ON attached=%d (SENSORS shows NOT ATTACHED if absent)\n", rfid2.present ? 1 : 0);
  } else if (line == "AEGIS RFID OFF") {
    rfid2.enabled = false;
    prefs.putBool("rfidOn", false);
    Serial.println("+AEGIS: RFID OFF (driver idle, bus untouched)");
  } else if (line == "AEGIS PCAP ON") {
    g_pcapEnabled = true;
    Serial.println("+AEGIS: PCAP ON (/AEGIS/logs/capture.pcap, LINKTYPE 802.11, 8MB max)");
  } else if (line == "AEGIS PCAP OFF") {
    g_pcapEnabled = false;
    Serial.printf("+AEGIS: PCAP OFF frames=%u dropped=%u\n", (unsigned)g_pcapFrames, (unsigned)g_pcapDropped);
  } else if (line == "AEGIS CHAN") {
    Serial.print("+AEGIS: CHAN density [");
    for (int c = 1; c <= 14; c++) Serial.printf("%s%u", c > 1 ? "," : "", (unsigned)g_chanFrames[c]);
    Serial.println("]");
  } else if (line == "AEGIS EXPORT MAP") {
    const int n = exportMaps();
    Serial.printf("+AEGIS: EXPORT MAP %s (%d GPS nets)\n", n < 0 ? "FAILED" : "OK", n);
  } else if (line.startsWith("AEGIS")) {
    Serial.println("+AEGIS: ERR unknown command (AEGIS HELP)");
  }
}
static void serviceSerial() {
  while (Serial.available()) {
    const char c = (char)Serial.read();
    if (c == '\n' || c == '\r') {
      if (g_serialLine.length()) { handleSerialLine(g_serialLine); g_serialLine = ""; }
    } else if (g_serialLine.length() < 96) g_serialLine += c;
  }
}

static void failHomeJoin(const String& shortWhy, const String& prompt, bool ssidProblem) {
  g_homeJoinActive = false; g_wifiJoinGuard = false;
  g_homeJoinStatus = "FAIL: " + shortWhy;
  WiFi.disconnect(false);
  g_view = VIEW_MODULES;
  if (ssidProblem) {
    g_shadowTyping = 4; g_shadowBuf = g_homeCandidateSsid;
    shadowSetMsg(prompt);
  } else {
    g_shadowTyping = 5; g_shadowBuf = "";
    shadowSetMsg(prompt);
  }
  drawCurrentView();
}
static void wifiJoinTick() {
  if (!g_homeJoinActive) return;
  const wl_status_t st = WiFi.status();
  if (st == WL_CONNECTED) {
    g_homeJoinActive = false;
    saveHomeProfile(g_homeCandidateSsid, g_homeCandidatePsk);   // only NOW is it stored
    g_homeJoinStatus = "CONNECTED " + WiFi.SSID();
    shadowSetMsg("CONNECTED to " + WiFi.SSID() + " - IP " + WiFi.localIP().toString());
    g_view = VIEW_MODULES;
    if (sdLogMgr.mounted) sdLogMgr.appendLog("aegis_boot.log", "[WIFI] joined " + WiFi.SSID() + " " + WiFi.localIP().toString());
    drawCurrentView();
    return;
  }
  if (st == WL_NO_SSID_AVAIL) {
    failHomeJoin("SSID NOT FOUND", "Network not found - check the SSID, ENTER retries:", true);
    return;
  }
  if (st == WL_CONNECT_FAILED) {
    failHomeJoin("ROUTER REFUSED", "Router refused - retype password (ESC cancels):", false);
    return;
  }
  if (millis() - g_homeJoinStartMs > 15000) {
    // ESP32 reports a wrong WPA password as a plain disconnect, so a
    // silent 15 s is called what it most likely is - with the retry open.
    failHomeJoin("WRONG PASSWORD?", "No connection - likely WRONG PASSWORD. Try again (ESC cancels):", false);
  }
}
static void homeNetTick() {
  if (g_homeJoinActive || g_shadowTyping == 4 || g_shadowTyping == 5) return;   // setup owns the radio
  if (g_passiveOffline || quarantined || !g_wifiSsid.length()) {
    g_autoJoinActive = false; g_wifiJoinGuard = false;
    // Say WHY we are not joining (v0.12.2) - silently sitting at
    // "not checked yet" left the user guessing.
    const String why = g_passiveOffline ? "not joining: Passive offline is ON (SETUP menu)"
                     : quarantined ? "not joining: quarantined - radios muted"
                                   : "no saved WiFi yet (SETUP > Home WiFi setup)";
    if (g_homeJoinStatus != why) g_homeJoinStatus = why;
    return;
  }
  if (WiFi.status() == WL_CONNECTED) {
    g_autoJoinActive = false; g_wifiJoinGuard = false; g_autoJoinNextMs = 0;
    g_homeJoinStatus = "CONNECTED " + WiFi.SSID() + " " + WiFi.localIP().toString();
    return;
  }
  const bool busy = g_wifiScanRunning || g_bleScanRunning || g_lanScan.active || g_wardriveOn;
  if (busy) return;                       // a scan already running finishes first
  const uint32_t now = millis();
  if (g_autoJoinActive) {
    g_wifiJoinGuard = true; g_wifiJoinGuardUntilMs = g_autoJoinStartMs + 20000;
    if (now - g_autoJoinStartMs < 20000) {
      g_homeJoinStatus = "AUTO-JOINING " + g_wifiSsid + " (" + String((now - g_autoJoinStartMs) / 1000) + "s)";
      return;
    }
    // One full 20 s attempt failed. Say why, then keep retrying every
    // 30 s - a saved network coming into range joins on its own.
    const wl_status_t st = WiFi.status();
    g_autoJoinActive = false; g_wifiJoinGuard = false; g_autoJoinNextMs = now + 30000;
    g_homeJoinStatus = st == WL_NO_SSID_AVAIL ? "AUTO: " + g_wifiSsid + " not in range - will keep trying"
                     : "AUTO FAIL: " + g_wifiSsid + " - " + wifiDiscWhy() + " - retrying";
    g_wifiDiscReason = 0;
    Serial.printf("[WIFI] auto-join attempt ended, status=%d - retry in 30 s\n", (int)st);
    return;
  }
  if ((int32_t)(now - g_autoJoinNextMs) < 0) return;
  WiFi.mode(WIFI_STA);
  WiFi.begin(g_wifiSsid.c_str(), g_wifiPsk.c_str());
  g_autoJoinActive = true; g_autoJoinStartMs = now;
  g_wifiJoinGuard = true; g_wifiJoinGuardUntilMs = now + 20000;
  g_homeJoinStatus = "AUTO-JOINING " + g_wifiSsid;
  Serial.printf("[WIFI] auto-joining saved network %s\n", g_wifiSsid.c_str());
}



// ------------------------------------------------- Wi-Fi CSI presence
// EXPERIMENTAL presence/motion sensing from Wi-Fi Channel State
// Info: the ESP32-S3 reports per-packet subcarrier amplitudes, and
// people moving in a room make them wobble. This is a
// self-calibrating motion score: presence/motion ONLY - no imaging,
// no identification, nothing like the "Wi-Fi lidar" videos (that is
// not physics this chip can do). Own/authorized spaces only.
#include <esp_wifi.h>
static volatile uint16_t g_csiRing[32];
static volatile uint8_t g_csiRingIdx = 0;
static volatile uint32_t g_csiPkts = 0;
static void csiRxCb(void* ctx, wifi_csi_info_t* info) {
  (void)ctx;
  if (!info || !info->buf || info->len <= 0) return;
  int n = info->len; if (n > 128) n = 128;
  uint32_t e = 0;
  for (int i = 0; i < n; i++) e += (uint32_t)abs((int)info->buf[i]);
  g_csiRing[g_csiRingIdx] = (uint16_t)(e * 16 / (uint32_t)n);
  g_csiRingIdx = (uint8_t)((g_csiRingIdx + 1) & 31);
  g_csiPkts++;
}
static bool g_csiStarted = false;
static float g_csiBase = 0;             // learned "calm room" variance
static uint32_t g_csiLastTickMs = 0, g_csiLastEventMs = 0;
static int g_csiHotSecs = 0;
static bool csiBegin() {
  wifi_csi_config_t cfg = {};
  cfg.lltf_en = true; cfg.htltf_en = true; cfg.stbc_htltf2_en = true;
  cfg.ltf_merge_en = true; cfg.channel_filter_en = false;
  cfg.manu_scale = false; cfg.shift = 0;
  if (esp_wifi_set_csi_config(&cfg) != ESP_OK) return false;
  if (esp_wifi_set_csi_rx_cb(&csiRxCb, nullptr) != ESP_OK) return false;
  if (esp_wifi_set_csi(true) != ESP_OK) return false;
  g_csiStarted = true;
  Serial.println("[CSI] presence sensing on");
  return true;
}
static void csiStop() {
  esp_wifi_set_csi(false);
  g_csiStarted = false; g_csiPresent = false; g_csiScore = 0;
  Serial.println("[CSI] presence sensing off");
}
static void csiTick() {
  if (!g_csiOn) return;
  if (!g_csiStarted) { if (WiFi.getMode() != WIFI_OFF) csiBegin(); return; }
  const uint32_t now = millis();
  if (now - g_csiLastTickMs < 1000) return;
  g_csiLastTickMs = now;
  uint16_t v[32];
  for (int i = 0; i < 32; i++) v[i] = g_csiRing[i];
  float mean = 0; for (int i = 0; i < 32; i++) mean += v[i];
  mean /= 32.0f;
  float var = 0; for (int i = 0; i < 32; i++) { const float d = v[i] - mean; var += d * d; }
  const float sd = sqrtf(var / 32.0f);
  if (g_csiBase < 1.0f) g_csiBase = sd > 1 ? sd : 20.0f;
  if (sd < g_csiBase * 1.6f) g_csiBase = g_csiBase * 0.95f + sd * 0.05f;   // learn only when calm
  const float ratio = g_csiBase > 0 ? sd / g_csiBase : 1.0f;
  int score = (int)((ratio - 1.0f) * 45.0f);
  g_csiScore = score < 0 ? 0 : (score > 100 ? 100 : score);
  const bool hot = g_csiScore >= 45 && g_csiPkts > 20;
  g_csiHotSecs = hot ? g_csiHotSecs + 1 : 0;
  const bool was = g_csiPresent;
  g_csiPresent = g_csiHotSecs >= 2;
  if (g_csiPresent && !was && now - g_csiLastEventMs > 300000) {
    g_csiLastEventMs = now;
    JsonDocument pd; JsonObject pf = pd.to<JsonObject>();
    pf["csi.score"] = g_csiScore;
    pushEvent("presence_motion", "wifi", "csi", "Motion detected nearby (Wi-Fi CSI presence)", pf);
  }
}

static void csiToggle() {
  g_csiOn = !g_csiOn;
  prefs.putBool("csiOn", g_csiOn);
  if (!g_csiOn) csiStop();
}

// ------------------------------------------------------ repo self-update
static bool fwVerNewer(const String& a, const String& b) {
  int pa[3] = {0, 0, 0}, pb[3] = {0, 0, 0};
  sscanf(a.c_str(), "%d.%d.%d", &pa[0], &pa[1], &pa[2]);
  sscanf(b.c_str(), "%d.%d.%d", &pb[0], &pb[1], &pb[2]);
  for (int i = 0; i < 3; i++) if (pa[i] != pb[i]) return pa[i] > pb[i];
  return false;
}

static bool fetchUpdateManifest(String& ver, String& binUrl, String& sha, uint32_t& sizeOut) {
  WiFiClientSecure cli;
  cli.setInsecure();  // transport only - the image is SHA-256-pinned below
  HTTPClient http;
  http.setTimeout(12000);
  http.setFollowRedirects(HTTPC_STRICT_FOLLOW_REDIRECTS);
  if (!http.begin(cli, g_updateFeed)) { g_updateStatus = "bad feed URL"; return false; }
  const int code = http.GET();
  if (code != 200) {
    g_updateStatus = code == 404 ? "feed 404 (repo private?)" : "feed HTTP " + String(code);
    http.end();
    return false;
  }
  String body = http.getString();
  http.end();
  JsonDocument d;
  if (deserializeJson(d, body)) { g_updateStatus = "bad update manifest"; return false; }
  ver = d["version"].as<String>();
  sha = d["sha256"].as<String>();
  sizeOut = d["size"].as<uint32_t>();
  binUrl = d["url"].as<String>();
  if (!binUrl.length()) {
    String file = d["file"].as<String>();
    const int slash = g_updateFeed.lastIndexOf('/');
    if (file.length() && slash > 0) binUrl = g_updateFeed.substring(0, slash + 1) + file;
  }
  if (!ver.length() || !binUrl.length()) { g_updateStatus = "manifest incomplete"; return false; }
  return true;
}

static bool otaConfirm(const String& line1, const String& line2) {
  screen(line1, line2 + "  ENTER=yes ESC=no", TFT_YELLOW);
  const uint32_t t0 = millis();
  while (millis() - t0 < 30000) {
    M5Cardputer.update();
    if (M5Cardputer.Keyboard.isChange() && M5Cardputer.Keyboard.isPressed()) {
      auto ks = M5Cardputer.Keyboard.keysState();
      if (ks.enter) return true;
      for (char ch : ks.word) if (ch == '`') return false;
    }
    delay(20);
  }
  return false;
}

static String otaFileSha256(const char* path, uint32_t& sizeOut) {
  File f = SD.open(path, FILE_READ);
  if (!f) { sizeOut = 0; return ""; }
  sizeOut = f.size();
  mbedtls_sha256_context ctx;
  mbedtls_sha256_init(&ctx);
  mbedtls_sha256_starts(&ctx, 0);
  uint8_t buf[1024];
  size_t n;
  while ((n = f.read(buf, sizeof(buf))) > 0) mbedtls_sha256_update(&ctx, buf, n);
  uint8_t out[32];
  mbedtls_sha256_finish(&ctx, out);
  mbedtls_sha256_free(&ctx);
  f.close();
  char hex[65];
  for (int i = 0; i < 32; i++) snprintf(hex + i * 2, 3, "%02x", out[i]);
  return String(hex);
}

static void runFirmwareUpdate() {
  if (WiFi.status() != WL_CONNECTED) { g_updateStatus = "join WiFi first"; screen("FIRMWARE UPDATE", "join WiFi first (SETUP)", TFT_RED); delay(1500); return; }
  if (!sdLogMgr.mounted) { g_updateStatus = "needs SD card"; screen("FIRMWARE UPDATE", "insert an SD card first", TFT_RED); delay(1500); return; }
  if (g_wardriveOn) { g_updateStatus = "stop wardrive first"; screen("FIRMWARE UPDATE", "stop wardrive first", TFT_RED); delay(1500); return; }
  screen("FIRMWARE UPDATE", "checking the repo...", TFT_CYAN);
  String ver, url, sha;
  uint32_t size = 0;
  if (!fetchUpdateManifest(ver, url, sha, size)) {
    screen("UPDATE CHECK FAILED", g_updateStatus, TFT_RED);
    delay(2000);
    return;
  }
  g_updateChecked = true;
  g_updateLastCheckMs = millis();
  if (!fwVerNewer(ver, FW_VER_NUM)) {
    g_updateAvailVer = "";
    g_updateStatus = "up to date";
    screen("UP TO DATE", "this is v" FW_VER_NUM " - nothing newer", TFT_GREEN);
    delay(1600);
    return;
  }
  g_updateAvailVer = ver;
  g_updateStatus = "v" + ver + " available";
  if (g_batt.pct >= 0 && g_batt.pct < 25 && !g_batt.charging) {
    g_updateStatus = "battery too low";
    screen("UPDATE PAUSED", "battery under 25% - charge first", TFT_RED);
    delay(2000);
    return;
  }
  if (!otaConfirm("UPDATE TO v" + ver + "?", "download + flash (~1.6 MB)")) {
    g_updateStatus = "update postponed";
    return;
  }
  // Download to SD first so the image can be hash-verified whole.
  WiFiClientSecure cli;
  cli.setInsecure();
  HTTPClient http;
  http.setTimeout(15000);
  http.setFollowRedirects(HTTPC_STRICT_FOLLOW_REDIRECTS);
  if (!http.begin(cli, url) || http.GET() != 200) {
    g_updateStatus = "download failed";
    screen("UPDATE FAILED", "download did not start", TFT_RED);
    delay(2000);
    return;
  }
  const int total = http.getSize();
  WiFiClient* stream = http.getStreamPtr();
  File out = SD.open("/AEGIS/update.bin", FILE_WRITE);
  if (!out) { g_updateStatus = "SD write failed"; http.end(); return; }
  uint8_t buf[1024];
  int got = 0;
  uint32_t lastDraw = 0;
  bool aborted = false;
  while (http.connected() && (total == -1 || got < total)) {
    const size_t avail = stream->available();
    if (avail) {
      const int want = avail > sizeof(buf) ? (int)sizeof(buf) : (int)avail;
      const int n = stream->readBytes(buf, want);
      if (n > 0) { out.write(buf, n); got += n; }
    } else {
      delay(5);
    }
    if (millis() - lastDraw > 400) {
      lastDraw = millis();
      screen("DOWNLOADING v" + ver, total > 0 ? (String(got * 100 / total) + "% - ESC aborts") : (String(got / 1024) + " KB - ESC aborts"), TFT_CYAN);
    }
    M5Cardputer.update();
    if (M5Cardputer.Keyboard.isChange() && M5Cardputer.Keyboard.isPressed()) {
      auto ks = M5Cardputer.Keyboard.keysState();
      for (char ch : ks.word) if (ch == '`') aborted = true;
    }
    if (aborted) break;
  }
  out.close();
  http.end();
  if (aborted) {
    SD.remove("/AEGIS/update.bin");
    g_updateStatus = "download aborted";
    return;
  }
applyStagedUpdate(ver, sha, size);
}

static void applyStagedUpdate(const String& ver, const String& sha, uint32_t size) {
screen("VERIFYING", "SHA-256 check...", TFT_CYAN);
uint32_t fsize = 0;
const String digest = otaFileSha256("/AEGIS/update.bin", fsize);
if (!sha.length() || !digest.equalsIgnoreCase(sha) || (size && fsize != size)) {
  SD.remove("/AEGIS/update.bin");
  g_updateStatus = "SHA-256 mismatch - refused";
  screen("UPDATE REFUSED", "file failed SHA-256 - not flashed", TFT_RED);
  delay(2500);
  return;
}
screen("FLASHING v" + ver, "do NOT power off...", TFT_YELLOW);
File in = SD.open("/AEGIS/update.bin", FILE_READ);
if (!in || !Update.begin(fsize)) {
  g_updateStatus = "flash begin failed";
  screen("UPDATE FAILED", "could not start flashing", TFT_RED);
  delay(2000);
  return;
}
uint8_t fbuf[1024];
size_t n;
while ((n = in.read(fbuf, sizeof(fbuf))) > 0) Update.write(fbuf, n);
in.close();
if (Update.end(true) && Update.isFinished()) {
  SD.remove("/AEGIS/update.bin");
  screen("UPDATE COMPLETE", "v" + ver + " - rebooting...", TFT_GREEN);
  delay(1500);
  ESP.restart();
}
g_updateStatus = "flash failed";
screen("UPDATE FAILED", String("error: ") + Update.errorString(), TFT_RED);
delay(2500);
}

// PC-pushed update (v0.12.4): pull the offered image from the
// Companion over the LAN on a fresh authenticated connection, stage
// it on SD, then verify + flash exactly like the repo path.
static void runLanFirmwareUpdate() {
  const String ver = g_fwOfferVer, sha = g_fwOfferSha;
  const uint32_t size = g_fwOfferSize;
  if (WiFi.status() != WL_CONNECTED) { g_updateStatus = "PC update: join WiFi first"; screen("PC UPDATE", "join WiFi first", TFT_RED); delay(1800); return; }
  if (!sdLogMgr.mounted) { g_updateStatus = "PC update: needs SD card"; screen("PC UPDATE", "insert an SD card first", TFT_RED); delay(1800); return; }
  if (g_wardriveOn) { g_updateStatus = "PC update: stop wardrive first"; screen("PC UPDATE", "stop wardrive first", TFT_RED); delay(1800); return; }
  if (g_batt.pct >= 0 && g_batt.pct < 25 && !g_batt.charging) {
    g_updateStatus = "PC update: battery too low";
    screen("UPDATE PAUSED", "battery under 25% - charge first", TFT_RED); delay(2000); return;
  }
  if (!g_deskHost.length()) loadDesktopCfg();
  screen("PC UPDATE v" + ver, "connecting to your PC...", TFT_CYAN);
  WiFiClient c;
  if (!c.connect(g_deskHost.c_str(), g_deskPort, 5000)) {
    g_updateStatus = "PC update: PC unreachable";
    screen("UPDATE FAILED", "could not reach the PC", TFT_RED); delay(2000); return;
  }
  String ln;
  bool ok = liveReadLine(c, ln) && ln.indexOf("AEGIS/1") >= 0;
  String nonce;
  if (ok) { const int np = ln.indexOf("\"nonce\":\""); nonce = np >= 0 ? ln.substring(np + 9, ln.indexOf("\"", np + 9)) : ""; }
  if (ok) {
    c.print("{\"auth\":\"" + liveHmacHex(g_deskToken, nonce) + "\",\"prog\":\"PHOENIX\",\"ver\":\"" FW_VER_NUM "\"}\n");
    ok = liveReadLine(c, ln) && (ln.indexOf("\"ok\": true") >= 0 || ln.indexOf("\"ok\":true") >= 0);
  }
  if (ok) {
    c.print("{\"fw_get\":true}\n");
    ok = liveReadLine(c, ln);
  }
  uint32_t total = 0; String srvSha;
  if (ok) {
    JsonDocument d;
    ok = !deserializeJson(d, ln) && d["fw_begin"].as<bool>();
    if (ok) { total = d["size"].as<uint32_t>(); srvSha = d["sha256"].as<String>(); }
  }
  if (!ok || !total) {
    g_updateStatus = "PC update refused by PC";
    screen("UPDATE FAILED", "the PC has no image to give", TFT_RED); delay(2000); c.stop(); return;
  }
  File out = SD.open("/AEGIS/update.bin", FILE_WRITE);
  if (!out) { g_updateStatus = "PC update: SD write failed"; screen("UPDATE FAILED", "SD write failed", TFT_RED); delay(2000); c.stop(); return; }
  uint8_t buf[1024];
  uint32_t got = 0, lastDraw = 0;
  bool aborted = false;
  const uint32_t t0 = millis();
  while (got < total && millis() - t0 < 180000) {
    const size_t avail = c.available();
    if (avail) {
      size_t want = avail > sizeof(buf) ? sizeof(buf) : avail;
      if (want > (size_t)(total - got)) want = (size_t)(total - got);
      const int n = c.read(buf, want);
      if (n > 0) { out.write(buf, n); got += (uint32_t)n; }
    } else {
      if (!c.connected()) break;
      delay(3);
    }
    if (millis() - lastDraw > 400) {
      lastDraw = millis();
      screen("PC UPDATE v" + ver, String(got * 100 / total) + "% from your PC - ESC aborts", TFT_CYAN);
    }
    M5Cardputer.update();
    if (M5Cardputer.Keyboard.isChange() && M5Cardputer.Keyboard.isPressed()) {
      auto ks = M5Cardputer.Keyboard.keysState();
      for (char ch : ks.word) if (ch == '`') aborted = true;
    }
    if (aborted) break;
  }
  out.close();
  c.stop();
  if (aborted || got != total) {
    SD.remove("/AEGIS/update.bin");
    g_updateStatus = aborted ? "PC update aborted" : "PC download incomplete";
    screen("UPDATE FAILED", aborted ? "aborted - nothing flashed" : "download incomplete - nothing flashed", TFT_RED);
    delay(2200);
    return;
  }
  g_updateStatus = "PC image downloaded - verifying";
  applyStagedUpdate(ver, srvSha.length() ? srvSha : sha, total);
}

static void fwJobTick() {
  if (g_fwPromptActive && millis() - g_fwOfferAtMs > 90000) g_fwPromptActive = false;
  if (!g_fwJobPending) return;
  g_fwJobPending = false;
  runLanFirmwareUpdate();
  drawCurrentView();
}

// Quiet auto-check: once ~2 min after boot (and every 6 h) while on
// home Wi-Fi, so SETUP can say "vX available" before you go looking.

// ------------------------------------------------- open Wi-Fi (info only)
// Join a FREE/OPEN network the user picks, for information only:
// clock sync + a firmware-update check. Open networks are treated
// as UNTRUSTED - no passwords, no link secret, nothing sensitive
// crosses them (the OTA image is SHA-256-pinned, so its download
// is tamper-proof even here). Never auto-joins: the user picks
// from this list and confirms, every time (house rule).
struct OpenAp { String ssid; String bssid; int rssi; int chan; };
static std::vector<OpenAp> g_openAps;
static int g_openSel = 0;

static void openWifiBanner(const String& msg) {   // red banner, house rule
  g_alertText = msg; g_alertUntilMs = millis() + 8000;
}
static void openWifiDraw() {
  auto& d = M5Cardputer.Display;
  d.fillScreen(TFT_BLACK);
  d.setTextSize(1); d.setTextColor(TFT_CYAN, TFT_BLACK); d.setCursor(4, 4);
  d.println("OPEN WiFi - free info only (UNTRUSTED)");
  const int rows = 9;
  int top = g_openSel - rows + 1; if (top < 0) top = 0;
  if (g_openSel < top) top = g_openSel;
  for (int r = 0; r < rows; r++) {
    const int i = top + r; if (i >= (int)g_openAps.size()) break;
    const bool sel = i == g_openSel;
    d.setTextColor(sel ? TFT_BLACK : TFT_WHITE, sel ? TFT_YELLOW : TFT_BLACK);
    d.setCursor(4, 18 + r * 10);
    String line = String(sel ? "> " : "  ") + g_openAps[i].ssid;
    while (line.length() < 24) line += ' ';
    line += String(g_openAps[i].rssi) + "dBm ch" + String(g_openAps[i].chan);
    d.println(line.substring(0, 38));
  }
  d.setTextColor(TFT_YELLOW, TFT_BLACK); d.setCursor(4, 112);
  d.println("UP/DN=move  ENTER=join  `=back");
  d.setTextColor(TFT_RED, TFT_BLACK); d.setCursor(4, 124);
  d.println("no passwords or secrets on open nets");
}
static bool openWifiPickKey(int& sel, bool& join) {   // one key event; true = a key arrived
  M5Cardputer.update();
  if (!(M5Cardputer.Keyboard.isChange() && M5Cardputer.Keyboard.isPressed())) return false;
  auto ks = M5Cardputer.Keyboard.keysState();
  if (ks.enter) { join = true; return true; }
  for (char ch : ks.word) {
    if (ch == '`') { sel = -1; return true; }
    if (ch == ';' && g_openSel > 0) g_openSel--;
    if (ch == '.' && g_openSel + 1 < (int)g_openAps.size()) g_openSel++;
  }
  return true;
}
static void openWifiLeave() {
  // Drop the open net; homeNetTick re-joins the saved home profile.
  WiFi.disconnect(false);
  g_openWifiStatus = "left open net - home rejoin pending";
}
static void runOpenWifi() {
  if (quarantined) { screen("OPEN WIFI", "quarantined - radios muted", TFT_RED); delay(1800); return; }
  if (g_wardriveOn) { screen("OPEN WIFI", "stop wardrive first", TFT_RED); delay(1800); return; }
  if (g_passiveOffline) { screen("OPEN WIFI", "passive offline is ON - SETUP first", TFT_RED); delay(1800); return; }
  screen("OPEN WIFI", "scanning for open networks...", TFT_CYAN);
  WiFi.mode(WIFI_STA);
  const int n = WiFi.scanNetworks(false, false);
  g_openAps.clear();
  for (int i = 0; i < n; i++) {
    if (WiFi.encryptionType(i) != WIFI_AUTH_OPEN) continue;
    const String ssid = WiFi.SSID(i);
    if (!ssid.length()) continue;
    bool dup = false;
    for (auto& a : g_openAps) if (a.ssid == ssid) { dup = true; if (WiFi.RSSI(i) > a.rssi) { a.rssi = WiFi.RSSI(i); a.chan = WiFi.channel(i); a.bssid = WiFi.BSSIDstr(i); } break; }
    if (!dup) g_openAps.push_back({ssid, WiFi.BSSIDstr(i), (int)WiFi.RSSI(i), (int)WiFi.channel(i)});
  }
  WiFi.scanDelete();
  for (size_t i = 1; i < g_openAps.size(); i++) {   // strongest first
    const OpenAp t = g_openAps[i]; size_t j = i;
    while (j > 0 && g_openAps[j - 1].rssi < t.rssi) { g_openAps[j] = g_openAps[j - 1]; j--; }
    g_openAps[j] = t;
  }
  if (g_openAps.empty()) {
    g_openWifiStatus = "no open networks in range";
    screen("OPEN WIFI", "no open networks in range", TFT_RED);
    openWifiBanner("Open WiFi: none in range");
    delay(2000); return;
  }
  g_openSel = 0; openWifiDraw();
  while (true) {                                     // pick one (user confirms - always)
    int sel = g_openSel; bool join = false;
    if (openWifiPickKey(sel, join)) {
      if (sel < 0) { g_openWifiStatus = "open net: not joined"; return; }
      if (join) break;
      openWifiDraw();
    }
    delay(15);
  }
  const OpenAp ap = g_openAps[g_openSel];
  if (!otaConfirm("JOIN OPEN NETWORK?", ap.ssid + " - untrusted, info only.")) {
    g_openWifiStatus = "open net: join cancelled"; return;
  }
  screen("OPEN WIFI", "joining " + ap.ssid + " ...", TFT_CYAN);
  WiFi.disconnect(false);
  WiFi.begin(ap.ssid.c_str());
  const uint32_t t0 = millis();
  while (WiFi.status() != WL_CONNECTED && millis() - t0 < 15000) {
    M5Cardputer.update();
    if (M5Cardputer.Keyboard.isChange() && M5Cardputer.Keyboard.isPressed()) {
      auto ks = M5Cardputer.Keyboard.keysState();
      for (char ch : ks.word) if (ch == '`') { WiFi.disconnect(false); g_openWifiStatus = "open net: join aborted"; return; }
    }
    delay(50);
  }
  if (WiFi.status() != WL_CONNECTED) {
    g_openWifiStatus = "JOIN FAILED: " + ap.ssid;
    screen("OPEN WIFI FAILED", "could not join " + ap.ssid, TFT_RED);
    openWifiBanner("Open WiFi join failed: " + ap.ssid);
    delay(2200); openWifiLeave(); return;
  }
  // Joined. Clock first (NTP), then classify the connection: a
  // captive portal answers 200-with-a-page instead of 204-empty.
  configTime(0, 0, "pool.ntp.org", "time.nist.gov");
  for (int i = 0; i < 10 && time(nullptr) < 1700000000; i++) delay(400);
  bool portal = false, internet = false;
  {
    WiFiClient plain; HTTPClient http; http.setTimeout(6000);
    if (http.begin(plain, "http://connectivitycheck.gstatic.com/generate_204")) {
      const int code = http.GET();
      internet = code > 0; portal = (code == 200) || (code >= 300 && code < 400);
      http.end();
    }
  }
  String when = "clock: waiting on NTP";
  { const time_t t = time(nullptr); if (t > 1700000000) { char b[24]; strftime(b, sizeof(b), "%H:%M:%S UTC", gmtime(&t)); when = String("clock: ") + b; } }
  String upd = "updates: not checked";
  if (internet && !portal) {
    String ver, url, sha; uint32_t sz = 0;
    if (fetchUpdateManifest(ver, url, sha, sz)) upd = fwVerNewer(ver, String(FW_VER_NUM)) ? ("updates: v" + ver + " AVAILABLE (SETUP)") : "updates: up to date";
    else upd = "updates: " + g_updateStatus;
  }
  if (sdLogMgr.mounted) sdLogMgr.appendLog("aegis_boot.log", "[OPENWIFI] joined " + ap.ssid + " portal=" + String(portal ? 1 : 0));
  { JsonDocument jd; JsonObject f = jd.to<JsonObject>();
    f["ssid"] = ap.ssid; f["bssid"] = ap.bssid; f["rssi"] = ap.rssi; f["portal"] = portal;
    pushEvent("open_wifi_join", "wifi", "open-ap", "Joined OPEN network (info only, untrusted)", f); }
  g_openWifiStatus = "on OPEN " + ap.ssid + " " + WiFi.localIP().toString() + (portal ? " (portal)" : "");
  while (true) {                                     // status screen until ESC
    auto& d = M5Cardputer.Display;
    d.fillScreen(TFT_BLACK); d.setTextSize(1);
    d.setTextColor(TFT_RED, TFT_BLACK); d.setCursor(4, 4);
    d.println("!! ON OPEN NETWORK - UNTRUSTED !!");
    d.setTextColor(TFT_WHITE, TFT_BLACK); d.setCursor(4, 20);
    d.println("net:  " + ap.ssid); d.setCursor(4, 32);
    d.println("ip:   " + WiFi.localIP().toString()); d.setCursor(4, 44);
    d.println(when); d.setCursor(4, 56);
    d.println(upd); d.setCursor(4, 68);
    d.setTextColor(portal ? TFT_RED : (internet ? TFT_GREEN : TFT_YELLOW), TFT_BLACK);
    d.println(portal ? "CAPTIVE PORTAL - no real internet" : (internet ? "internet: OK (info only)" : "internet: unreachable"));
    d.setTextColor(TFT_YELLOW, TFT_BLACK); d.setCursor(4, 112);
    d.println("`=leave (home WiFi rejoins itself)");
    bool leave = false;
    while (!leave) {
      M5Cardputer.update();
      if (M5Cardputer.Keyboard.isChange() && M5Cardputer.Keyboard.isPressed()) {
        auto ks = M5Cardputer.Keyboard.keysState();
        for (char ch : ks.word) if (ch == '`') leave = true;
      }
      delay(30);
    }
    break;
  }
  openWifiLeave();
}

static void updateCheckTick() {
  if (WiFi.status() != WL_CONNECTED || g_passiveOffline || quarantined) return;
  if (millis() < 120000) return;
  if (g_updateChecked && millis() - g_updateLastCheckMs < 6UL * 3600UL * 1000UL) return;
  if (millis() - g_lastKeyActivityMs < 10000) return;
  g_updateChecked = true;
  g_updateLastCheckMs = millis();
  String ver, url, sha;
  uint32_t size = 0;
  if (fetchUpdateManifest(ver, url, sha, size)) {
    if (fwVerNewer(ver, FW_VER_NUM)) {
      g_updateAvailVer = ver;
      g_updateStatus = "v" + ver + " available";
      Serial.printf("[OTA] update available: %s (this: %s)\n", ver.c_str(), FW_VER_NUM);
    } else {
      g_updateStatus = "up to date";
    }
  }
}

void loop() {
  // Keyboard hardware first. agentLoop() used to run (and, every 20 s,
  // block in a scan) before this code ever saw a key.
  M5Cardputer.update();
  esp_task_wdt_reset();
  homeNetTick();
  updateCheckTick();
  csiTick();
  wifiJoinTick();
  linkPromptTick();
  fwJobTick();
  liveUpdate();
  autoSyncPhxTick();
  const uint32_t nowMs = millis();
  g_loopWatch.tick(nowMs);
  serviceSerial();

  // Splash gate: 5 s animated logo, any pressed key skips.
  if (nowMs < g_splashUntil) {
    static uint32_t lastSplashFrame = 0;
    if (nowMs - lastSplashFrame > 120) {
      lastSplashFrame = nowMs;
      drawSplashFrame(nowMs / 120);
    }
    if (M5Cardputer.Keyboard.isPressed()) { g_splashUntil = 0; drawCurrentView(); }
    return;
  }

  bool pressedNow = false;
  int wordCount = 0;
  char onlyKey = 0;
  String sig = "";
  readLogicalKeyboard(pressedNow, wordCount, onlyKey, sig);
  if (pressedNow) g_lastKeyActivityMs = nowMs;

  // Optional screen sleep (SETUP; default never). Detectors keep
  // running while the panel is dark - only key handling and drawing
  // pause - and the first keypress just wakes the screen.
  if (g_screenAsleep) {
    if (pressedNow) {
      g_screenAsleep = false;
      M5Cardputer.Display.setBrightness((uint8_t)(g_brightnessPct * 255 / 100));
      g_lastKeyActivityMs = nowMs;
      drawCurrentView();
      return;
    }
  } else if (g_screenSleepSec && nowMs - g_lastKeyActivityMs > (uint32_t)g_screenSleepSec * 1000UL) {
    g_screenAsleep = true;
    M5Cardputer.Display.setBrightness(0);
    Serial.println("[UI] screen asleep (detectors still running)");
  }

  // TCA8418 FIFO-overflow phantom key: same stuck signature >5s after the
  // action fired -> reinitialise the reader instead of freezing forever.
  if (g_kbWatch.update(sig.c_str(), g_keys.busy(), nowMs)) {
    Serial.printf("[KB] stuck signature '%s' - resyncing keyboard reader\n", sig.c_str());
    g_keyboardResyncs++;
    M5Cardputer.Keyboard.begin();
    g_kbWatch.noteResync();
    g_keys.forceSettle(nowMs);
    g_repeatKey = 0;
    drawCurrentView();
    return;
  }

  if (!g_screenAsleep) {
    auto ev = g_keys.update(pressedNow, wordCount, onlyKey, nowMs);
    if (ev.type == KeyFSM::Event::Ghost) {
      g_ghostIgnored++;
      g_repeatKey = 0;
    } else if (ev.type == KeyFSM::Event::Key) {
      userKey(ev.key);
      if (isNavKey(ev.key)) {
        g_repeatKey = ev.key;
        g_repeatNextMs = nowMs + 550;
      } else {
        g_repeatKey = 0;
      }
    }
  }

  // Menu navigation repeats while an arrow is held; ordinary action keys
  // deliberately do not repeat (holding Q/S/T must never machine-gun).
  if (!pressedNow) {
    g_repeatKey = 0;
  } else if (g_repeatKey && onlyKey == g_repeatKey && nowMs >= g_repeatNextMs) {
    userKey(g_repeatKey);
    g_repeatNextMs = nowMs + 170;
  }

  if (nowMs < 45000) markStage("agent");
  agentLoop();
  if (nowMs < 45000) markStage("scans");
  serviceBackgroundScans();
  if (nowMs < 45000) markStage("lan");
  serviceLanScan();
  if (nowMs < 45000) markStage("lora-step");
  loraHat.pollScan();
  if (nowMs < 45000) markStage("ir");
  if (g_irRemote.pollLearning()) drawCurrentView();
  if (g_irFindActive) {
    if (g_view != VIEW_REMOTE) {
      g_irFindActive = false;
    } else if ((int32_t)(nowMs - g_irFindNextMs) >= 0) {
      g_irFindBrand++;
      if (g_irFindBrand >= IrRemote::kBrandCount) {
        g_irFindActive = false;
        g_irRemote.status = "auto-find done: no brand picked (A retries, C cycles)";
      } else {
        g_irRemote.applyBrand(g_irFindBrand);
        g_irRemote.sendButton(0);   // Power is the most visible test
        g_irRemote.status = String("trying ") + String(g_irFindBrand + 1) + "/" +
                            String(IrRemote::kBrandCount) + " " + IrRemote::brandAt(g_irFindBrand)->name +
                            " - ENTER if TV reacted, v=next";
        g_irFindNextMs = nowMs + 3000;
      }
      drawCurrentView();
    }
  }

  if (!g_streakCleared && nowMs > 90000) {
    prefs.putUInt("bootStreak", 0);
    g_bootSafeMute = false;
    if (g_diagMode) {   // stable run after a crash: diagnostics complete
      prefs.putBool("diag", false);
      prefs.putString("lastStage", "idle");
      g_diagMode = false;
      Serial.println("[CRASH] stable 90 s - diagnostics cleared, self-heal complete");
    }
    g_streakCleared = true;
    Serial.println("[SAFE] stable run - boot streak cleared");
  }
  {
    static uint32_t lastQuickSweep = 0;
    if (nowMs - lastQuickSweep > 600000) {   // 10-minute quick integrity
      lastQuickSweep = nowMs;
      g_loopWatch.expectStall();
      runIntegritySweep(false);
    }
  }
  {
    static uint32_t lastAliveLog = 0;
    if (nowMs - lastAliveLog > 10000) {
      lastAliveLog = nowMs;
      Serial.printf("[LOOP] alive t=%lus view=%d scan=%s worstGap=%lums kbResync=%lu\n",
                    (unsigned long)(nowMs / 1000), (int)g_view, g_scanStatus.c_str(),
                    (unsigned long)g_loopWatch.worstGapMs(), (unsigned long)g_keyboardResyncs);
    }
  }
  if (nowMs < 45000) markStage("draw-ui");
  // Sprite refresh is flicker-free; 1 s keeps uptime and SCAN state honest.
  if (!g_screenAsleep && nowMs - g_lastUiDraw > 1000) drawCurrentView();
  if (nowMs < 45000) markStage("idle");
}
