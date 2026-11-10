// AEGIS RFID2 (WS1850S / MFRC522-class, I2C) - defensive, read-only.
//
// The Cardputer ADV has NO built-in RFID/NFC. This driver supports the
// external M5 RFID Unit 2 (WS1850S) on the Grove I2C bus (SDA G8/SCL G9,
// shared with the keyboard) purely as a tag-presence sensor: it polls
// for ISO14443-A cards and reports UIDs. It never writes cards, never
// emulates anything, and if no reader answers on the bus it stays
// completely inert (one version-register probe every few seconds).
#pragma once
#include <Arduino.h>
#include <Wire.h>
#include "board_config.h"

class Rfid2Unit {
 public:
  // MFRC522 register subset (I2C framing: addr<<1 | R/W handled by Wire).
  static constexpr uint8_t kI2cAddr = 0x28;  // WS1850S default

  bool present = false;
  bool enabled = false;    // opt-in toggle (host persists it); OFF = never probed
  void (*onProbeStart)() = nullptr;  // host hook: fired right before a probe
  uint8_t version = 0;
  String lastUid = "";
  uint32_t seenCount = 0;
  bool tagEventPending = false;
  String pendingUid = "";

  // Called once at boot; cheap no-op probe, retried by poll().
  void begin() { if (enabled) probe(); }

  void poll() {
    if (!enabled) return;   // toggled off: this driver never touches the bus
    const uint32_t now = millis();
    if (!present) {
      // Was every 5 s. The v0.7.6 hardware report pinned the whole-UI
      // freeze inside this re-probe of an ABSENT reader (a Wire call
      // that never returns). Absent hardware now re-probes rarely, and
      // the boot-stage quarantine in agentSetup disables probing
      // entirely after a boot that died here.
      if (now - lastProbeMs_ > 30000) probe();
      return;
    }
    if (now - lastPollMs_ < 400) return;
    lastPollMs_ = now;
    if (!wakeupAndRequest()) return;
    String uid;
    if (!anticollUid(uid)) return;
    seenCount++;
    if (uid != lastUid || now - lastEventMs_ > 3000) {
      lastUid = uid;
      lastEventMs_ = now;
      pendingUid = uid;
      tagEventPending = true;
    }
  }

 private:
  uint32_t lastProbeMs_ = 0;
  uint32_t lastPollMs_ = 0;
  uint32_t lastEventMs_ = 0;

  bool writeReg(uint8_t reg, uint8_t val) {
    Wire.beginTransmission(kI2cAddr);
    Wire.write(reg);
    Wire.write(val);
    return Wire.endTransmission() == 0;
  }
  int readReg(uint8_t reg) {
    Wire.beginTransmission(kI2cAddr);
    Wire.write(0x80 | (reg & 0x3F));  // MFRC522 I2C read framing
    if (Wire.endTransmission(false) != 0) return -1;
    if (Wire.requestFrom((int)kI2cAddr, 1) != 1) return -1;
    return Wire.read();
  }

  bool probe() {
    if (onProbeStart) onProbeStart();   // deterministic stage persist (host)
    lastProbeMs_ = millis();
    const int v = readReg(0x37);  // VersionReg
    if (v < 0) { present = false; return false; }
    version = (uint8_t)v;
    present = (v == 0x82 || v == 0x88 || v == 0x90 || v == 0x91 || v == 0x92);
    if (!present) return false;
    // MFRC522 soft reset + standard ISO14443-A init.
    writeReg(0x01, 0x0F);            // CommandReg: SoftReset
    delay(5);
    writeReg(0x2A, 0x8D);            // TModeReg: TAuto, prescaler hi
    writeReg(0x2B, 0x3E);            // TPrescalerReg
    writeReg(0x2D, 30);              // TReloadReg low
    writeReg(0x2C, 0);               // TReloadReg high
    writeReg(0x15, 0x40);            // TxASKReg: 100% ASK
    writeReg(0x11, 0x3D);            // ModeReg: CRC preset 0x6363
    const int txc = readReg(0x14);   // TxControlReg: antenna on
    if (txc >= 0 && (txc & 0x03) != 0x03) writeReg(0x14, txc | 0x03);
    Serial.printf("[RFID2] reader present, version 0x%02X\n", version);
    return true;
  }

  // One MFRC522 transceive round; returns FIFO byte count or -1.
  int transceive(const uint8_t* send, int sendLen, uint8_t* back, int backMax) {
    writeReg(0x01, 0x00);            // Idle
    writeReg(0x04, 0x7F);            // ComIrqReg clear
    writeReg(0x0A, 0x80);            // FIFOLevelReg: flush
    for (int i = 0; i < sendLen; i++) writeReg(0x09, send[i]);
    writeReg(0x01, 0x0C);            // Transceive
    writeReg(0x0D, 0x80);            // BitFramingReg: StartSend
    uint32_t start = millis();
    int irq = 0;
    while (millis() - start < 40) {
      irq = readReg(0x04);
      if (irq < 0) return -1;
      if (irq & 0x30) break;         // RxIrq / IdleIrq
      if (irq & 0x01) return -1;     // TimerIrq: no card answered
    }
    writeReg(0x0D, 0x00);
    const int err = readReg(0x06);   // ErrorReg
    if (err < 0 || (err & 0x13)) return -1;  // BufferOvfl/Parity/Protocol (not CollErr)
    const int n = readReg(0x0A);
    if (n <= 0) return -1;
    const int take = n < backMax ? n : backMax;
    for (int i = 0; i < take; i++) {
      const int b = readReg(0x09);
      if (b < 0) return -1;
      back[i] = (uint8_t)b;
    }
    return take;
  }

  bool wakeupAndRequest() {
    writeReg(0x0D, 0x07);            // 7-bit short frame (REQA)
    const uint8_t reqa = 0x26;
    uint8_t atqa[2] = {0};
    return transceive(&reqa, 1, atqa, 2) == 2;
  }

  bool anticollUid(String& out) {
    writeReg(0x0D, 0x00);            // full-byte framing
    const uint8_t cmd[2] = {0x93, 0x20};  // SEL_CL1 + NVB
    uint8_t resp[5] = {0};
    if (transceive(cmd, 2, resp, 5) != 5) return false;
    if ((resp[0] ^ resp[1] ^ resp[2] ^ resp[3]) != resp[4]) return false;  // BCC
    char buf[16];
    snprintf(buf, sizeof(buf), "%02X%02X%02X%02X", resp[0], resp[1], resp[2], resp[3]);
    out = buf;
    return true;
  }
};
