/**
 * AEGIS LoRa driver - Cap LoRa-1262 Semtech SX1262 via RadioLib.
 * ------------------------------------------------------------------
 * REAL radio driver (replaces the old stub, which read SX1276 register
 * 0x42 that the SX1262 does not have, and therefore could never detect
 * anything). SX1262 is a command-protocol radio (BUSY/DIO1 handshake);
 * RadioLib implements that protocol correctly.
 *
 * Design rules:
 *  - Shares the FSPI bus with the microSD card (separate CS lines).
 *  - RX-first blue-team posture: continuous receive, interrupt-driven
 *    packet capture with RSSI/SNR, noise-floor/jam indication.
 *  - Tries to enable the Cap's RF path: DIO2-as-RF-switch on the SX1262
 *    plus the PI4IOE5V6408 IO-expander P0 (SX_ANT_SW) over I2C, best
 *    effort at 0x20 - skipped silently if the expander does not ACK.
 *  - `active` is only true after SX1262 begin() succeeds; with no Cap
 *    attached the driver is inert and reports inactive.
 */

#pragma once
#include <Arduino.h>
#include <RadioLib.h>
#include <Wire.h>
#include "board_config.h"

class LoRaHatDriver {
 public:
  bool active = false;
  uint32_t frequencyHz = (uint32_t)(AEGIS_LORA_FREQ_MHZ * 1e6f);
  uint32_t packetsCaptured = 0;
  float lastSnr = 0.0f;
  int lastRssi = -120;
  int channelRssi = -120;
  bool jammingDetected = false;
  String lastPacketHex = "";
  // Channel-activity sweep results (press R on the CAP screen)
  float scanFreq[8] = {0};
  int scanRssi[8] = {0};
  int scanCount = 0;
  int scanIdx_ = 0;
  uint32_t scanNextMs_ = 0;

  // RX-only sweep across the US915 band (EU users: adjust the list).
  // Stepped channel sweep: one channel per poll call (~250 ms apart),
  // so the keyboard and UI stay alive during the ~2 s sweep.
  bool scanBusy = false;
  bool scanActivity() {   // now only *starts* the sweep
    if (!active || !radio_ || scanBusy) return scanBusy;
    scanBusy = true;
    scanIdx_ = 0;
    scanNextMs_ = 0;
    return true;
  }
  void pollScan() {
    if (!scanBusy || !radio_) return;
    static const float freqs[8] = {902.3f, 904.0f, 905.7f, 907.4f, 909.1f, 910.8f, 912.5f, 914.2f};
    const uint32_t now = millis();
    if (now < scanNextMs_) return;
    if (scanIdx_ > 0) {   // previous channel has dwelled 250 ms: sample it
      scanFreq[scanIdx_ - 1] = freqs[scanIdx_ - 1];
      scanRssi[scanIdx_ - 1] = (int)radio_->getRSSI(false);
      scanCount = scanIdx_;
    }
    if (scanIdx_ >= 8) {
      radio_->setFrequency(AEGIS_LORA_FREQ_MHZ);
      radio_->startReceive();
      scanCount = 8;
      scanBusy = false;
      return;
    }
    radio_->setFrequency(freqs[scanIdx_]);
    radio_->startReceive();
    scanIdx_++;
    scanNextMs_ = now + 250;
  }
  uint32_t lastPacketAt = 0;
  String lastPacketKind = "";
  String lastPacketDetail = "";
  int lastPacketLen = 0;
  int beginError = 0;

  bool begin(float freqMHz = AEGIS_LORA_FREQ_MHZ) {
    enableAntennaSwitch();
    if (!radio_) {
      radio_ = new SX1262(new Module(AEGIS_LORA_NSS, AEGIS_LORA_DIO1,
                                     AEGIS_LORA_RST, AEGIS_LORA_BUSY));
    }
    frequencyHz = (uint32_t)(freqMHz * 1e6f);
    pinMode(AEGIS_LORA_BUSY, INPUT);
    Serial.printf("[INIT] SX1262 begin (BUSY pin reads %d; floating/high with no Cap is expected to fail gracefully)\n", digitalRead(AEGIS_LORA_BUSY));
    int st = radio_->begin(freqMHz, AEGIS_LORA_BW_KHZ, AEGIS_LORA_SF, AEGIS_LORA_CR,
                           AEGIS_LORA_SYNC, AEGIS_LORA_PWR_DBM, 8, AEGIS_LORA_TCXO_V, false);
    beginError = st;
    if (st != RADIOLIB_ERR_NONE) {
      active = false;
      Serial.printf("[LoRa] SX1262 not detected (RadioLib err %d) - Cap absent or antenna/RF path off\n", st);
      return false;
    }
    radio_->setDio2AsRfSwitch(true);
    radio_->setPacketReceivedAction(onPacket_);
    st = radio_->startReceive();
    if (st != RADIOLIB_ERR_NONE) {
      active = false;
      Serial.printf("[LoRa] startReceive failed (err %d)\n", st);
      return false;
    }
    active = true;
    Serial.printf("[LoRa] SX1262 listening on %.1f MHz SF%d BW%.0f (US915 default; set region in config)\n",
                  freqMHz, AEGIS_LORA_SF, (double)AEGIS_LORA_BW_KHZ);
    return true;
  }

  // Transmit one short packet at the configured power (v0.12.9),
  // then return to continuous receive. Blocking for the airtime
  // (~0.3 s for a beacon at SF9) - callers keep packets rare.
  bool transmit(const String& payload) {
    if (!active || !radio_ || !payload.length()) return false;
    String copy = payload;   // this RadioLib wants String& (non-const)
    const int st = radio_->transmit(copy);
    radio_->startReceive();   // always re-arm RX, success or not
    return st == RADIOLIB_ERR_NONE;
  }

  // Packet callback: void cb(hexPayload, rssiDbm, snrDb)
  void poll(void (*onPacket)(const String& hexData, int rssi, float snr)) {
    if (!active || !radio_) return;
    // Noise floor / crude CW-jam indication, sampled at most every 2 s.
    if (millis() - lastNoiseCheck_ > 2000) {
      lastNoiseCheck_ = millis();
      channelRssi = (int)radio_->getRSSI(false);
      jammingDetected = channelRssi > -55;   // band should idle far below this
    }
    if (!rxFlag_) return;
    rxFlag_ = false;
    size_t len = radio_->getPacketLength();
    if (len == 0 || len > 255) { radio_->startReceive(); return; }
    uint8_t buf[255];
    int st = radio_->readData(buf, len);
    if (st == RADIOLIB_ERR_NONE) {
      lastRssi = (int)radio_->getRSSI();
      lastSnr = radio_->getSNR();
      packetsCaptured++;
      lastPacketAt = millis();
      lastPacketLen = (int)len;
      // Lightweight wire decode. LoRaWAN has a parseable MAC header;
      // Meshtastic payloads are AES-encrypted on-air (channel key), so
      // we identify the shape honestly instead of faking a decode.
      {
        const uint8_t mtype = buf[0] >> 5;
        if (len >= 6 && memcmp(buf, "AEGIS1", 6) == 0) {
          // Family presence beacon (v0.12.9): "AEGIS1 <PROG> <fw> <mac6>"
          char det[64];
          size_t cplen = len < sizeof(det) - 1 ? len : sizeof(det) - 1;
          memcpy(det, buf, cplen); det[cplen] = 0;
          lastPacketKind = "AEGIS beacon";
          lastPacketDetail = det;
        } else if (len >= 12 && mtype <= 6) {
          char det[72];
          const uint32_t devAddr = (uint32_t)buf[1] | ((uint32_t)buf[2] << 8) |
                                   ((uint32_t)buf[3] << 16) | ((uint32_t)buf[4] << 24);
          const uint16_t fcnt = len >= 8 ? (buf[6] | (buf[7] << 8)) : 0;
          snprintf(det, sizeof(det), "mtype %u DevAddr %08lX FCnt %u len %u",
                   mtype, (unsigned long)devAddr, fcnt, (unsigned)len);
          lastPacketKind = "LoRaWAN";
          lastPacketDetail = det;
        } else if (len >= 16) {
          lastPacketKind = "LoRa/Meshtastic?";
          lastPacketDetail = "len " + String((unsigned)len) + " payload encrypted/other";
        } else {
          lastPacketKind = "LoRa raw";
          lastPacketDetail = "len " + String((unsigned)len);
        }
      }
      lastPacketHex = "";
      char tmp[3];
      for (size_t i = 0; i < len; i++) { snprintf(tmp, sizeof(tmp), "%02X", buf[i]); lastPacketHex += tmp; }
      if (onPacket) onPacket(lastPacketHex, lastRssi, lastSnr);
    }
    radio_->startReceive();   // re-arm continuous RX
  }

 private:
  SX1262* radio_ = nullptr;
  static volatile bool rxFlag_;
  uint32_t lastNoiseCheck_ = 0;

  static void onPacket_() { rxFlag_ = true; }

  // PI4IOE5V6408: config reg 0x03 (0=output), output reg 0x01. P0=SX_ANT_SW.
  static void enableAntennaSwitch() {
    Wire.begin(AEGIS_I2C_SDA, AEGIS_I2C_SCL);  // In_I2C bus (keyboard/IMU/codec share it)
    for (uint8_t addr : {0x20, 0x21}) {
      Wire.beginTransmission(addr);
      Wire.write(0x03); Wire.write(0xFE);          // P0 output, rest inputs
      if (Wire.endTransmission() != 0) continue;
      Wire.beginTransmission(addr);
      Wire.write(0x01); Wire.write(0x01);          // P0 HIGH
      if (Wire.endTransmission() == 0) {
        Serial.printf("[LoRa] PI4IOE @0x%02X: SX_ANT_SW enabled\n", addr);
        return;
      }
    }
    Serial.println("[LoRa] PI4IOE not found (relying on DIO2 RF switch)");
  }
};

extern LoRaHatDriver loraHat;
volatile bool LoRaHatDriver::rxFlag_ = false;  // single-TU firmware: definition lives here
