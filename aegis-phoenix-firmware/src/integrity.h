// AEGIS Integrity Manager - on-device verification + fuzz + auto-repair.
// Scope (honest): verifies THIS firmware's running partition, every
// .bin stored on the SD card, SD health, the AES vault, and fuzz-feeds
// the rule/NMEA parsers. Apps Launcher flashed into internal partitions
// cannot be enumerated from inside a running app; SD-side artifacts are
// what can be checked, and corrupt ones are quarantined (.bin.bad).
#pragma once
#include <Arduino.h>
#include <esp_ota_ops.h>
#include <esp_partition.h>
#include <esp_app_format.h>
#include <mbedtls/sha256.h>

struct IntegrityReport {
  bool ran = false;
  bool full = false;
  String appVersion = "";
  String partitionLabel = "";
  String partitionSha = "";     // first 16 hex chars are shown on UI
  int binsFound = 0, binsValid = 0, binsQuarantined = 0;
  bool sdProbeOk = false;
  bool vaultOk = false;
  int rulesFuzzCases = 0;
  int gnssFuzzCases = 0;
  uint32_t freeHeap = 0, minHeap = 0;
  int fixesApplied = 0;
  uint32_t ranAtMs = 0;
};
static IntegrityReport g_integrity;
static std::vector<String> g_programReports;

static String sha256FileHex(File& f, bool& magicOk, bool& mergedMarker) {
  mbedtls_sha256_context ctx;
  mbedtls_sha256_init(&ctx);
  mbedtls_sha256_starts(&ctx, 0);
  uint8_t buf[1024];
  size_t total = 0;
  magicOk = false; mergedMarker = false;
  bool first = true;
  while (f.available()) {
    int n = f.read(buf, sizeof(buf));
    if (n <= 0) break;
    if (first && n >= 1) magicOk = buf[0] == 0xE9;
    // merged marker lives at absolute offset 0x8000
    if (total <= 32768 && total + (size_t)n > 32770) {
      size_t off = 32768 - total;
      if (off + 1 < (size_t)n) mergedMarker = buf[off] == 0xAA && buf[off + 1] == 0x50;
    }
    mbedtls_sha256_update(&ctx, buf, n);
    total += n;
    first = false;
  }
  uint8_t out[32];
  mbedtls_sha256_finish(&ctx, out);
  mbedtls_sha256_free(&ctx);
  char hex[65];
  for (int i = 0; i < 32; i++) snprintf(hex + i * 2, 3, "%02x", out[i]);
  hex[64] = 0;
  return String(hex);
}

// Fuzz the NMEA parser with mutated valid sentences (driver restores state).
// Implemented in gnss_hat.h as fuzzSelfTest().

static IntegrityReport runIntegritySweep(bool full) {
  IntegrityReport r;
  r.full = full;
  r.ranAtMs = millis();

  // 1) Running app partition identity + digest
  const esp_partition_t* part = esp_ota_get_running_partition();
  const esp_app_desc_t* desc = esp_ota_get_app_description();
  if (desc) r.appVersion = String(desc->version);
  if (part) {
    r.partitionLabel = String(part->label);
    uint8_t sha[32];
    if (esp_partition_get_sha256(part, sha) == ESP_OK) {
      char hex[65];
      for (int i = 0; i < 32; i++) snprintf(hex + i * 2, 3, "%02x", sha[i]);
      hex[64] = 0;
      r.partitionSha = String(hex);
    }
  }

  // 2) Heap sanity
  r.freeHeap = ESP.getFreeHeap();
  r.minHeap = ESP.getMinFreeHeap();

  // 3) SD write/read/remove probe, with one remount attempt as auto-fix
  if (sdLogMgr.mounted) {
    auto probe = []() {
      const char* path = "/AEGIS/logs/.health_probe";
      File w = SD.open(path, FILE_WRITE);
      if (!w) return false;
      w.print("AEGIS-PROBE-0123456789");
      w.close();
      File rd = SD.open(path, FILE_READ);
      if (!rd) return false;
      String s = rd.readString();
      rd.close();
      SD.remove(path);
      return s == "AEGIS-PROBE-0123456789";
    };
    r.sdProbeOk = probe();
    if (!r.sdProbeOk) {
      Serial.println("[INTEGRITY] SD probe failed - remounting and retrying");
      sdLogMgr.begin(AEGIS_SD_CS);
      r.sdProbeOk = probe();
      if (r.sdProbeOk) r.fixesApplied++;
    }
  }

  // 4) Vault live-fire: fresh round-trip + tamper rejection
  if (aegisVault.ready) {
    String enc = aegisVault.encryptLine("INTEGRITY-PROBE");
    String dec;
    bool ok = enc.length() && aegisVault.decryptLine(enc, dec) && dec == "INTEGRITY-PROBE";
    String tampered = enc;
    if (tampered.length() > 4) tampered.setCharAt(tampered.length() - 2, tampered.charAt(tampered.length() - 2) == 'A' ? 'B' : 'A');
    String junk;
    r.vaultOk = ok && !aegisVault.decryptLine(tampered, junk);
    if (!r.vaultOk) { aegisVault.ready = false; r.fixesApplied++; }  // never log into a broken vault
  }

  if (full) {
    // 5) SD-stored firmware artifacts: hash, classify, quarantine corrupt
    g_programReports.clear();
    if (sdLogMgr.mounted) {
      for (auto& fi : sdLogMgr.listFiles("/")) {
        String name = fi.name;
        if (name.startsWith("/")) name = name.substring(1);
        String lower = name; lower.toLowerCase();
        if (fi.isDir || !lower.endsWith(".bin")) continue;
        r.binsFound++;
        String path = "/" + name;
        File f = SD.open(path.c_str(), FILE_READ);
        if (!f) continue;
        bool magicOk, merged;
        String sha = sha256FileHex(f, magicOk, merged);
        f.close();
        String kind = merged ? "MERGED(0x0)" : "APP(Launcher)";
        g_programReports.push_back(name + " " + String(fi.size / 1024) + "k " + kind + " " + sha.substring(0, 8));
        if (magicOk) {
          r.binsValid++;
        } else {
          // Auto-fix: a non-ESP image named .bin can only brick a slot.
          String bad = path + ".bad";
          if (SD.rename(path.c_str(), bad.c_str())) { r.binsQuarantined++; r.fixesApplied++; }
        }
      }
    }

    // 6) Parser fuzz: deterministic mutations must never crash/hang us.
    {
      static const char* bases[3] = {
        "wifi.mgmt == 0xC0 && count(window=1s) > 40",
        "wifi.ssid in corp_list && wifi.bssid notin known_bssids",
        "kind == \"deauth_flood\" || rssi_delta >= 6",
      };
      uint32_t rngState = 0xAE615u;
      auto rnd = [&]() { rngState = rngState * 1664525u + 1013904223u; return rngState >> 16; };
      JsonDocument doc;
      JsonObject fields = doc.to<JsonObject>();
      fields["wifi.mgmt"] = 0xC0; fields["count(window=1s)"] = 100;
      int cases = 0;
      for (int i = 0; i < 300; i++) {
        String expr = bases[i % 3];
        int pos = rnd() % expr.length();
        char mut = (char)(' ' + (rnd() % 90));
        expr.setCharAt(pos, mut);
        (void)Rule::parses(expr);
        Rule rr; rr.expression = expr; rr.toks = ruleTokenize(expr);
        (void)rr.evaluate(Ctx{fields, &corpSsids, &knownBssids});
        cases++;
      }
      r.rulesFuzzCases = cases;
    }
    r.gnssFuzzCases = gnssHat.fuzzSelfTest(300);
  }

  r.ran = true;
  g_integrity = r;
  if (sdLogMgr.mounted) {
    sdLogMgr.appendLog("integrity.log",
      String("[SWEEP] full=") + (full ? "1" : "0") + " app=" + r.appVersion +
      " part=" + r.partitionLabel + " bins=" + String(r.binsFound) + "/" + String(r.binsValid) +
      " quarantined=" + String(r.binsQuarantined) + " sd=" + (r.sdProbeOk ? "ok" : "FAIL") +
      " vault=" + (r.vaultOk ? "ok" : "n/a") + " heap=" + String(r.freeHeap) +
      " fixes=" + String(r.fixesApplied));
  }
  return r;
}
