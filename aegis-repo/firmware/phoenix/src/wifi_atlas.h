// AEGIS Wi-Fi Atlas - persistent per-network memory on SD.
// Remembers, per BSSID: SSID, every channel seen, last/best RSSI,
// sighting count, and the GNSS position of the best sighting.
// Stored as JSON at /AEGIS/data/wifi_atlas.json (max 64 networks,
// least-recently-seen evicted). Saving is throttled by the caller.
#pragma once
#include <Arduino.h>
#include <ArduinoJson.h>
#include <SD.h>
#include <vector>
#include <algorithm>

struct AtlasEntry {
  String ssid, bssid;
  std::vector<int> channels;
  int lastChannel = 0;
  int bestRssi = -127;
  uint32_t seenCount = 0;
  uint32_t lastSeenSec = 0;
  float lat = 0, lon = 0;
};

class WifiAtlas {
 public:
  std::vector<AtlasEntry> entries;
  bool dirty = false;

  void observe(const String& ssid, const String& bssid, int channel, int rssi,
               float lat, float lon, bool hasFix, uint32_t nowSec) {
    AtlasEntry* e = nullptr;
    for (auto& x : entries) if (x.bssid == bssid) { e = &x; break; }
    if (!e) {
      if (entries.size() >= 64) {
        // evict least-recently-seen
        size_t worst = 0;
        for (size_t i = 1; i < entries.size(); i++)
          if (entries[i].lastSeenSec < entries[worst].lastSeenSec) worst = i;
        entries.erase(entries.begin() + worst);
      }
      entries.push_back(AtlasEntry());
      e = &entries.back();
      e->ssid = ssid; e->bssid = bssid;
    }
    e->ssid = ssid;
    e->lastChannel = channel;
    if (std::find(e->channels.begin(), e->channels.end(), channel) == e->channels.end()
        && e->channels.size() < 12) e->channels.push_back(channel);
    if (rssi > e->bestRssi) {
      e->bestRssi = rssi;
      if (hasFix) { e->lat = lat; e->lon = lon; }
    }
    e->seenCount++;
    if (hasFix && e->lat == 0 && e->lon == 0) { e->lat = lat; e->lon = lon; }  // backfill
    e->lastSeenSec = nowSec;
    dirty = true;
  }

  bool load() {
    if (!SD.exists("/AEGIS/data/wifi_atlas.json")) return false;
    File f = SD.open("/AEGIS/data/wifi_atlas.json", FILE_READ);
    if (!f) return false;
    JsonDocument doc;
    DeserializationError err = deserializeJson(doc, f);
    f.close();
    if (err) return false;
    entries.clear();
    for (JsonObject o : doc.as<JsonArray>()) {
      AtlasEntry e;
      e.ssid = o["ssid"].as<String>(); e.bssid = o["bssid"].as<String>();
      e.lastChannel = o["lastCh"] | 0; e.bestRssi = o["bestRssi"] | -127;
      e.seenCount = o["seen"] | 0; e.lastSeenSec = o["lastSeen"] | 0;
      e.lat = o["lat"] | 0.0f; e.lon = o["lon"] | 0.0f;
      for (JsonVariant c : o["ch"].as<JsonArray>()) e.channels.push_back(c.as<int>());
      if (e.bssid.length()) entries.push_back(e);
      if (entries.size() >= 64) break;
    }
    dirty = false;
    return true;
  }

  bool save() {
    if (!SD.exists("/AEGIS")) return false;
    if (!SD.exists("/AEGIS/data")) SD.mkdir("/AEGIS/data");
    File f = SD.open("/AEGIS/data/wifi_atlas.json", FILE_WRITE);
    if (!f) return false;
    JsonDocument doc;
    JsonArray arr = doc.to<JsonArray>();
    for (auto& e : entries) {
      JsonObject o = arr.add<JsonObject>();
      o["ssid"] = e.ssid; o["bssid"] = e.bssid;
      o["lastCh"] = e.lastChannel; o["bestRssi"] = e.bestRssi;
      o["seen"] = e.seenCount; o["lastSeen"] = e.lastSeenSec;
      o["lat"] = e.lat; o["lon"] = e.lon;
      JsonArray ch = o["ch"].to<JsonArray>();
      for (int c : e.channels) ch.add(c);
    }
    bool ok = serializeJson(doc, f) > 0;
    f.close();
    if (ok) dirty = false;
    return ok;
  }
};
