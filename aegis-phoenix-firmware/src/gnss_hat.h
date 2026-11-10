/**
 * AEGIS GNSS driver - Cap LoRa-1262 ATGM336H-6N (AT6668), UART1 @115200.
 * ------------------------------------------------------------------
 * REAL NMEA-0183 parser (replaces the old stub, which never decoded a
 * single coordinate): checksum-validated $GxGGA + $GxRMC parsing with
 * ddmm.mmmm -> decimal degrees, altitude, HDOP, speed/course, UTC, and
 * an impossible-jump (spoof/leap) detector.
 *
 * `active` means "valid NMEA sentences are arriving" - it stays false
 * with no Cap attached instead of the old always-true claim.
 */

#pragma once
#include <Arduino.h>
#include <math.h>

struct GnssFix {
  bool valid = false;
  float latitude = 0.0f;
  float longitude = 0.0f;
  float altitude = 0.0f;
  int satellites = 0;
  float speedKnots = 0.0f;
  float courseDeg = 0.0f;
  float hdop = 99.9f;
  int fixQuality = 0;
  String utcTime = "";
  String utcDate = "";
  bool spoofDetected = false;
};

class GnssHatDriver {
 public:
  bool active = false;               // true once valid NMEA is flowing
  GnssFix currentFix;
  uint32_t sentencesParsed = 0;
  uint32_t checksumErrors = 0;
  uint32_t lastFixTime = 0;
  uint32_t acceptedFixes = 0;
  uint32_t rejectedJumps = 0;

  // Consumer GNSS does not report true accuracy in GGA/RMC. HDOP is
  // the best available evidence; use the usual ~5 m per HDOP unit
  // planning factor and never claim centimetre precision.
  float estimatedAccuracyM() const {
    if (currentFix.hdop > 0 && currentFix.hdop < 20) {
      float acc = currentFix.hdop * 5.0f;
      return acc < 5 ? 5 : (acc > 250 ? 250 : acc);
    }
    if (currentFix.satellites >= 10) return 20;
    if (currentFix.satellites >= 7) return 35;
    if (currentFix.satellites >= 5) return 60;
    if (currentFix.satellites >= 4) return 90;
    return 150;
  }

  // Stricter than "receiver says valid": wardrive records should not
  // inherit a stale, very noisy, or just-rejected leap point.
  bool positionForLog() const {
    return currentFix.valid && !currentFix.spoofDetected &&
           currentFix.satellites >= 4 && currentFix.hdop > 0 &&
           currentFix.hdop <= 8.0f;
  }

  void begin(int rxPin = 15, int txPin = 13, uint32_t baud = 115200) {
    Serial1.begin(baud, SERIAL_8N1, rxPin, txPin);
    Serial.printf("[GNSS] ATGM336H UART1 RX=%d TX=%d @%u (active when NMEA arrives)\n", rxPin, txPin, baud);
  }

  // On-device fuzz: mutate a valid sentence deterministically and feed
  // the real parser. State is preserved; returns cases executed.
  int fuzzSelfTest(uint32_t rounds) {
    GnssFix savedFix = currentFix;
    bool savedActive = active;
    uint32_t savedSentences = sentencesParsed;
    float savedPrevLat = prevLat_, savedPrevLon = prevLon_;
    uint32_t savedPrevMs = prevFixMs_, savedAccepted = acceptedFixes, savedRejected = rejectedJumps;
    String body = "GNGGA,123519,4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,";
    uint8_t x = 0; for (size_t i = 0; i < body.length(); i++) x ^= (uint8_t)body[i];
    char cs[3]; snprintf(cs, sizeof(cs), "%02X", x);
    String valid = "$" + body + "*" + String(cs);
    uint32_t s = 0x6E55u;
    for (uint32_t i = 0; i < rounds; i++) {
      s = s * 1664525u + 1013904223u;
      String m = valid;
      int pos = 1 + (int)((s >> 16) % (uint32_t)(m.length() - 1));
      m.setCharAt(pos, (char)(' ' + ((s >> 24) % 90)));
      processLine(m);
    }
    currentFix = savedFix; active = savedActive; sentencesParsed = savedSentences;
    prevLat_ = savedPrevLat; prevLon_ = savedPrevLon; prevFixMs_ = savedPrevMs;
    acceptedFixes = savedAccepted; rejectedJumps = savedRejected;
    rejectCount_ = 0;
    return (int)rounds;
  }

  void poll() {
    while (Serial1.available()) {
      char c = (char)Serial1.read();
      if (c == '\n') {
        if (buffer_.length() > 8) processLine(buffer_);
        buffer_ = "";
      } else if (c != '\r') {
        buffer_ += c;
        if (buffer_.length() > 100) buffer_ = "";   // runaway line guard
      }
    }
    // Fix goes stale 5 s after the last valid position update.
    if (currentFix.valid && millis() - lastFixTime > 5000) {
      currentFix.valid = false;
      currentFix.spoofDetected = false;
    }
  }

 private:
  String buffer_ = "";
  float prevLat_ = 0, prevLon_ = 0;
  uint32_t prevFixMs_ = 0;
  float rejectLat_ = 0, rejectLon_ = 0;
  uint32_t rejectFirstMs_ = 0, rejectLastMs_ = 0;
  uint8_t rejectCount_ = 0;

  static bool checksumOk(const String& s) {
    int star = s.lastIndexOf('*');
    if (!s.startsWith("$") || star < 1 || star + 2 >= (int)s.length()) return false;
    uint8_t x = 0;
    for (int i = 1; i < star; i++) x ^= (uint8_t)s[i];
    long want = strtol(s.substring(star + 1, star + 3).c_str(), nullptr, 16);
    return x == (uint8_t)want;
  }

  // Split by commas (empties preserved). Returns field count.
  static int split(const String& s, String out[], int maxOut) {
    int n = 0, start = 0;
    for (int i = 0; i <= (int)s.length() && n < maxOut; i++) {
      if (i == (int)s.length() || s[i] == ',') { out[n++] = s.substring(start, i); start = i + 1; }
    }
    return n;
  }

  static float nmeaToDeg(const String& raw, const String& hemi) {
    if (raw.isEmpty()) return 0.0f;
    double v = atof(raw.c_str());
    double deg = floor(v / 100.0);
    double dec = deg + (v - deg * 100.0) / 60.0;
    if (hemi == "S" || hemi == "W") dec = -dec;
    return (float)dec;
  }

  static float haversineM(float la1, float lo1, float la2, float lo2) {
    const float R = 6371000.0f, D = PI / 180.0f;
    float dla = (la2 - la1) * D, dlo = (lo2 - lo1) * D;
    float a = sinf(dla / 2) * sinf(dla / 2) + cosf(la1 * D) * cosf(la2 * D) * sinf(dlo / 2) * sinf(dlo / 2);
    return 2 * R * asinf(sqrtf(a));
  }

  bool notePosition(float lat, float lon) {
    uint32_t now = millis();
    if (prevFixMs_ && now > prevFixMs_) {
      float dist = haversineM(prevLat_, prevLon_, lat, lon);
      float dt = (now - prevFixMs_) / 1000.0f;
      // >160 m/s implied ground speed from a pocket device = leap.
      if (dt > 0 && dist > 200.0f && (dist / dt) > 160.0f) {
        currentFix.spoofDetected = true;
        rejectedJumps++;
        if (rejectCount_ == 0 || haversineM(rejectLat_, rejectLon_, lat, lon) > 100.0f) {
          rejectLat_ = lat; rejectLon_ = lon;
          rejectFirstMs_ = rejectLastMs_ = now;
          rejectCount_ = 1;
        } else {
          rejectLastMs_ = now;
          if (rejectCount_ < 255) rejectCount_++;
        }
        // A persistent, self-consistent "new place" for 2 s is more
        // likely a receiver correction / genuine relocation than a
        // one-off spike, so accept it eventually rather than wedging
        // forever on the first noisy fix after boot.
        if (rejectCount_ < 3 || rejectLastMs_ - rejectFirstMs_ < 2000) return false;
      }
    }
    currentFix.spoofDetected = false;
    rejectCount_ = 0;
    prevLat_ = lat; prevLon_ = lon; prevFixMs_ = now;
    lastFixTime = now;
    acceptedFixes++;
    return true;
  }

  void processLine(String line) {
    line.trim();
    if (!checksumOk(line)) { checksumErrors++; return; }
    int star = line.lastIndexOf('*');
    String body = line.substring(0, star);
    String f[20];
    int n = split(body, f, 20);
    if (n < 8) return;
    active = true;
    sentencesParsed++;

    if (f[0].endsWith("GGA")) {
      // $GxGGA,time,lat,N/S,lon,E/W,quality,sats,hdop,alt,M,...
      currentFix.utcTime = f[1];
      currentFix.fixQuality = f[6].toInt();
      currentFix.satellites = f[7].toInt();
      currentFix.hdop = f[8].isEmpty() ? 99.9f : (float)atof(f[8].c_str());
      if (currentFix.fixQuality > 0 && !f[2].isEmpty() && !f[4].isEmpty()) {
        float candLat = nmeaToDeg(f[2], f[3]);
        float candLon = nmeaToDeg(f[4], f[5]);
        if (notePosition(candLat, candLon)) {
          currentFix.latitude = candLat;
          currentFix.longitude = candLon;
          currentFix.altitude = (float)atof(f[9].c_str());
          currentFix.valid = currentFix.satellites >= 4 &&
                               currentFix.hdop > 0 && currentFix.hdop <= 10.0f;
        } else if (acceptedFixes == 0) {
          currentFix.valid = false;
        }
      }
    } else if (f[0].endsWith("RMC")) {
      // $GxRMC,time,status,lat,N/S,lon,E/W,speedKn,course,date,...
      currentFix.utcTime = f[1];
      currentFix.utcDate = f[9];
      currentFix.speedKnots = (float)atof(f[7].c_str());
      currentFix.courseDeg = (float)atof(f[8].c_str());
      if (f[2] == "A" && !f[3].isEmpty() && !f[5].isEmpty()) {
        float candLat = nmeaToDeg(f[3], f[4]);
        float candLon = nmeaToDeg(f[5], f[6]);
        if (notePosition(candLat, candLon)) {
          currentFix.latitude = candLat;
          currentFix.longitude = candLon;
          // RMC carries no satellite/HDOP evidence of its own; only
          // call it valid when recent GGA evidence supports it.
          currentFix.valid = currentFix.satellites >= 4 &&
                               currentFix.hdop > 0 && currentFix.hdop <= 10.0f;
        } else if (acceptedFixes == 0) {
          currentFix.valid = false;
        }
      }
    }
  }
};

extern GnssHatDriver gnssHat;
