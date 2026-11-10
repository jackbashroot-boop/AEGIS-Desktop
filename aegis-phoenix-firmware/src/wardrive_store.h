// AEGIS Wardrive Store - deduplicating wardrive record builder.
// Emits one JSONL record per AP unless the same BSSID+channel was
// logged in the last 60 s with an RSSI within 5 dBm (prevents the SD
// card filling with identical beacons while standing still).
#pragma once
#include <Arduino.h>
#include <ArduinoJson.h>
#include <map>

class WardriveStore {
 public:
  // Returns true and fills `outJson` when this sighting is worth saving.
  bool buildRecord(const String& ssid, const String& bssid, int channel, int rssi,
                   int encType, float lat, float lon, int sats, bool hasFix,
                   uint32_t nowMs, String& outJson,
                   float hdop = 99.9f, float accM = 0) {
    String key = bssid + "@" + String(channel);
    auto it = lastLog_.find(key);
    if (it != lastLog_.end() && nowMs - it->second.ms < 60000 && abs(rssi - it->second.rssi) < 5) {
      return false;
    }
    lastLog_[key] = {nowMs, rssi};
    kept++;
    if (lastLog_.size() > 512) lastLog_.clear();   // memory bound; file keeps everything

    JsonDocument doc;
    doc["ts"] = nowMs / 1000;
    doc["ssid"] = ssid;
    doc["bssid"] = bssid;
    doc["ch"] = channel;
    doc["rssi"] = rssi;
    doc["enc"] = encType;
    if (hasFix) {
      doc["lat"] = lat;
      doc["lon"] = lon;
      doc["sats"] = sats;
      doc["hdop"] = hdop;
      doc["acc"] = accM;
    }
    outJson = "";
    serializeJson(doc, outJson);
    return true;
  }

  size_t tracked() const { return lastLog_.size(); }
  uint32_t kept = 0;

 private:
  struct Mark { uint32_t ms; int rssi; };
  std::map<String, Mark> lastLog_;
};
