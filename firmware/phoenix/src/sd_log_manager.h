/**
 * PHOENIX Cardputer MicroSD File & Offline Log Manager
 * --------------------------------------------------------
 * Manages saving and reading persistent captures on the MicroSD card:
 *  - /phoenix/logs/pcap_capture.pcap
 *  - /phoenix/logs/esp32c5_5ghz.log
 *  - /phoenix/logs/lora_hat.log
 *  - /phoenix/logs/gnss_stream.nmea
 *  - /phoenix/logs/syslog.log
 *
 * Provides API to list files, read contents, and sync offline dumps to the console.
 */

#pragma once
#include <Arduino.h>
#include <SD.h>
#include <FS.h>
#include <vector>

struct SdFileInfo {
  String name;
  size_t size;
  bool isDir;
};

class SdLogManager {
 public:
  bool mounted = false;

  void begin(int csPin = 12) {
    SPI.begin(40, 39, 14, 12);
    if (SD.begin(csPin, SPI, 25000000)) {
      mounted = true;
      const char* dirs[] = {"/AEGIS", "/AEGIS/logs", "/AEGIS/downloads", "/AEGIS/rules", "/AEGIS/art", "/AEGIS/standalone"};
      for (auto* d : dirs) if (!SD.exists(d)) SD.mkdir(d);
      Serial.println("[SD Logs] /AEGIS/logs/ initialized on MicroSD card");
    } else {
      Serial.println("[SD Logs] MicroSD card not mounted");
    }
  }

  void appendLog(const String& filename, const String& entry) {
    if (!mounted) return;
    String path = "/AEGIS/logs/" + filename;
    // Rotation: no single log may grow past ~512 KB on the card.
    if (SD.exists(path.c_str())) {
      File chk = SD.open(path.c_str(), FILE_READ);
      const size_t sz = chk ? chk.size() : 0;
      if (chk) chk.close();
      if (sz > 512 * 1024) {
        String old = path + ".1";
        if (SD.exists(old.c_str())) SD.remove(old.c_str());
        SD.rename(path.c_str(), old.c_str());
      }
    }
    File f = SD.open(path.c_str(), FILE_APPEND);
    if (f) {
      f.println(entry);
      f.close();
    }
  }

  String readFile(const String& path, size_t maxBytes = 4096) {
    if (!mounted || !SD.exists(path.c_str())) return "";
    File f = SD.open(path.c_str(), FILE_READ);
    if (!f) return "";
    String content = "";
    while (f.available() && content.length() < maxBytes) {
      content += (char)f.read();
    }
    f.close();
    return content;
  }

  std::vector<SdFileInfo> listFiles(const String& dirPath = "/AEGIS/logs") {
    std::vector<SdFileInfo> list;
    if (!mounted) return list;
    File root = SD.open(dirPath.c_str());
    if (!root || !root.isDirectory()) return list;

    File file = root.openNextFile();
    while (file) {
      list.push_back({ String(file.name()), file.size(), file.isDirectory() });
      file = root.openNextFile();
    }
    return list;
  }

  uint32_t getTotalMb() {
    if (!mounted) return 0;
    return (uint32_t)(SD.totalBytes() / (1024 * 1024));
  }

  uint32_t getFreeMb() {
    if (!mounted) return 0;
    return (uint32_t)((SD.totalBytes() - SD.usedBytes()) / (1024 * 1024));
  }
};

extern SdLogManager sdLogMgr;
