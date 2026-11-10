// PHOENIX agent — core loop, detectors, expansion hardware and command handling
#pragma once

#include "esp32c5_driver.h"
#include "lora_hat.h"
#include "gnss_hat.h"
#include "rfid2.h"
#include "ble_hid_manager.h"
#include "sd_log_manager.h"
#include "pcap_log.h"

ESP32C5Driver esp32c5;
LoRaHatDriver loraHat;
GnssHatDriver gnssHat;
Rfid2Unit rfid2;
BleHidManager bleHid;
SdLogManager sdLogMgr;

static void heartbeat();

// ---------------------------------------------------------------- helpers
static String sha256Hex(const String& data) {
  unsigned char out[32];
  mbedtls_sha256((const unsigned char*)data.c_str(), data.length(), out, 0);
  char hex[65];
  for (int i = 0; i < 32; i++) snprintf(hex + i * 2, 3, "%02x", out[i]);
  hex[64] = 0;
  return String(hex);
}

static String hmacHex(const String& key, const String& data) {
  unsigned char out[32];
  const mbedtls_md_info_t* md = mbedtls_md_info_from_type(MBEDTLS_MD_SHA256);
  mbedtls_md_hmac(md, (const unsigned char*)key.c_str(), key.length(), (const unsigned char*)data.c_str(), data.length(), out);
  char hex[65];
  for (int i = 0; i < 32; i++) snprintf(hex + i * 2, 3, "%02x", out[i]);
  hex[64] = 0;
  return String(hex);
}

static String macToString(const uint8_t* mac) {
  char buf[18];
  snprintf(buf, sizeof(buf), "%02x:%02x:%02x:%02x:%02x:%02x", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5]);
  return String(buf);
}

static void screen(const String& line1, const String& line2 = "", uint16_t color = TFT_ORANGE) {
  auto& d = M5Cardputer.Display;
  d.fillScreen(TFT_BLACK);
  d.setTextColor(color, TFT_BLACK);
  d.setTextSize(1);
  d.setCursor(4, 4);
  d.println("PHOENIX agent " + String(AGENT_VERSION));
  d.setTextColor(TFT_WHITE, TFT_BLACK);
  d.setCursor(4, 22);  d.println(line1);
  d.setCursor(4, 36);  d.println(line2);
  d.setCursor(4, 110);
  d.setTextColor(quarantined ? TFT_RED : TFT_GREEN, TFT_BLACK);
  d.println(quarantined ? "QUARANTINED  radios muted" : "ARMED  rules: " + String(localRules.size()));
}

static int httpJson(const String& method, const String& path, const String& body, String& response, String* sigHeader = nullptr) {
  WiFiClientSecure secure;
  WiFiClient plain;
  HTTPClient http;
  String url = g_consoleUrl + path;
  bool tls = url.startsWith("https");
  if (tls) { if (g_tlsInsecure) secure.setInsecure(); http.begin(secure, url); } else { http.begin(plain, url); }
  http.addHeader("Content-Type", "application/json");
  http.addHeader("User-Agent", "phoenix-agent/" + String(AGENT_VERSION));
  if (deviceKey.length()) http.addHeader("X-Device-Key", deviceKey);
  const char* collect[] = { "X-Phoenix-Signature", "X-Phoenix-Sha256" };
  http.collectHeaders(collect, 2);
  int code = method == "GET" ? http.GET() : http.POST(body);
  response = http.getString();
  if (sigHeader) *sigHeader = http.header("X-Phoenix-Signature");
  http.end();
  return code;
}

// ---------------------------------------------------------------- MicroSD Config Loader
static void loadSdConfig() {
  // SdLogManager::begin() owns the SPI bus + mount (called first in setup).
  if (!sdLogMgr.mounted) {
    Serial.println("[SD] MicroSD not mounted; using compiled defaults");
    return;
  }
  const char* cfgPath = SD.exists("/AEGIS/config.json") ? "/AEGIS/config.json" : "/phoenix/config.json";
  if (!SD.exists(cfgPath)) {
    Serial.println("[SD] no config.json (/AEGIS or /phoenix); using compiled defaults");
    return;
  }
  File f = SD.open(cfgPath, FILE_READ);
  if (!f) return;
  JsonDocument cfg;
  DeserializationError err = deserializeJson(cfg, f);
  f.close();
  if (err) {
    Serial.println("[SD] Failed to parse /phoenix/config.json");
    return;
  }
  if (cfg["wifi_ssid"].is<const char*>()) g_wifiSsid = cfg["wifi_ssid"].as<String>();
  // The shipped SD template uses placeholders (YOUR_... / CHANGE-ME...).
  // Treating them as real config made boot sit on "joining YOUR_2.4GHZ_SSID"
  // for the full join timeout (v0.7.3 fix: placeholders mean "not set").
  if (g_wifiSsid.startsWith("YOUR_") || g_wifiSsid.startsWith("CHANGE")) g_wifiSsid = "";
  if (cfg["wifi_psk"].is<const char*>()) g_wifiPsk = cfg["wifi_psk"].as<String>();
  if (cfg["console_url"].is<const char*>()) g_consoleUrl = cfg["console_url"].as<String>();
  if (cfg["enroll_token"].is<const char*>()) g_enrollToken = cfg["enroll_token"].as<String>();
  if (cfg["hmac_secret"].is<const char*>()) g_hmacSecret = cfg["hmac_secret"].as<String>();
  if (cfg["tls_insecure"].is<bool>()) g_tlsInsecure = cfg["tls_insecure"].as<bool>();
  if (cfg["vault_passphrase"].is<const char*>()) g_vaultPass = cfg["vault_passphrase"].as<String>();
  if (g_vaultPass.startsWith("CHANGE")) g_vaultPass = "";   // template placeholder = unset
  if (cfg["heartbeat_sec"].is<int>()) heartbeatSec = cfg["heartbeat_sec"].as<int>();
  if (cfg["offline_passive"].is<bool>()) g_passiveOffline = cfg["offline_passive"].as<bool>();
  if (cfg["background_scans"].is<bool>()) g_backgroundScansEnabled = cfg["background_scans"].as<bool>();
  if (cfg["c5_hotswap"].is<bool>()) g_c5HotSwapEnabled = cfg["c5_hotswap"].as<bool>();
  Serial.println("[SD] Loaded credentials and console URL from /phoenix/config.json");
}

// ---- SD rule packs: /AEGIS/rules/*.json, validated before arming -------
static void saveDisabledRules();
static void loadDisabledRules();

static int loadSdRulePacks() {
  g_rulePackFiles = 0;
  int rejected = 0;
  if (!sdLogMgr.mounted) return 0;
  File dir = SD.open("/AEGIS/rules");
  if (!dir) return 0;
  int accepted = 0;
  File f;
  while ((f = dir.openNextFile())) {
    String name = f.name();
    String lower = name; lower.toLowerCase();
    if (!f.isDirectory() && lower.endsWith(".json") && g_rulePackFiles < 8) {
      g_rulePackFiles++;
      JsonDocument doc;
      if (!deserializeJson(doc, f)) {
        JsonArrayConst arr = doc["rules"].is<JsonArrayConst>() ? doc["rules"].as<JsonArrayConst>()
                                                               : doc.as<JsonArrayConst>();
        accepted += localRules.loadRulesValidated(arr, rejected);
      } else {
        rejected++;
      }
    }
    f.close();
  }
  dir.close();
  g_rulePackRejected = rejected;
  Serial.printf("[RULES] SD packs: %d files, %d accepted, %d rejected, %u total armed\n",
                g_rulePackFiles, accepted, rejected, (unsigned)localRules.size());
  if (sdLogMgr.mounted) {
    sdLogMgr.appendLog("aegis_boot.log", String("[RULES] packs=") + g_rulePackFiles +
                       " accepted=" + accepted + " rejected=" + rejected);
  }
  return accepted;
}

// ---- Persistent event index (summaries only; detail stays in the vault) -
static void appendEventIndex(const AegisEventRecord& rec) {
  if (!sdLogMgr.mounted) return;
  if (!SD.exists("/AEGIS/data")) SD.mkdir("/AEGIS/data");
  File f = SD.open("/AEGIS/data/event_index.jsonl", FILE_APPEND);
  if (!f) return;
  JsonDocument doc;
  doc["id"] = rec.id; doc["at"] = rec.atMs; doc["epoch"] = rec.epoch; doc["boot"] = rec.bootId;
  doc["kind"] = rec.kind; doc["ch"] = rec.channel; doc["src"] = rec.source;
  doc["title"] = rec.title; doc["conf"] = rec.confidence; doc["rev"] = rec.reviewed;
  // Tamper-evident hash chain: each line commits to the previous line, so
  // edits/deletions in the plaintext index are detectable off-device.
  static uint32_t chain = 0x811c9dc5u;
  String line; serializeJson(doc, line);
  for (size_t i = 0; i < line.length(); i++) { chain ^= (uint8_t)line[i]; chain *= 0x01000193u; }
  char chainHex[9]; snprintf(chainHex, sizeof(chainHex), "%08x", chain);
  doc["chain"] = chainHex;
  serializeJson(doc, f);
  f.print("\n");
  f.close();
}

static void saveEventIndex() {
  if (!sdLogMgr.mounted) return;
  if (!SD.exists("/AEGIS/data")) SD.mkdir("/AEGIS/data");
  SD.remove("/AEGIS/data/event_index.jsonl");  // FILE_WRITE appends on ESP32 SD
  File f = SD.open("/AEGIS/data/event_index.jsonl", FILE_WRITE);
  if (!f) return;
  // Keep the newest 64 summaries so the index can never grow unbounded.
  size_t first = g_eventHistory.size() > 64 ? g_eventHistory.size() - 64 : 0;
  for (size_t i = first; i < g_eventHistory.size(); i++) {
    const AegisEventRecord& rec = g_eventHistory[i];
    JsonDocument doc;
    doc["id"] = rec.id; doc["at"] = rec.atMs; doc["epoch"] = rec.epoch; doc["boot"] = rec.bootId;
    doc["kind"] = rec.kind; doc["ch"] = rec.channel; doc["src"] = rec.source;
    doc["title"] = rec.title; doc["conf"] = rec.confidence; doc["rev"] = rec.reviewed;
    serializeJson(doc, f);
    f.print("\n");
  }
  f.close();
}

static void loadEventIndex() {
  if (!sdLogMgr.mounted || !SD.exists("/AEGIS/data/event_index.jsonl")) return;
  File f = SD.open("/AEGIS/data/event_index.jsonl", FILE_READ);
  if (!f) return;
  int loaded = 0;
  while (f.available()) {
    String line = f.readStringUntil('\n');
    line.trim();
    if (!line.length()) continue;
    JsonDocument doc;
    if (deserializeJson(doc, line)) continue;
    AegisEventRecord rec;
    rec.id = doc["id"] | 0; rec.atMs = doc["at"] | 0; rec.epoch = doc["epoch"] | 0;
    rec.bootId = doc["boot"] | 0; rec.kind = doc["kind"].as<String>();
    rec.channel = doc["ch"].as<String>(); rec.source = doc["src"].as<String>();
    rec.title = doc["title"].as<String>(); rec.confidence = doc["conf"] | 0;
    rec.reviewed = doc["rev"] | false;
    rec.previousBoot = true;
    if (rec.id >= g_nextEventId) g_nextEventId = rec.id + 1;
    g_eventHistory.push_back(rec);
    if (g_eventHistory.size() > 64) g_eventHistory.erase(g_eventHistory.begin());
    loaded++;
  }
  f.close();
  if (loaded) Serial.printf("[EVENTS] index loaded: %d prior summaries\n", loaded);
}

// ---------------------------------------------------------------- enrolment
static bool enroll() {
  JsonDocument doc;
  doc["enrollToken"] = g_enrollToken;
  doc["serial"] = "CP-" + String((uint32_t)(ESP.getEfuseMac() >> 16), HEX);
  doc["model"] = "Cardputer ADV";
  doc["firmware"] = FIRMWARE_VER;
  doc["agentVersion"] = AGENT_VERSION;
  String body; serializeJson(doc, body);
  String res;
  int code = httpJson("POST", "/api/agent/enroll", body, res);
  if (code != 200 && code != 201) { screen("enrol failed", String(code), TFT_RED); return false; }
  JsonDocument out; deserializeJson(out, res);
  deviceKey = out["deviceKey"].as<String>();
  deviceId = out["deviceId"].as<String>();
  heartbeatSec = out["heartbeatSec"] | 30;
  prefs.putString("deviceKey", deviceKey);
  prefs.putString("deviceId", deviceId);
  screen("enrolled", deviceId);
  return true;
}

// ---------------------------------------------------------------- manifest + modules
static bool installModule(const String& slug, const String& expectedSha, const String& version, String& err) {
  String artifact, sig;
  int code = httpJson("GET", String("/api/agent/modules/") + slug, "", artifact, &sig);
  if (code != 200) { err = "download http " + String(code); return false; }
  if (sha256Hex(artifact) != expectedSha) { err = "sha256 mismatch"; return false; }
  if (g_hmacSecret.length() && hmacHex(g_hmacSecret, artifact) != sig) { err = "signature mismatch"; return false; }
  JsonDocument bundle;
  if (deserializeJson(bundle, artifact)) { err = "artifact is not a rule bundle"; return false; }
  for (JsonObject r : bundle["rules"].as<JsonArray>()) {        // self-test: every rule must parse
    if (!Rule::parses(r["expression"].as<String>())) { err = "rule failed to parse: " + r["code"].as<String>(); return false; }
  }
  prefs.putString(("mod_" + slug).c_str(), artifact);
  bool found = false;
  for (auto& m : installedModules) if (m.slug == slug) { m.version = version; m.sha256 = expectedSha; found = true; }
  if (!found) installedModules.push_back({ slug, version, expectedSha });
  localRules.loadFromBundle(bundle);
  return true;
}

static void rollbackModule(const String& slug) {
  prefs.remove(("mod_" + slug).c_str());
  installedModules.erase(std::remove_if(installedModules.begin(), installedModules.end(), [&](const InstalledModule& m) { return m.slug == slug; }), installedModules.end());
  localRules.clear();
  for (auto& m : installedModules) {
    JsonDocument b; if (!deserializeJson(b, prefs.getString(("mod_" + m.slug).c_str()))) localRules.loadFromBundle(b);
  }
}

static void pullManifest() {
  String res, sig;
  if (httpJson("GET", "/api/agent/manifest", "", res, &sig) != 200) return;
  if (g_hmacSecret.length() && hmacHex(g_hmacSecret, res) != sig) { screen("manifest REJECTED", "bad signature", TFT_RED); return; }
  JsonDocument m; if (deserializeJson(m, res)) return;
  corpSsids.clear();
  for (JsonVariant v : m["sets"]["corp_list"].as<JsonArray>()) corpSsids.push_back(v.as<String>());
  localRules.loadRules(m["rules"].as<JsonArray>());          // console-armed rules run locally too
  heartbeatSec = m["heartbeatSec"] | heartbeatSec;
}

// ---------------------------------------------------------------- telemetry
static void pushEvent(const String& kind, const String& channel, const String& source, const String& title, JsonObject fields) {
  if (quarantined) return;
  // local pre-filter: only ship captures that a local rule fired on, or that have no local rule at all
  bool matchedLocally = localRules.evaluate(kind, fields, corpSsids, knownBssids);
  bool hasRuleForKind = localRules.hasKind(kind);
  if (hasRuleForKind && !matchedLocally) return;
  if (!fields["time_src"].is<const char*>()) {
    fields["time_src"] = gnssHat.currentFix.valid ? "gnss" : "uptime";
    if (gnssHat.currentFix.valid && gnssHat.currentFix.utcTime.length())
      fields["gnss_utc"] = gnssHat.currentFix.utcTime;
  }
  g_lastEventKind = kind; g_lastEventTitle = title; g_lastEventAt = millis(); g_eventsLogged++;
  String fieldsJson; serializeJson(fields, fieldsJson);
  {
    AegisEventRecord rec;
    rec.id = g_nextEventId++;
    rec.atMs = g_lastEventAt;
    {
      const time_t nowEpoch = time(nullptr);
      rec.epoch = nowEpoch > 1700000000 ? (uint32_t)nowEpoch : 0;
    }
    rec.bootId = g_bootStreak;
    rec.kind = kind;
    rec.channel = channel;
    rec.source = source;
    rec.title = title;
    rec.fieldsJson = fieldsJson;
    rec.confidence = fields["confidence"] | 0;
    if (g_eventHistory.size() >= 64) g_eventHistory.erase(g_eventHistory.begin());
    g_eventHistory.push_back(rec);
    appendEventIndex(rec);
    if (rec.confidence >= 80 && !quarantined) {
      g_alertText = rec.title;
      g_alertUntilMs = millis() + 3000;
      if (g_alertBeeps) M5Cardputer.Speaker.tone(1760, 60);
    }
  }
  if (!g_consoleUrl.length() || WiFi.status() != WL_CONNECTED) {
    // Autonomous mode: archive to the encrypted forensic vault on SD.
    String line = kind + "|" + channel + "|" + source + "|" + title + "|" + fieldsJson;
    if (sdLogMgr.mounted) {
      String enc = aegisVault.ready ? aegisVault.encryptLine(line) : "";
      sdLogMgr.appendLog("forensic_vault.log", enc.length() ? enc : ("[VAULT-UNAVAILABLE] " + line));
    }
    return;
  }
  JsonDocument doc;
  JsonArray events = doc["events"].to<JsonArray>();
  JsonObject ev = events.add<JsonObject>();
  ev["kind"] = kind; ev["channel"] = channel; ev["source"] = source; ev["title"] = title;
  ev["fields"] = fields;
  String body; serializeJson(doc, body);
  String res; httpJson("POST", "/api/agent/telemetry", body, res);
}

// Per-second frame-class counters + per-channel density (sniffer-fed,
// evaluated once per second in detectDeauth()). Beacon/probe classes feed
// the beacon-spam and Karma-suspect detectors; channel counts back the
// `AEGIS CHAN` density report.
static volatile uint32_t g_beaconFrames1s = 0;
static volatile uint32_t g_probeResp1s = 0;
static uint32_t g_chanFrames[15] = {0};
static uint32_t g_bssidWin[24] = {0};
static volatile uint8_t g_bssidWinN = 0;

// ---- deauth / KRACK detector: 802.11 promiscuous callback
static void IRAM_ATTR sniffer(void* buf, wifi_promiscuous_pkt_type_t type) {
  const wifi_promiscuous_pkt_t* pkt = (wifi_promiscuous_pkt_t*)buf;
  const uint8_t* frame = pkt->payload;
  if (pkt->rx_ctrl.sig_len < 24) return;  // malformed/short frame: never read MAC offsets OOB
  uint8_t subtype = (frame[0] & 0xF0);
  const uint8_t chan = pkt->rx_ctrl.channel;
  if (chan >= 1 && chan <= 14) g_chanFrames[chan]++;
  pcapCapture(frame, (uint16_t)pkt->rx_ctrl.sig_len, (uint32_t)(pkt->rx_ctrl.timestamp / 1000));
  if (type == WIFI_PKT_MGMT && subtype == 0x80) {                            // beacon
    g_beaconFrames1s++;
    uint32_t h = 0x811c9dc5u;                       // FNV over BSSID (frame+16)
    for (int i = 0; i < 6; i++) { h ^= frame[16 + i]; h *= 0x01000193u; }
    if (g_bssidWinN < 24) g_bssidWin[g_bssidWinN++] = h;
  }
  if (type == WIFI_PKT_MGMT && subtype == 0x50) g_probeResp1s++;             // probe response
  if (type == WIFI_PKT_MGMT && (subtype == 0xC0 || subtype == 0xA0)) {       // deauth / disassoc
    deauthFrames1s++; g_deauthFramesTotal++;
    lastDeauthSource = macToString(frame + 10);
  } else if (type == WIFI_PKT_DATA && pkt->rx_ctrl.sig_len > 40) {
    // EAPOL (0x888E) key message 3 replayed with the same replay counter → KRACK-style
    if (frame[30] == 0x88 && frame[31] == 0x8E && (frame[39] & 0x80) && (frame[39] & 0x08)) eapolMsg3Repeats++;
  }
}

static void detectDeauth() {
  static uint32_t window = 0;
  if (millis() - window < 1000) return;
  uint32_t n = deauthFrames1s; deauthFrames1s = 0;
  uint32_t krack = eapolMsg3Repeats; eapolMsg3Repeats = 0;
  window = millis();
  if (n > 0) {
    JsonDocument d; JsonObject f = d.to<JsonObject>();
    f["wifi.mgmt"] = 0xC0; f["count(window=1s)"] = (int)n; f["confidence"] = n > 100 ? 95 : 75;
    pushEvent("deauth_flood", "wifi", lastDeauthSource, "Deauthentication burst on current channel", f);
  }
  if (krack > 1) {
    JsonDocument d; JsonObject f = d.to<JsonObject>();
    f["wifi.eapol.msg"] = 3; f["repeat(nonce)"] = (int)krack; f["confidence"] = 70;
    pushEvent("wifi_krack", "wifi", "", "Repeated EAPOL message 3 (key reinstall attempt)", f);
  }
  uint32_t beacons = g_beaconFrames1s; g_beaconFrames1s = 0;
  uint32_t probeResp = g_probeResp1s; g_probeResp1s = 0;
  int uniqueBssid = 0;                                  // distinct hashes this second
  for (uint8_t i = 0; i < g_bssidWinN; i++) {
    bool dup = false;
    for (uint8_t j = 0; j < i; j++) if (g_bssidWin[j] == g_bssidWin[i]) { dup = true; break; }
    if (!dup) uniqueBssid++;
  }
  g_bssidWinN = 0;
  if (beacons > 400 && uniqueBssid > 16) {              // beacon flood / spam tooling
    JsonDocument d; JsonObject f = d.to<JsonObject>();
    f["wifi.beacon(per_s)"] = (int)beacons; f["wifi.unique_bssid"] = uniqueBssid; f["confidence"] = 65;
    pushEvent("beacon_spam", "wifi", "", "Beacon flood from many BSSIDs (spam/impersonation tooling)", f);
  }
  if (probeResp > 200) {                                // answers to everything = Karma-style
    JsonDocument d; JsonObject f = d.to<JsonObject>();
    f["wifi.probe_resp(per_s)"] = (int)probeResp; f["confidence"] = 60;
    pushEvent("karma_suspect", "wifi", "", "Excess probe responses (possible Karma impersonation)", f);
  }
}

// ---- evil twin / rogue AP detector: asynchronous periodic scan -------
// WiFi.scanNetworks(false, ...) used to block the only loop for seconds at
// the 20 s cycle. That starved M5Cardputer.update(), overflowed the
// TCA8418 FIFO during button mashing, and made the sprite look frozen.
// The scan now runs in the radio background; only completed results are
// processed, after keyboard input has already been serviced.
static bool g_wifiScanRunning = false;
static bool g_bleScanRunning = false;
static bool g_scanPending = false;
static bool g_scanManualPending = false;
static uint32_t g_wifiScanStartedMs = 0;
static uint32_t g_bleScanEndsMs = 0;
static uint32_t g_lastBackgroundScanMs = 0;

static void bootStageNow(const char* stage);  // defined with bootStage() below
// ---- Shadow store (v0.8.3, user request) --------------------------------
// One AES-256-GCM encrypted envelope on SD (/AEGIS/shadow.dat, key =
// PBKDF2-HMAC-SHA256(password, salt, 60000) via AesVault). Holds the
// info worth recalling across boots: Wi-Fi credentials and the learned
// baseline (known BSSIDs / corp SSIDs), so rogue-AP detection works
// from the first scan instead of relearning every boot. First use sets
// the password (DEVICES screen, U key); nothing is readable without it.
static AesVault g_shadowVault;
static int g_shadowState = 0;   // 0 = no file, 1 = locked, 2 = unlocked
static String g_shadowMsg = "";
static void shadowRefreshState() {
  g_shadowState = g_shadowVault.ready ? 2 :
      (sdLogMgr.mounted && SD.exists("/AEGIS/shadow.dat") ? 1 : 0);
}
static bool shadowSave() {
  if (!g_shadowVault.ready || !sdLogMgr.mounted) return false;
  JsonDocument d;
  d["wifi_ssid"] = g_wifiSsid;
  d["wifi_psk"] = g_wifiPsk;
  JsonArray kb = d["known_bssids"].to<JsonArray>();
  for (auto& b : knownBssids) kb.add(b);
  JsonArray cs = d["corp_ssids"].to<JsonArray>();
  for (auto& x : corpSsids) cs.add(x);
  String plain; serializeJson(d, plain);
  const String env = g_shadowVault.encryptLine(plain);
  if (!env.length()) return false;
  File f = SD.open("/AEGIS/shadow.dat", FILE_WRITE);
  if (!f) return false;
  f.print(env); f.close();
  g_shadowState = 2;
  return true;
}
static bool shadowUnlock(const String& pass) {
  File f = SD.open("/AEGIS/shadow.dat", FILE_READ);
  if (!f) { g_shadowMsg = "no shadow file yet (U sets one)"; return false; }
  const String env = f.readString(); f.close();
  if (!g_shadowVault.begin(pass)) { g_shadowMsg = g_shadowVault.lastError; return false; }
  String plain;
  if (!g_shadowVault.decryptLine(env, plain)) {
    g_shadowVault = AesVault();
    shadowRefreshState();
    g_shadowMsg = "wrong password";
    return false;
  }
  JsonDocument d;
  if (deserializeJson(d, plain)) { g_shadowMsg = "shadow file corrupt"; return false; }
  const String ssid = d["wifi_ssid"] | "";
  const String psk = d["wifi_psk"] | "";
  if (ssid.length() && !g_wifiSsid.length()) { g_wifiSsid = ssid; g_wifiPsk = psk; }
  for (JsonVariant v : d["known_bssids"].as<JsonArray>()) {
    const String b = v.as<String>();
    if (std::find(knownBssids.begin(), knownBssids.end(), b) == knownBssids.end() &&
        knownBssids.size() < 200) knownBssids.push_back(b);
  }
  for (JsonVariant v : d["corp_ssids"].as<JsonArray>()) {
    const String x = v.as<String>();
    if (std::find(corpSsids.begin(), corpSsids.end(), x) == corpSsids.end()) corpSsids.push_back(x);
  }
  if (!knownBssids.empty()) g_baselineLearned = true;   // recalled baseline
  g_shadowState = 2;
  g_shadowMsg = "unlocked - saved info recalled";
  if (g_wifiSsid.length() && WiFi.status() != WL_CONNECTED && !g_passiveOffline)
    WiFi.begin(g_wifiSsid.c_str(), g_wifiPsk.c_str());
  Serial.printf("[SHADOW] unlocked: %u known BSSIDs recalled\n", (unsigned)knownBssids.size());
  return true;
}
static bool shadowCreate(const String& pass) {
  if (!g_shadowVault.begin(pass)) { g_shadowMsg = g_shadowVault.lastError; return false; }
  if (!shadowSave()) { g_shadowMsg = "could not write shadow file"; return false; }
  g_shadowMsg = "shadow created + unlocked";
  return true;
}
static void shadowLock() {
  g_shadowVault = AesVault();
  shadowRefreshState();
  g_shadowMsg = "locked";
}
// Accessors so main.cpp (which includes this header later) can drive the
// password-entry flow without touching the vault object directly.
static int shadowState() { return g_shadowState; }
static bool shadowIsUnlocked() { return g_shadowVault.ready; }
static void shadowSetMsg(const String& m) { g_shadowMsg = m; }

static void processWifiScanResults(int n) {
  if (quarantined || n < 0) return;
  int bestCorpRssi = -100;
  for (int i = 0; i < n; i++) for (auto& s : corpSsids) if (WiFi.SSID(i) == s && WiFi.RSSI(i) > bestCorpRssi) bestCorpRssi = WiFi.RSSI(i);
  for (int i = 0; i < n; i++) {
    String bssid = WiFi.BSSIDstr(i); bssid.toLowerCase();
    bool known = std::find(knownBssids.begin(), knownBssids.end(), bssid) != knownBssids.end();
    bool corpSsid = std::find(corpSsids.begin(), corpSsids.end(), WiFi.SSID(i)) != corpSsids.end();
    if (g_baselineLearned && corpSsid && !known) {
      JsonDocument d; JsonObject f = d.to<JsonObject>();
      f["wifi.ssid"] = WiFi.SSID(i); f["wifi.bssid"] = bssid; f["rssi_delta"] = WiFi.RSSI(i) - bestCorpRssi + 7;
      f["wifi.open"] = WiFi.encryptionType(i) == WIFI_AUTH_OPEN; f["confidence"] = 80;
      pushEvent("evil_twin", "wifi", bssid, "Corporate SSID from an unknown BSSID", f);
    } else if (g_baselineLearned && !known && knownBssids.size() > 0) {
      JsonDocument d; JsonObject f = d.to<JsonObject>();
      f["ap.unknown"] = true; f["wifi.ssid"] = WiFi.SSID(i); f["wifi.channel"] = WiFi.channel(i); f["confidence"] = 55;
      pushEvent("rogue_ap", "wifi", bssid, "New access point appeared on site", f);
    }
    if (!g_baselineLearned && knownBssids.size() < 200 && !known) knownBssids.push_back(bssid);  // baseline pass only
  }
  if (sdLogMgr.mounted) {
    // Every scan feeds the persistent Wi-Fi Atlas (channels per network);
    // wardrive records are deduplicated by the WardriveStore.
    for (int i = 0; i < n; i++) {
      bool fix = gnssHat.currentFix.valid;
      g_wifiAtlas.observe(WiFi.SSID(i), WiFi.BSSIDstr(i), WiFi.channel(i), WiFi.RSSI(i),
                          fix ? gnssHat.currentFix.latitude : 0, fix ? gnssHat.currentFix.longitude : 0,
                          fix, millis() / 1000);
      if (g_wardriveOn) {
        String rec;
        if (g_wardriveStore.buildRecord(WiFi.SSID(i), WiFi.BSSIDstr(i), WiFi.channel(i), WiFi.RSSI(i),
                                        (int)WiFi.encryptionType(i),
                                        fix ? gnssHat.currentFix.latitude : 0, fix ? gnssHat.currentFix.longitude : 0,
                                        gnssHat.currentFix.satellites, fix, millis(), rec)) {
          sdLogMgr.appendLog("wardrive.jsonl", rec);
          String ssid = WiFi.SSID(i);
          ssid.replace(",", "_"); ssid.replace("\"", "_");
          sdLogMgr.appendLog("wardrive.csv", String(millis() / 1000) + "," + ssid + "," + WiFi.BSSIDstr(i) + "," +
                             String(WiFi.channel(i)) + "," + String(WiFi.RSSI(i)));
        }
      }
    }
  }
  WiFi.scanDelete();
  if (!g_baselineLearned) {
    g_baselineLearned = true;
    Serial.printf("[APS] baseline learned: %u known BSSIDs\n", (unsigned)knownBssids.size());
    if (g_shadowVault.ready) shadowSave();   // persist baseline into the shadow
  }
}


// ---- BLE stalker detector: random-address advertisers that keep following us
struct BleSeen { uint32_t firstSeen; uint32_t lastSeen; int sightings; };
static std::map<String, BleSeen> bleTable;
static char g_skimmerPending[40] = "";
static String g_skimmerPendingName;
class BleCb : public NimBLEScanCallbacks {
  void onResult(const NimBLEAdvertisedDevice* dev) override {
    if (dev->getAddressType() != BLE_ADDR_RANDOM) return;
    String key = dev->getAddress().toString().c_str();
    auto& e = bleTable[key];
    if (!e.firstSeen) {
      // Card-skimmer fingerprint: cheap serial/BLE bridge modules (HC-05,
      // HC-06, BT05) are the classic skimmer radios (Marauder heuristic).
      // Only RECORD it here - this runs in the NimBLE host task, and
      // pushEvent() does SD I/O which is not callback-safe (v0.7.2 fix:
      // deferred to evaluateBleScan() in the main loop).
      String nm = dev->getName().c_str();
      if (nm.indexOf("HC-05") >= 0 || nm.indexOf("HC-06") >= 0 || nm.indexOf("BT05") >= 0) {
        snprintf(g_skimmerPending, sizeof(g_skimmerPending), "%s", key.c_str());
        g_skimmerPendingName = nm;
      }
    }
    if (!e.firstSeen) e.firstSeen = millis();
    e.lastSeen = millis(); e.sightings++;
  }
};
static void evaluateBleScan() {
  if (quarantined) return;
  if (g_skimmerPending[0]) {           // deferred from the BLE callback
    JsonDocument d; JsonObject f = d.to<JsonObject>();
    f["ble.name"] = g_skimmerPendingName; f["confidence"] = 50;
    pushEvent("ble_skimmer_suspect", "ble", String(g_skimmerPending),
              "Possible card-skimmer radio nearby (serial-bridge BLE module)", f);
    g_skimmerPending[0] = 0;
  }
  for (auto& [addr, e] : bleTable) {
    uint32_t minutes = (e.lastSeen - e.firstSeen) / 60000;
    if (minutes >= 15 && e.sightings > 20) {
      JsonDocument d; JsonObject f = d.to<JsonObject>();
      f["ble.adv.rotating"] = true; f["proximity.follow(minutes)"] = (int)minutes; f["sightings"] = e.sightings; f["confidence"] = 70;
      pushEvent("ble_stalker", "ble", addr, "Rotating-address BLE advertiser persisting nearby", f);
      e.firstSeen = millis(); e.sightings = 0;
    }
  }
  if (bleTable.size() > 300) bleTable.clear();
}

static void startBleScan() {
  bootStageNow("sc-blestart");
  if (quarantined) {
    g_scanActive = false;
    g_scanStatus = "quarantined";
    return;
  }
  NimBLEScan* scan = NimBLEDevice::getScan();
  if (!scan) {
    g_scanActive = false;
    g_scanStatus = "BLE unavailable";
    return;
  }
  // NimBLE-Arduino 2.x start() is non-blocking; BleCb collects results.
  if (scan->start(3000, false, true)) {
    g_bleScanRunning = true;
    g_bleScanEndsMs = millis() + 3000;
    g_scanStatus = "BLE scanning";
  } else {
    g_scanActive = false;
    g_scanStatus = "BLE scan failed";
  }
}

static void requestBackgroundScan() {
  if (!quarantined) g_scanManualPending = true;
}

// Called from the main loop AFTER keyboard handling. It starts background
// work only during an input-idle window, but always polls work in flight.
static void serviceBackgroundScans() {
  const uint32_t now = millis();

  if (quarantined) {
    if (g_wifiScanRunning) WiFi.scanDelete();
    if (g_bleScanRunning) {
      NimBLEScan* scan = NimBLEDevice::getScan();
      if (scan) scan->stop();
    }
    g_wifiScanRunning = false;
    g_bleScanRunning = false;
    g_scanPending = false;
    g_scanActive = false;
    g_scanStatus = "quarantined";
    return;
  }

  if (!g_backgroundScansEnabled && !g_scanManualPending && !g_wifiScanRunning && !g_bleScanRunning) {
    g_scanPending = false;
    g_scanActive = false;
    g_scanStatus = "background scans off";
    return;
  }

  if (g_wifiScanRunning) {
    const int n = WiFi.scanComplete();
    if (n >= 0) {
      g_wifiScanRunning = false;
      g_liveAps = n;
      g_scanStatus = "processing APs";
      bootStageNow("sc-wifiproc");
      processWifiScanResults(n);
      bootStageNow("sc-wifidone");
      startBleScan();
    } else if (n == WIFI_SCAN_FAILED) {
      g_wifiScanRunning = false;
      WiFi.scanDelete();
      Serial.println("[SCAN] WiFi async scan failed");
      startBleScan();
    } else if (now - g_wifiScanStartedMs > 20000) {
      g_wifiScanRunning = false;
      WiFi.scanDelete();
      Serial.println("[SCAN] WiFi async scan timed out");
      g_scanStatus = "WiFi scan timeout";
      startBleScan();
    }
    return;
  }

  if (g_bleScanRunning) {
    if ((int32_t)(now - g_bleScanEndsMs) >= 0) {
      NimBLEScan* scan = NimBLEDevice::getScan();
      if (scan) scan->stop();
      g_bleScanRunning = false;
      bootStageNow("sc-bleeval");
      evaluateBleScan();
      bootStageNow("sc-done");
      g_lastBackgroundScanMs = now;
      g_scanActive = false;
      g_scanStatus = "scan complete";
      Serial.println("[SCAN] background WiFi+BLE scan complete");
    }
    return;
  }

  if (!g_scanPending && now - g_lastBackgroundScanMs >= 20000 &&
      now - g_lastKeyActivityMs > 3000 && !M5Cardputer.Keyboard.isPressed()) {
    g_scanPending = true;
  }
  if (!g_scanPending && !g_scanManualPending) return;
  if (M5Cardputer.Keyboard.isPressed() || now - g_lastKeyActivityMs <= 1500) return;

  bootStageNow("sc-kick");
  g_scanPending = false;
  g_scanManualPending = false;
  g_scanActive = true;
  g_scanStatus = "WiFi scanning";
  // Offline-passive means exactly that on Wi-Fi too: listen on each
  // channel instead of transmitting active probe requests.
  const int rc = WiFi.scanNetworks(true, true, g_passiveOffline, g_passiveOffline ? 250 : 120);
  if (rc == WIFI_SCAN_RUNNING) {
    g_wifiScanRunning = true;
    g_wifiScanStartedMs = now;
    Serial.println("[SCAN] async WiFi scan started");
  } else if (rc >= 0) {
    processWifiScanResults(rc);
    startBleScan();
  } else {
    Serial.printf("[SCAN] async WiFi scan start failed rc=%d\n", rc);
    g_scanStatus = "WiFi scan failed";
    startBleScan();
  }
}

// ---------------------------------------------------------------- commands
static void runCommand(JsonObject cmd) {
  String id = cmd["id"].as<String>();
  String kind = cmd["kind"].as<String>();
  JsonObject p = cmd["payload"].as<JsonObject>();
  String err; bool ok = true;
  if (kind == "install_module") {
    ok = installModule(p["slug"].as<String>(), p["sha256"].as<String>(), p["version"].as<String>(), err);
  } else if (kind == "rollback_module") {
    rollbackModule(p["slug"].as<String>());
  } else if (kind == "quarantine") {
    quarantined = true; esp_wifi_set_promiscuous(false); NimBLEDevice::getScan()->stop();
  } else if (kind == "release") {
    quarantined = false; esp_wifi_set_promiscuous(true);
  } else if (kind == "reboot") {
    acks.push_back({ id, true, "rebooting" }); heartbeat(); delay(200); ESP.restart();
  } else if (kind == "block_source" || kind == "isolate_segment" || kind == "broadcast_ioc") {
    blockedSources.push_back(p["source"].as<String>());           // IOC kept for local correlation
  } else if (kind == "deauth_counter") {
    // Defensive response only: raise PMF-required flag + log; this firmware never transmits attack frames.
    prefs.putBool("pmfRequired", true);
  } else if (kind == "flash_firmware") {
    String url = p["url"].as<String>();
    acks.push_back({ id, true, "ota starting" }); heartbeat();
    WiFiClientSecure c; if (g_tlsInsecure) c.setInsecure();
    httpUpdate.rebootOnUpdate(true);
    t_httpUpdate_return r = httpUpdate.update(c, url);       // console 302s to the signed .bin; 404 = keep image
    ok = r == HTTP_UPDATE_OK; err = httpUpdate.getLastErrorString();
    if (!ok) { acks.push_back({ id, false, "ota: " + err }); }
    return;
  } else if (kind == "report_now") {
    requestBackgroundScan();
  } else if (kind == "ble_pair_hid") {
    bleHid.startPairingScan(p["durationSec"] | 10);
    ok = true;
  } else if (kind == "scan_5ghz") {
    std::vector<String> ss, bs; std::vector<int> rs;
    esp32c5.scan5Ghz(ss, bs, rs);
    ok = true;
  } else if (kind == "connect_5ghz") {
    ok = esp32c5.connect5Ghz(p["ssid"].as<String>(), p["psk"].as<String>());
  } else { ok = false; err = "unknown command"; }
  acks.push_back({ id, ok, ok ? kind + " applied" : err });
  screen(kind, ok ? "ok" : err, ok ? TFT_GREEN : TFT_RED);
}

// ---------------------------------------------------------------- heartbeat
static void heartbeat() {
  if (g_passiveOffline || !g_consoleUrl.length() || WiFi.status() != WL_CONNECTED) return;
  JsonDocument doc;
  doc["firmware"] = FIRMWARE_VER;
  doc["agentVersion"] = AGENT_VERSION;
  doc["battery"] = M5Cardputer.Power.getBatteryLevel();
  doc["signal"] = constrain(2 * (WiFi.RSSI() + 100), 0, 100);
  doc["uptimeHours"] = (millis() - bootMillis) / 3600000.0;
  doc["has5Ghz"] = esp32c5.active;
  doc["hasLora"] = loraHat.active;
  doc["hasGnss"] = gnssHat.active;
  doc["hasBleHid"] = bleHid.active && (bleHid.pairedDevices.size() > 0);
  JsonArray bleList = doc["blePairedDevices"].to<JsonArray>();
  for (const auto& dev : bleHid.getPairedDeviceSummaries()) bleList.add(dev);
  if (gnssHat.currentFix.valid) {
    JsonObject g = doc["gnssCoords"].to<JsonObject>();
    g["lat"] = gnssHat.currentFix.latitude;
    g["lon"] = gnssHat.currentFix.longitude;
    g["alt"] = gnssHat.currentFix.altitude;
    g["sats"] = gnssHat.currentFix.satellites;
    g["speed"] = gnssHat.currentFix.speedKnots;
    g["fix"] = true;
  }
  JsonObject sd = doc["sdCardStatus"].to<JsonObject>();
  sd["mounted"] = sdLogMgr.mounted;
  sd["totalMb"] = sdLogMgr.getTotalMb();
  sd["freeMb"] = sdLogMgr.getFreeMb();
  JsonArray inst = doc["installed"].to<JsonArray>();
  for (auto& m : installedModules) { JsonObject o = inst.add<JsonObject>(); o["slug"] = m.slug; o["version"] = m.version; }
  JsonArray ak = doc["acks"].to<JsonArray>();
  for (auto& a : acks) { JsonObject o = ak.add<JsonObject>(); o["id"] = a.id; o["ok"] = a.ok; o["result"] = a.result; }
  acks.clear();
  String body; serializeJson(doc, body);
  String res;
  int code = httpJson("POST", "/api/agent/heartbeat", body, res);
  if (code == 401) { prefs.remove("deviceKey"); deviceKey = ""; enroll(); return; }   // key rotated → re-enrol
  if (code != 200) return;
  JsonDocument out; if (deserializeJson(out, res)) return;
  heartbeatSec = out["heartbeatSec"] | heartbeatSec;
  if (out["manifestChanged"] | false) pullManifest();
  for (JsonObject cmd : out["commands"].as<JsonArray>()) runCommand(cmd);
  if (out["commands"].size()) heartbeat();      // ack promptly so the console reflects the result
}

// ---------------------------------------------------------------- lifecycle
static String g_bootStage = "boot";   // shown on screen for the first 45 s (no-serial diagnostics)
static String g_prevStage = "none";   // stage the PREVIOUS boot last reached (read from NVS)
static bool g_diagMode = false;       // true only after a detected crash (watchdog/panic restart)
static bool g_censusDone = false;     // boot device census finished (20 s window)
static uint32_t g_censusStartMs = 0;
static bool g_gnssOff = false;        // census: no NMEA seen -> GNSS polling off
static bool g_chanHop = true;         // sniffer channel hopping (1..13)
static uint8_t g_sniffChannel = 1;
static bool g_c5Off = false;          // census: no C5 hello -> C5 probing off
static uint32_t g_crashCount = 0;
static String g_healNote = "";        // self-healing action taken for this boot
static void markStage(const char* stage) {
  // Draw-before-call marker: during the first 75 s, paint the stage name
  // directly on the LCD BEFORE the guarded call runs. If the loop hangs
  // inside that call, sprite pushes stop and this marker stays on screen,
  // naming the culprit on the frozen display itself (v0.7.6; the throttled
  // NVS sampling in v0.7.4/0.7.5 could phase-lock onto a wrong stage).
  g_bootStage = stage;
  // Yellow stage markers appear ONLY in crash-diagnostics mode (v0.7.9):
  // after a watchdog/panic restart they run for up to 5 min while the
  // device gathers crash data; normal boots show nothing. The stage is
  // always recorded to NVS silently either way (see bootStage()).
  if (!g_diagMode || millis() > 300000) return;
  auto& d = M5Cardputer.Display;
  d.setTextSize(1);
  d.setTextColor(0xFFE0, 0x0000);
  d.fillRect(138, 124, 102, 11, 0x0000);
  d.drawString(stage, 140, 126);
}
static void bootStageNow(const char* stage) {   // immediate NVS persist, scan substages only
  markStage(stage);
  if (millis() < 90000) prefs.putString("lastStage", stage);
}
static void bootStage(const char* stage) {
  if (millis() > 60000 || g_bootStage == stage) return;
  g_bootStage = stage;
  static uint32_t lastPersistMs = 0;   // throttle: NVS writes are slow flash ops
  if (millis() - lastPersistMs >= 500) {
    lastPersistMs = millis();
    prefs.putString("lastStage", stage);   // survives the power-cycle the user does anyway
  }
}
void agentSetup() {
  auto cfg = M5.config();
  M5Cardputer.begin(cfg);
  Serial.begin(115200);
  setCpuFrequencyMhz(80);   // power trim (v0.8.1): plenty for UI + radios
  bootMillis = millis();
  prefs.begin("phoenix", false);
  g_passiveOffline = prefs.getBool("passiveOff", false);
  g_backgroundScansEnabled = prefs.getBool("bgScans", true);
  g_liveEnabled = prefs.getBool("liveLink", true);
  g_autoSync = prefs.getBool("autoSync", true);
  g_chanHop = prefs.getBool("chanHop", true);
  g_c5HotSwapEnabled = prefs.getBool("c5HotSwap", true);
  g_brightnessPct = prefs.getUInt("bright", 80);
  g_screenSleepSec = prefs.getUInt("sleepSec", 0);
  g_alertBeeps = prefs.getBool("alertBeep", true);
  g_irSavedBrand = prefs.getInt("irBrand", -1);
  g_prevStage = prefs.getString("lastStage", "none");
  // Crash detection (v0.7.9): a watchdog/panic reset reason means the last
  // boot crashed. Only then does the device enter diagnostics mode: the
  // yellow stage markers appear, the splash reports where it died, and
  // self-healing for that site is applied. A clean 90 s run clears it.
  {
    const esp_reset_reason_t rr = esp_reset_reason();
    const bool crashed = (rr == ESP_RST_TASK_WDT || rr == ESP_RST_INT_WDT ||
                          rr == ESP_RST_PANIC || rr == ESP_RST_WDT);
    g_crashCount = prefs.getUInt("crashCnt", 0);
    if (crashed) {
      g_crashCount++;
      prefs.putUInt("crashCnt", g_crashCount);
      prefs.putBool("diag", true);
      Serial.printf("[CRASH] reset_reason=%d prev_stage=%s crash #%u - diagnostics ON\n",
                    (int)rr, g_prevStage.c_str(), (unsigned)g_crashCount);
    }
    g_diagMode = prefs.getBool("diag", false);
    if (g_diagMode) {
      if (g_prevStage == "rfid-poll") g_healNote = "RFID probe forced OFF";
      else if (g_prevStage == "scans" || g_prevStage.startsWith("sc-")) {
        g_backgroundScansEnabled = false;   // this boot only; user pref kept
        g_healNote = "background scans paused this boot";
      } else if (g_prevStage == "lora-poll" || g_prevStage == "lora-step")
        g_healNote = "LoRa radio state reset";
      else if (g_prevStage != "none" && g_prevStage != "idle")
        g_healNote = "crash site logged for review";
    }
  }
  // RFID is an OPT-IN toggle (v0.7.8, user request): most Cardputers have
  // no RFID2 reader, and probing for one hung the UI (v0.7.6/0.7.7 field
  // reports). Default OFF - the driver then never touches the I2C bus.
  // If a previous boot died at rfid-poll, force the toggle back OFF.
  if (g_prevStage == "rfid-poll") prefs.putBool("rfidOn", false);
  rfid2.enabled = prefs.getBool("rfidOn", false);
  Serial.printf("[RFID] toggle %s (SENSORS view, R key, or AEGIS RFID ON/OFF)\n",
                rfid2.enabled ? "ON" : "OFF");
  // Deterministic freeze evidence: persist "rfid-poll" immediately before
  // every probe attempt (max once per 30 s), so a hang inside the probe is
  // provable next boot regardless of the throttled stage sampling.
  rfid2.onProbeStart = []() { prefs.putString("lastStage", "rfid-poll"); };
  g_bootStreak = prefs.getUInt("bootStreak", 0) + 1;
  prefs.putUInt("bootStreak", g_bootStreak);
  if (g_bootStreak >= 3) {
    g_safeMode = true;
    quarantined = true;
    Serial.printf("[SAFE] boot streak %u - SAFE MODE (expansion + radios muted)\n", (unsigned)g_bootStreak);
  }
  Wire.setTimeOut(50);   // a wedged peripheral must never hang the loop on I2C
  g_censusStartMs = millis();
  sdLogMgr.begin(12);
  shadowRefreshState();
  Serial.printf("[SHADOW] state=%s (DEVICES > U to %s)\n",
                g_shadowState == 2 ? "unlocked" : g_shadowState == 1 ? "locked" : "not set",
                g_shadowState == 0 ? "set password" : "unlock");
  loadSdConfig();
  loadEventIndex();
  if (sdLogMgr.mounted && g_wifiAtlas.load()) Serial.printf("[ATLAS] %u saved networks loaded\n", (unsigned)g_wifiAtlas.entries.size());
  if (g_vaultPass.length()) {
    screen("unlocking vault", "PBKDF2 key derivation...", TFT_CYAN);
    aegisVault.setPassphrase(g_vaultPass);
    if (aegisVault.begin(g_vaultPass)) Serial.println("[VAULT] AES-256-GCM forensic vault ready");
    else Serial.println("[VAULT] init failed: " + aegisVault.lastError);
  }

  // ---- Boot self-tests: break-it-here, not in the field -----------------
  {
    bool ruleOk = Rule::parses("wifi.mgmt == 0xC0 && count(window=1s) > 40");
    bool ruleBadRejected = !Rule::parses("wifi.mgmt ==") && !Rule::parses("(((") && !Rule::parses("");
    Serial.printf("[SELFTEST] rules valid-accepted=%d invalid-rejected=%d\n", ruleOk, ruleBadRejected);
    if (!ruleOk || !ruleBadRejected) Serial.println("[SELFTEST] RULE PARSER FAULT");
    if (aegisVault.ready) {
      String enc = aegisVault.encryptLine("AEGIS-SELFTEST-PROBE");
      String dec;
      bool roundTrip = enc.length() && aegisVault.decryptLine(enc, dec) && dec == "AEGIS-SELFTEST-PROBE";
      String tampered = enc;
      if (tampered.length() > 4) tampered.setCharAt(tampered.length() - 2, tampered.charAt(tampered.length() - 2) == 'A' ? 'B' : 'A');
      String junk;
      bool tamperRejected = !aegisVault.decryptLine(tampered, junk);
      Serial.printf("[SELFTEST] AES-256-GCM roundtrip=%d tamper-rejected=%d\n", roundTrip, tamperRejected);
      if (!roundTrip || !tamperRejected) { Serial.println("[SELFTEST] VAULT FAULT - disabling vault"); aegisVault.ready = false; }
    }
  }
  deviceKey = prefs.getString("deviceKey", "");
  deviceId = prefs.getString("deviceId", "");
  WiFi.mode(WIFI_STA);
  bool online = false;
  if (g_passiveOffline) {
    WiFi.disconnect(false, false);
    screen("passive offline mode", "RX + scans; no WiFi join", TFT_CYAN);
    Serial.println("[SETUP] passive offline: radio RX/scans on, association off");
  } else if (g_wifiSsid.length()) {
    WiFi.begin(g_wifiSsid.c_str(), g_wifiPsk.c_str());
    uint32_t wifiStart = millis();
    while (WiFi.status() != WL_CONNECTED && millis() - wifiStart < 6000) {
      screen("joining " + g_wifiSsid, "wait " + String(6 - (millis() - wifiStart) / 1000) + "s (autonomous if offline)", TFT_CYAN);
      delay(250); M5Cardputer.update();
    }
    online = WiFi.status() == WL_CONNECTED;
  }
  if (online) configTime(0, 0, "pool.ntp.org", "time.nist.gov");  // real timestamps for events
  if (online && g_consoleUrl.length()) {
    if (!deviceKey.length()) enroll();
    pullManifest();
  } else {
    screen("autonomous mode", g_consoleUrl.length() ? "wifi offline" : "no console set", TFT_YELLOW);
  }
  for (auto& m : installedModules) { JsonDocument b; if (!deserializeJson(b, prefs.getString(("mod_" + m.slug).c_str()))) localRules.loadFromBundle(b); }
  loadSdRulePacks();
  loadDisabledRules();

#if AEGIS_ENABLE_C5
  // Hot-swap: never block boot waiting for a board that may be plugged
  // in later. poll() probes Grove G1/G2 and auto-starts 5 GHz work.
  if (g_c5HotSwapEnabled && !g_safeMode) {
    esp32c5.beginHotSwap(AEGIS_C5_RX_PIN, AEGIS_C5_TX_PIN, AEGIS_C5_BAUD);
  } else {
    Serial.println("[C5] hot-swap disabled (setup toggle or safe mode)");
  }
#endif

  Serial.println("[INIT] promiscuous sniffer...");
  esp_wifi_set_promiscuous_rx_cb(&sniffer);
  esp_wifi_set_promiscuous(true);
  Serial.println("[INIT] NimBLE init...");
  NimBLEDevice::init("aegis-sensor");
  bleHid.begin();
  NimBLEDevice::getScan()->setScanCallbacks(new BleCb(), false);
  NimBLEDevice::getScan()->setActiveScan(false);
  Serial.println("[INIT] core setup complete");
  heartbeat();
  screen(online ? "online autonomous" : "offline autonomous",
         g_wifiSsid.length() ? g_wifiSsid : String("local only"), TFT_GREEN);

  // Expansion init LAST, with markers: if a driver ever hangs, the serial
  // log and the already-drawn status screen show exactly where.
#if AEGIS_ENABLE_LORA
  Serial.println("[INIT] LoRa begin...");
  if (!g_safeMode) loraHat.begin(); else Serial.println("[SAFE] LoRa skipped");
  if (!g_safeMode) rfid2.begin();  // inert when no WS1850S reader is attached
  Serial.printf("[INIT] LoRa done, active=%d\n", loraHat.active);
#endif
#if AEGIS_ENABLE_GNSS
  Serial.println("[INIT] GNSS begin...");
  if (!g_safeMode) gnssHat.begin(AEGIS_GPS_RX_PIN, AEGIS_GPS_TX_PIN, AEGIS_GPS_BAUD); else Serial.println("[SAFE] GNSS skipped");
  Serial.println("[INIT] GNSS UART started (active when NMEA arrives)");
#endif
  Serial.println("[INIT] setup complete - entering main loop");
}

// Disabled rule codes persist as one code per line on SD.
static void saveDisabledRules() {
  if (!sdLogMgr.mounted) return;
  if (!SD.exists("/AEGIS/rules")) SD.mkdir("/AEGIS/rules");
  File f = SD.open("/AEGIS/rules/disabled.txt", FILE_WRITE);
  if (!f) return;
  for (auto& c : localRules.disabled) f.println(c);
  f.close();
}
static void loadDisabledRules() {
  if (!sdLogMgr.mounted) return;
  File f = SD.open("/AEGIS/rules/disabled.txt", FILE_READ);
  if (!f) return;
  localRules.disabled.clear();
  while (f.available()) {
    String line = f.readStringUntil('\n');
    line.trim();
    if (line.length()) localRules.setDisabled(line, true);
  }
  f.close();
}

// ---- Map exports ------------------------------------------------------
// GPS-tagged wardrive data in formats external tools open directly:
// GeoJSON (geojson.io, QGIS), CSV, KML (Google Earth), GPX track.
static int exportMaps() {
  if (!sdLogMgr.mounted) return -1;
  if (!SD.exists("/AEGIS/data")) SD.mkdir("/AEGIS/data");
  int geo = 0;
  File f = SD.open("/AEGIS/data/wifi_map.geojson", FILE_WRITE);
  if (f) {
    f.print("{\"type\":\"FeatureCollection\",\"features\":[");
    bool first = true;
    for (const auto& e : g_wifiAtlas.entries) {
      if (e.lat == 0.0f && e.lon == 0.0f) continue;
      if (!first) f.print(",");
      first = false; geo++;
      String ssid = e.ssid; ssid.replace("\"", "'");
      char chans[64] = ""; size_t co = 0;
      for (size_t ci = 0; ci < e.channels.size(); ci++)
        co += snprintf(chans + co, sizeof(chans) - co, ci ? ",%d" : "%d", e.channels[ci]);
      char buf[420];
      snprintf(buf, sizeof(buf),
        "{\"type\":\"Feature\",\"geometry\":{\"type\":\"Point\",\"coordinates\":[%.6f,%.6f]},"
        "\"properties\":{\"ssid\":\"%s\",\"bssid\":\"%s\",\"channels\":\"%s\",\"best_rssi\":%d,\"sightings\":%u}}",
        (double)e.lon, (double)e.lat, ssid.c_str(), e.bssid.c_str(), chans, e.bestRssi, e.seenCount);
      f.print(buf);
    }
    f.print("]}");
    f.close();
  }
  f = SD.open("/AEGIS/data/wifi_map.csv", FILE_WRITE);
  if (f) {
    f.println("ssid,bssid,channel,best_rssi,sightings,lat,lon");
    for (const auto& e : g_wifiAtlas.entries) {
      if (e.lat == 0.0f && e.lon == 0.0f) continue;
      String ssid = e.ssid; ssid.replace(",", ";");
      char buf[320];
      snprintf(buf, sizeof(buf), "%s,%s,%d,%d,%u,%.6f,%.6f",
               ssid.c_str(), e.bssid.c_str(), e.lastChannel, e.bestRssi,
               e.seenCount, (double)e.lat, (double)e.lon);
      f.println(buf);
    }
    f.close();
  }
  f = SD.open("/AEGIS/data/wifi_map.kml", FILE_WRITE);
  if (f) {
    f.println("<?xml version=\"1.0\" encoding=\"UTF-8\"?>");
    f.println("<kml xmlns=\"http://www.opengis.net/kml/2.2\"><Document><name>Aegis Wi-Fi Atlas</name>");
    for (const auto& e : g_wifiAtlas.entries) {
      if (e.lat == 0.0f && e.lon == 0.0f) continue;
      String ssid = e.ssid; ssid.replace("&", "and"); ssid.replace("<", "");
      f.printf("<Placemark><name>%s</name><description>%s ch%d %ddBm seen %u</description>"
               "<Point><coordinates>%.6f,%.6f,0</coordinates></Point></Placemark>\n",
               ssid.c_str(), e.bssid.c_str(), e.lastChannel, e.bestRssi, e.seenCount,
               (double)e.lon, (double)e.lat);
    }
    f.println("</Document></kml>");
    f.close();
  }
  f = SD.open("/AEGIS/data/wifi_map_wigle.csv", FILE_WRITE);
  if (f) {
    // WiGLE wardrive format (WigleWifi-1.4): pre-header + CSV header.
    f.println("WigleWifi-1.4,appRelease=AEGIS-PHX,model=CardputerADV,release=v0.8.3,device=AEGIS-PHX,display=ST7789,board=ESP32-S3,brand=M5Stack");
    f.println("MAC,SSID,AuthMode,FirstSeen,Channel,RSSI,CurrentLatitude,CurrentLongitude,AltitudeMeters,AccuracyMeters,Type");
    String seen = "2020-01-01 00:00:00";                // no trusted clock: flagged in README
    if (gnssHat.currentFix.valid && gnssHat.currentFix.utcDate.length() == 6 && gnssHat.currentFix.utcTime.length() >= 6) {
      const String& d = gnssHat.currentFix.utcDate; const String& t = gnssHat.currentFix.utcTime;
      seen = "20" + d.substring(4, 6) + "-" + d.substring(2, 4) + "-" + d.substring(0, 2) + " " +
             t.substring(0, 2) + ":" + t.substring(2, 4) + ":" + t.substring(4, 6);
    }
    for (const auto& e : g_wifiAtlas.entries) {
      if (e.lat == 0.0f && e.lon == 0.0f) continue;
      String ssid = e.ssid; ssid.replace(",", ";"); ssid.replace("\"", "'");
      char buf[360];
      snprintf(buf, sizeof(buf), "%s,%s,WPA2,%s,%d,%d,%.6f,%.6f,0,0,WIFI",
               e.bssid.c_str(), ssid.c_str(), seen.c_str(), e.lastChannel, e.bestRssi,
               (double)e.lat, (double)e.lon);
      f.println(buf);
    }
    f.close();
  }
  if (!g_track.empty()) {
    f = SD.open("/AEGIS/data/track.gpx", FILE_WRITE);
    if (f) {
      f.println("<?xml version=\"1.0\" encoding=\"UTF-8\"?>");
      f.println("<gpx version=\"1.1\" creator=\"Aegis Phoenix\" xmlns=\"http://www.topografix.com/GPX/1/1\"><trk><name>wardrive</name><trkseg>");
      for (const auto& tp : g_track) f.printf("<trkpt lat=\"%.6f\" lon=\"%.6f\"></trkpt>\n", (double)tp.lat, (double)tp.lon);
      f.println("</trkseg></trk></gpx>");
      f.close();
    }
  }
  return geo;
}

static void agentLoop() {
  // M5Cardputer.update() is owned by loop() so keyboard input is serviced
  // before detectors, SD writes, or network maintenance.
  if (millis() < 45000) markStage("detectors");
  detectDeauth();
  pcapService();
  { // Sniffer channel hop (v0.8.2): promiscuous RX otherwise sits on one
    // channel and misses deauth/KRACK traffic elsewhere. Cycle 1..13 at
    // 1.2 s dwell whenever we are not scanning or joined to an AP.
    static uint32_t lastHopMs = 0;
    if (g_chanHop && !quarantined && !g_wifiScanRunning &&
        WiFi.status() != WL_CONNECTED && millis() - lastHopMs > 1200) {
      lastHopMs = millis();
      g_sniffChannel = (uint8_t)(g_sniffChannel % 13 + 1);
      esp_wifi_set_channel(g_sniffChannel, WIFI_SECOND_CHAN_NONE);
    }
  }
  const bool booting = millis() < 45000;

  // Poll expansion coprocessor and hats (inert when hardware absent:
  // GNSS only goes active on real NMEA, LoRa only after SX1262 answers)
  if (booting) markStage("c5-poll");
  if (!g_c5Off) esp32c5.poll();   // census may power this down
  if (booting) markStage("gnss-poll");
  if (!g_gnssOff) gnssHat.poll();   // census may power this down
  if (booting) markStage("rfid-poll");
  rfid2.poll();  // read-only ISO14443-A presence; inert without hardware
  if (rfid2.tagEventPending) {
    rfid2.tagEventPending = false;
    JsonDocument d; JsonObject f = d.to<JsonObject>();
    f["rfid.uid"] = rfid2.pendingUid; f["confidence"] = 60;
    pushEvent("rfid_tag", "rfid", rfid2.pendingUid, "RFID/NFC tag presented (RFID2 reader)", f);
  }
  if (g_wardriveOn && gnssHat.currentFix.valid) {
    static uint32_t lastTrackMs = 0;
    if (millis() - lastTrackMs > 10000 && g_track.size() < 600) {
      lastTrackMs = millis();
      TrackPoint tp; tp.lat = gnssHat.currentFix.latitude; tp.lon = gnssHat.currentFix.longitude; tp.atMs = millis();
      g_track.push_back(tp);
    }
  }
  // Boot device census: verify every hat/connection ONCE, then power
  // down polling for whatever is not attached (user request, v0.8.0).
  if (!g_censusDone && millis() - g_censusStartMs > 20000) {
    g_censusDone = true;
    if (!gnssHat.active && gnssHat.sentencesParsed == 0) g_gnssOff = true;
    if (!esp32c5.active) g_c5Off = true;
    Serial.printf("[CENSUS] sd=%d lora=%d gnss=%s c5=%s rfid_en=%d wifi=%s\n",
                  sdLogMgr.mounted ? 1 : 0, loraHat.active ? 1 : 0,
                  g_gnssOff ? "off" : "on", g_c5Off ? "off" : "on",
                  rfid2.enabled ? 1 : 0,
                  WiFi.status() == WL_CONNECTED ? "joined" : "offline");
  }
  if (booting) markStage("lora-poll");
  loraHat.poll([](const String& hexData, int rssi, float snr) {
    sdLogMgr.appendLog("lora_hat.log", "[LORA] rssi=" + String(rssi) + " snr=" + String(snr, 1) + " hex=" + hexData);
  });
  if (gnssHat.currentFix.spoofDetected) {
    static uint32_t lastSpoofPush = 0;
    if (millis() - lastSpoofPush > 60000) {
      lastSpoofPush = millis();
      JsonDocument d; JsonObject f = d.to<JsonObject>();
      f["gnss.spoof"] = true;
      f["lat"] = gnssHat.currentFix.latitude; f["lon"] = gnssHat.currentFix.longitude;
      f["confidence"] = 65;
      pushEvent("gnss_spoof", "gnss", "cap-lora-1262", "Impossible GNSS position leap (spoof/jump suspect)", f);
    }
  }

  if (esp32c5.deauth5GCount > 0) {
    uint32_t cnt = esp32c5.deauth5GCount; esp32c5.deauth5GCount = 0;
    JsonDocument d; JsonObject f = d.to<JsonObject>();
    f["wifi.mgmt"] = 0xC0; f["count(window=1s)"] = (int)cnt; f["band"] = "5GHz"; f["confidence"] = 90;
    pushEvent("deauth_flood", "wifi_5g", esp32c5.lastDeauthSource5G, "5GHz Deauthentication Flood burst detected via ESP32-C5", f);
    sdLogMgr.appendLog("esp32c5_5ghz.log", "[5GHz DEAUTH] Count: " + String(cnt) + " Source: " + esp32c5.lastDeauthSource5G);
  }

  {
    static uint32_t lastAtlasSave = 0;
    if (g_wifiAtlas.dirty && millis() - lastAtlasSave > 60000) { g_wifiAtlas.save(); lastAtlasSave = millis(); }
  }
  if (millis() - lastHeartbeat > heartbeatSec * 1000UL) { heartbeat(); lastHeartbeat = millis(); }
  {
    static uint32_t lastReconnectTry = 0;
    if (!g_passiveOffline && g_wifiSsid.length() && WiFi.status() != WL_CONNECTED && millis() - lastReconnectTry > 30000) {
      lastReconnectTry = millis();
      WiFi.reconnect();
    }
  }
  delay(5);  // ~200 Hz keyboard FIFO drain when the loop is otherwise idle
}
