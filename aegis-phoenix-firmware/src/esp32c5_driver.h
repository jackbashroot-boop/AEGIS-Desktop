/**
 * AEGIS ESP32-C5 hot-swap companion link (Option A: Grove/EXT UART)
 * ------------------------------------------------------------------
 * The C5 is NOT probed once at boot. This manager owns Serial2 on the
 * Cardputer ADV Grove port (RX=G1, TX=G2, 115200 8N1) and runs a small
 * state machine from poll():
 *
 *   OFFLINE  -- every 2.5 s --> send "AEGIS_HELLO"
 *   C5 answers "+AEGIS:C5,<fw>,<mac>"  --> ONLINE: we immediately send
 *   AT+WIFI5G=1 / AT+SNIFF5G=1 / AT+SCAN5G=1 (no user input), then keep
 *   the link warm with AT+PING every 5 s.
 *   No recognised traffic for 15 s --> OFFLINE again (unplugged, reset,
 *   or crashed companion), and probing resumes automatically.
 *
 * The C5 can be powered separately over its own USB-C cable; USB-C is
 * power-only in this option. Data is the 3.3 V UART on Grove:
 *   ADV G1 (RX) <- C5 TX     ADV G2 (TX) -> C5 RX     GND <-> GND
 * Never connect 5 V to a GPIO.
 *
 * Line protocol (implemented by aegis-c5-companion/):
 *   -> AEGIS_HELLO            <- +AEGIS:C5,<fw>,<mac> / OK
 *   -> AT+PING                <- +PONG:<ms> / OK
 *   -> AT+SNIFF5G=1|0         5 GHz promiscuous deauth monitor on/off
 *   -> AT+SCAN5G=1|0          periodic 5 GHz AP scans on/off
 *   -> AT+CWLAP5G             <- +AP5G:"ssid","bssid",rssi,chan ... +SCAN5G_DONE
 *   -> AT+CWJAP5G="ssid","psk" <- +WIFI5G:CONNECTED,"ssid",rssi
 *   <- +5G_DEAUTH:<count>,<source_mac>   (unsolicited detector events)
 */
#pragma once
#include <Arduino.h>
#include <vector>

class ESP32C5Driver {
 public:
  bool active = false;              // companion answered and is configured
  bool begun = false;               // Serial2 owned by the hot-swap probe
  bool sniffing = false;
  bool connected5G = false;
  String currentSsid5G = "";
  int rssi5G = -100;
  uint32_t deauth5GCount = 0;
  String lastDeauthSource5G = "";
  String firmware = "";
  int apCount5G = 0;
  uint32_t lastSeenMs = 0;
  uint32_t probesSent = 0;
  uint32_t handshakes = 0;

  void beginHotSwap(int rxPin = 1, int txPin = 2, uint32_t baud = 115200) {
    if (begun) return;
    pinMode(rxPin, INPUT_PULLUP);   // unplugged RX idles high, less noise
    Serial2.begin(baud, SERIAL_8N1, rxPin, txPin);
    Serial2.setTimeout(20);
    begun = true;
    nextProbeMs_ = millis() + 400;
    Serial.printf("[C5] hot-swap probe on UART G%d/G%d @%lu - plug in any time\n",
                  rxPin, txPin, (unsigned long)baud);
  }

  void endHotSwap() {
    if (!begun) return;
    Serial2.end();
    begun = false;
    active = false;
    sniffing = false;
    connected5G = false;
    Serial.println("[C5] hot-swap link disabled");
  }

  // Non-blocking state machine; call every main-loop iteration.
  void poll() {
    if (!begun) return;
    const uint32_t now = millis();

    // Drain whatever arrived, one line at a time.
    while (Serial2.available()) {
      char c = (char)Serial2.read();
      if (c == '\n') {
        line_.trim();
        if (line_.length()) handleLine(line_, now);
        line_ = "";
      } else if (c != '\r') {
        line_ += c;
        if (line_.length() > 240) line_ = "";   // noise guard
      }
    }

    if (active) {
      if (now - lastPingMs_ > 5000) {
        lastPingMs_ = now;
        sendLine("AT+PING");
      }
      if (now - lastSeenMs > 15000) {
        active = false;
        sniffing = false;
        connected5G = false;
        apCount5G = 0;
        nextProbeMs_ = now + 500;
        Serial.println("[C5] companion lost (unplugged/reset?) - resuming hot-swap probe");
      }
      return;
    }

    if ((int32_t)(now - nextProbeMs_) >= 0) {
      sendLine("AEGIS_HELLO");
      probesSent++;
      nextProbeMs_ = now + 2500;
    }
  }

  void setSniffing(bool on) {
    if (!active) return;
    sendLine(on ? "AT+SNIFF5G=1" : "AT+SNIFF5G=0");
    sniffing = on;
  }

  // One-shot 5 GHz scan (console command path; pumps poll() while waiting).
  bool scan5Ghz(std::vector<String>& ssids, std::vector<String>& bssids, std::vector<int>& rssis) {
    if (!active) return false;
    lastSsids_.clear(); lastBssids_.clear(); lastRssis_.clear();
    scanDone_ = false;
    sendLine("AT+CWLAP5G");
    const uint32_t start = millis();
    while (!scanDone_ && millis() - start < 6000) { poll(); delay(5); }
    ssids = lastSsids_; bssids = lastBssids_; rssis = lastRssis_;
    return scanDone_;
  }

  bool connect5Ghz(const String& ssid, const String& psk) {
    if (!active || !ssid.length()) return false;
    sendLine("AT+CWJAP5G=\"" + ssid + "\",\"" + psk + "\"");
    const uint32_t start = millis();
    while (!connected5G && millis() - start < 12000) { poll(); delay(10); }
    return connected5G;
  }

  // Legacy one-shot begin() kept for API compatibility; hot-swap is the
  // supported path and never blocks waiting for a board that is absent.
  void begin(int rxPin = 1, int txPin = 2, uint32_t baud = 115200) { beginHotSwap(rxPin, txPin, baud); }
  void startPromiscuous5Ghz() { setSniffing(true); }

 private:
  String line_;
  uint32_t nextProbeMs_ = 0;
  uint32_t lastPingMs_ = 0;
  bool scanDone_ = false;
  std::vector<String> lastSsids_, lastBssids_;
  std::vector<int> lastRssis_;

  void sendLine(const String& s) {
    if (!begun) return;
    Serial2.println(s);
  }

  void configureCompanion() {
    // Automatic bring-up: radio on, deauth monitor on, periodic AP scans.
    sendLine("AT+WIFI5G=1");
    sendLine("AT+SNIFF5G=1");
    sendLine("AT+SCAN5G=1");
    sniffing = true;
  }

  void handleLine(const String& line, uint32_t now) {
    if (line.startsWith("+AEGIS:C5")) {
      // Format: +AEGIS:C5,<firmware>,<mac>
      int c1 = line.indexOf(',');
      int c2 = c1 >= 0 ? line.indexOf(',', c1 + 1) : -1;
      if (c1 >= 0) firmware = line.substring(c1 + 1, c2 >= 0 ? c2 : line.length());
      lastSeenMs = now;
      if (!active) {
        active = true;
        handshakes++;
        lastPingMs_ = now;
        configureCompanion();
        Serial.printf("[C5] companion online (%s) - 5 GHz monitor auto-started\n", firmware.c_str());
      }
      return;
    }
    if (line.startsWith("+PONG") || line == "OK") { lastSeenMs = now; return; }
    if (line.startsWith("+5G_DEAUTH:")) {
      lastSeenMs = now;
      String rest = line.substring(11);
      int comma = rest.indexOf(',');
      uint32_t count = comma >= 0 ? (uint32_t)rest.substring(0, comma).toInt() : 1;
      deauth5GCount += count ? count : 1;
      if (comma >= 0) lastDeauthSource5G = rest.substring(comma + 1);
      return;
    }
    if (line.startsWith("+AP5G:")) {
      lastSeenMs = now;
      // +AP5G:"ssid","bssid",rssi,channel
      int q1 = line.indexOf('"');
      int q2 = q1 >= 0 ? line.indexOf('"', q1 + 1) : -1;
      int q3 = q2 >= 0 ? line.indexOf('"', q2 + 1) : -1;
      int q4 = q3 >= 0 ? line.indexOf('"', q3 + 1) : -1;
      if (q1 >= 0 && q4 > q3) {
        if (lastSsids_.size() < 32) {
          lastSsids_.push_back(line.substring(q1 + 1, q2));
          lastBssids_.push_back(line.substring(q3 + 1, q4));
          String tail = line.substring(q4 + 1);
          int c1 = tail.indexOf(',');
          int c2 = c1 >= 0 ? tail.indexOf(',', c1 + 1) : -1;
          lastRssis_.push_back(c1 >= 0 ? tail.substring(c1 + 1, c2 >= 0 ? c2 : tail.length()).toInt() : 0);
        }
        apCount5G = (int)lastSsids_.size();
      }
      return;
    }
    if (line.startsWith("+SCAN5G_DONE")) { lastSeenMs = now; scanDone_ = true; return; }
    if (line.startsWith("+WIFI5G:CONNECTED")) {
      lastSeenMs = now;
      connected5G = true;
      int q1 = line.indexOf('"');
      int q2 = q1 >= 0 ? line.indexOf('"', q1 + 1) : -1;
      if (q1 >= 0 && q2 > q1) currentSsid5G = line.substring(q1 + 1, q2);
      int comma = line.lastIndexOf(',');
      if (comma >= 0) rssi5G = line.substring(comma + 1).toInt();
      return;
    }
    if (line.startsWith("+WIFI5G:DISCONNECTED")) {
      lastSeenMs = now;
      connected5G = false;
      currentSsid5G = "";
      rssi5G = -100;
      return;
    }
    // Unknown lines are ignored on purpose: a floating/unplugged UART or
    // a foreign device on Grove must never fake a C5 detection. Only the
    // +AEGIS:C5 handshake above can bring the link online.
  }
};

extern ESP32C5Driver esp32c5;
