#pragma once
// Optional presence sensors (v0.12.11) - auto-detecting, zero-impact
// when absent. Nothing here blocks, allocates per-loop, or touches the
// radios; both drivers simply stay silent until hardware answers.
//
// LD2410 24 GHz mmWave (presence / moving / still + distance): UART on
// the Grove port, ESP RX=G1 <- sensor TX, ESP TX=G2 -> sensor RX at
// the LD2410's default 256000 8N1. G1/G2 are shared with the ESP32-C5
// hot-swap link and the optional IR receiver, so the LD2410 is only
// probed while C5 hot-swap is DISABLED in SETTINGS (the C5 wins its
// pins, exactly like the IR receiver rule). Wiring: sensor VCC to
// Grove 5V, GND to GND.
//
// VL53L1X ToF laser distance: I2C 0x29 on the shared In_I2C bus
// (SDA G8 / SCL G9). Detection only in this build - the part answers
// its address poll; full ranging init lands when hardware is on the
// bench to verify against.

#include <Arduino.h>
#include <Wire.h>

struct Ld2410State {
  bool present = false;     // a target is in front of the sensor
  bool moving = false;
  bool still = false;
  uint16_t moveCm = 0;
  uint16_t stillCm = 0;
  uint32_t lastFrameMs = 0; // last well-formed frame
  bool everSeen = false;    // at least one frame ever parsed
};

static Ld2410State g_ld2410;
static bool g_tofPresent = false;

static HardwareSerial g_ldSerial(2);
static bool g_ldStarted = false;
static uint32_t g_tofNextProbeMs = 0;

// LD2410 basic report frame:
// F4 F3 F2 F1 | len LE | 0x02 0xAA | state | moveCm LE | moveEnergy |
// stillCm LE | stillEnergy | detectCm LE | F8 F7 F6 F5
// state: 0 none, 1 moving, 2 still, 3 both.
static uint8_t g_ldBuf[48];
static int g_ldLen = 0;

static void ld2410Feed(uint8_t b) {
  if (g_ldLen == 0 && b != 0xF4) return;
  if (g_ldLen == 1 && b != 0xF3) { g_ldLen = 0; return; }
  if (g_ldLen == 2 && b != 0xF2) { g_ldLen = 0; return; }
  if (g_ldLen == 3 && b != 0xF1) { g_ldLen = 0; return; }
  if (g_ldLen < (int)sizeof(g_ldBuf)) g_ldBuf[g_ldLen++] = b; else g_ldLen = 0;
  if (g_ldLen >= 10) {
    const int payLen = g_ldBuf[4] | (g_ldBuf[5] << 8);
    const int total = 6 + payLen + 4;
    if (total > (int)sizeof(g_ldBuf)) { g_ldLen = 0; return; }
    if (g_ldLen == total) {
      if (g_ldBuf[total - 4] == 0xF8 && g_ldBuf[total - 3] == 0xF7 &&
          g_ldBuf[total - 2] == 0xF6 && g_ldBuf[total - 1] == 0xF5 &&
          payLen >= 9 && g_ldBuf[6] == 0x02) {
        const uint8_t st = g_ldBuf[8];
        g_ld2410.moving = (st & 0x01) != 0;
        g_ld2410.still = (st & 0x02) != 0;
        g_ld2410.present = st != 0;
        g_ld2410.moveCm = g_ldBuf[9] | (g_ldBuf[10] << 8);
        g_ld2410.stillCm = g_ldBuf[12] | (g_ldBuf[13] << 8);
        g_ld2410.lastFrameMs = millis();
        g_ld2410.everSeen = true;
      }
      g_ldLen = 0;
    }
  }
}

// Call every loop. Starts the UART once (after Wi-Fi settle), drains
// bytes non-blocking, probes the ToF address at most every 30 s.
static void sensorsTick(bool c5Enabled) {
  if (!g_ldStarted && !c5Enabled && millis() > 8000) {
    g_ldSerial.begin(256000, SERIAL_8N1, AEGIS_C5_RX_PIN, AEGIS_C5_TX_PIN);
    g_ldSerial.setTimeout(0);
    g_ldStarted = true;
  }
  if (g_ldStarted && !c5Enabled) {
    int guard = 256;
    while (g_ldSerial.available() > 0 && guard-- > 0) {
      ld2410Feed((uint8_t)g_ldSerial.read());
    }
    if (g_ld2410.present && millis() - g_ld2410.lastFrameMs > 3000) {
      g_ld2410.present = g_ld2410.moving = g_ld2410.still = false;
    }
  }
  if ((int32_t)(millis() - g_tofNextProbeMs) >= 0) {
    g_tofNextProbeMs = millis() + 30000;
    static bool wireBegun = false;   // never probe an unbegun bus (v0.12.15)
    if (!wireBegun) { Wire.begin(AEGIS_I2C_SDA, AEGIS_I2C_SCL); Wire.setTimeOut(50); wireBegun = true; }
    Wire.beginTransmission(0x29);
    g_tofPresent = Wire.endTransmission() == 0;
  }
}

static String ld2410Summary() {
  if (!g_ld2410.everSeen) return "LD2410 not detected (Grove G1/G2)";
  if (!g_ld2410.present) return "LD2410 on Grove - no target";
  String s = "LD2410 ";
  if (g_ld2410.moving) s += "MOVING " + String(g_ld2410.moveCm) + "cm ";
  if (g_ld2410.still) s += "STILL " + String(g_ld2410.stillCm) + "cm";
  return s;
}
