// AEGIS IR remote - TV control through the Cardputer ADV's built-in
// infrared emitter (GPIO44), plus optional raw learning through an
// external 38 kHz receiver module.
//
// Hardware truth:
//   * The ADV contains an IR *emitter* only. It can transmit immediately.
//   * It cannot hear a TV remote until a receiver such as a VS1838B /
//     TSOP38238 is wired as: OUT -> EXT GPIO1, VCC -> 3.3V, GND -> GND.
//   * Learned signals are stored as raw mark/space durations and replayed
//     through the ESP32 RMT peripheral, so no protocol decoder has to
//     guess what a proprietary remote sent.
//
// TV profiles live at /AEGIS/ir/tv.json, for example:
// {
//   "name": "Living room TV",
//   "protocol": "NEC",
//   "buttons": {
//     "power": {"address": 4660, "command": 17},
//     "vol_up": {"address": 4660, "command": 18}
//   }
// }
// "address"/"command" also accept strings such as "0x1234". A button may
// instead use {"raw_file":"power.json"}, where that file is a JSON array
// of microsecond durations in /AEGIS/ir/.
#pragma once
#include <Arduino.h>
#include <ArduinoJson.h>
#include <SD.h>
#include <driver/rmt.h>
#include <freertos/ringbuf.h>
#include <map>
#include <vector>

class IrRemote {
 public:
  struct Code {
    bool present = false;
    String protocol;
    uint32_t address = 0;
    uint32_t command = 0;
    int bits = 12;
    String rawFile;
    std::vector<uint32_t> rawDurations;
  };

  static constexpr int kButtonCount = 7;
  static const char* buttonKey(int index) {
    static const char* keys[kButtonCount] = {
      "power", "vol_up", "vol_down", "ch_up", "ch_down", "mute", "input"
    };
    return (index >= 0 && index < kButtonCount) ? keys[index] : "";
  }
  static const char* buttonLabel(int index) {
    static const char* labels[kButtonCount] = {
      "Power", "Volume +", "Volume -", "Channel +", "Channel -", "Mute", "Input"
    };
    return (index >= 0 && index < kButtonCount) ? labels[index] : "?";
  }

  // ---- Built-in TV code database -------------------------------------
  // Widely published remote codesets (LIRC/Flipper community data).
  // Button order matches buttonKey(): power, vol+, vol-, ch+, ch-, mute, input.
  struct Brand { const char* name; const char* protocol; uint32_t address; int bits; uint32_t cmd[7]; };
  static constexpr uint32_t kNoCmd = 0xFFFFFFFFu;  // button not in codeset
  static constexpr int kBrandCount = 6;
  static const Brand* brandAt(int i) {
    // Codesets from published LIRC/Flipper-IRDB data. kNoCmd marks buttons
    // a codeset doesn't publish (auto-find only needs Power, button 0;
    // the rest can be learned from the real remote with L afterwards).
    static const Brand brands[kBrandCount] = {
      {"Samsung TV",       "SAMSUNG", 0x07,   0,  {0x02, 0x07, 0x0B, 0x12, 0x10, 0x0F, 0x01}},
      {"LG TV (modern)",   "NEC",     0x20,   0,  {0x10, 0x40, 0xC0, 0x00, 0x80, 0x90, 0xD0}},  // NEC 20DF family (also many Vizio)
      {"LG TV (older)",    "NEC",     0x04,   0,  {0x08, 0x02, 0x03, 0x00, 0x01, 0x09, 0x0B}},
      {"Sony TV",          "SONY",    0x01,   12, {0x15, 0x12, 0x13, 0x10, 0x11, 0x14, 0x25}},
      {"Philips TV",       "RC5",     0x00,   0,  {0x0C, 0x10, 0x11, 0x20, 0x21, 0x0D, 0x38}},
      {"TCL Roku TV",      "NEC",     0xC7EA, 0,  {0x17, kNoCmd, kNoCmd, kNoCmd, kNoCmd, kNoCmd, kNoCmd}},  // NECext EA C7, Power 0x17
    };
    return (i >= 0 && i < kBrandCount) ? &brands[i] : nullptr;
  }

  void applyBrand(int i) {
    const Brand* b = brandAt(i);
    if (!b) return;
    codes_.clear();
    for (int k = 0; k < kButtonCount; k++) {
      Code c;
      c.present = b->cmd[k] != kNoCmd;
      c.protocol = b->protocol;
      c.address = b->address;
      c.command = b->cmd[k];
      c.bits = b->bits;
      codes_[buttonKey(k)] = c;
    }
    profileLoaded = true;
    profileName = b->name;
    status = String("using built-in: ") + b->name;
    Serial.printf("[IR] %s\n", status.c_str());
  }

  bool profileLoaded = false;
  String profileName = "no TV profile";
  String status = "TX ready; add /AEGIS/ir/tv.json";
  bool learning = false;
  int learnButton = -1;

  void loadProfile() {
    codes_.clear();
    profileLoaded = false;
    profileName = "no TV profile";
    if (!SD.exists("/AEGIS")) {
      status = "no SD card for TV profile";
      return;
    }
    if (!SD.exists("/AEGIS/ir")) SD.mkdir("/AEGIS/ir");
    auto addLearnedCodes = [&]() {
      for (int i = 0; i < kButtonCount; i++) {
        const String filename = String("learned_") + buttonKey(i) + ".json";
        if (!SD.exists(("/AEGIS/ir/" + filename).c_str())) continue;
        Code c;
        c.present = true;
        c.protocol = "RAW";
        c.rawFile = filename;
        codes_[buttonKey(i)] = c;
      }
    };

    const char* path = "/AEGIS/ir/tv.json";
    if (!SD.exists(path)) {
      addLearnedCodes();
      profileLoaded = !codes_.empty();
      if (profileLoaded) {
        profileName = "learned on Cardputer";
        status = "learned IR buttons loaded";
      } else {
        status = "TX ready; add /AEGIS/ir/tv.json";
      }
      return;
    }
    File f = SD.open(path, FILE_READ);
    if (!f) {
      status = "could not open TV profile";
      return;
    }
    JsonDocument doc;
    DeserializationError err = deserializeJson(doc, f);
    f.close();
    if (err) {
      status = "TV profile JSON invalid";
      return;
    }

    profileName = doc["name"].as<String>();
    if (!profileName.length()) profileName = "TV profile";
    const String defaultProtocol = doc["protocol"].as<String>();
    JsonObject buttons = doc["buttons"].as<JsonObject>();
    for (int i = 0; i < kButtonCount; i++) {
      JsonObject b = buttons[buttonKey(i)].as<JsonObject>();
      if (b.isNull()) continue;
      Code c;
      c.present = true;
      c.protocol = b["protocol"].as<String>();
      if (!c.protocol.length()) c.protocol = defaultProtocol;
      c.address = parseCode(b["address"]);
      c.command = parseCode(b["command"]);
      c.bits = b["bits"] | c.bits;
      c.rawFile = b["raw_file"].as<String>();
      codes_[buttonKey(i)] = c;
    }
    // A code learned on-device is the freshest evidence for that button,
    // so it replaces any stale profile entry after a reboot.
    addLearnedCodes();
    profileLoaded = !codes_.empty();
    status = profileLoaded ? ("profile loaded: " + profileName) : "TV profile has no known buttons";
    Serial.printf("[IR] %s (%u buttons)\n", status.c_str(), (unsigned)codes_.size());
  }

  bool hasButton(int index) const {
    auto it = codes_.find(buttonKey(index));
    return it != codes_.end() && it->second.present;
  }

  bool sendButton(int index) {
    const String key = buttonKey(index);
    auto it = codes_.find(key);
    if (it == codes_.end() || !it->second.present) {
      status = "no code stored for " + String(buttonLabel(index));
      return false;
    }
    Code& c = it->second;
    if (c.rawFile.length() && c.rawDurations.empty()) {
      if (!loadRawFile(c.rawFile, c.rawDurations)) {
        status = "could not load raw IR file";
        return false;
      }
    }
    bool ok = false;
    if (!c.rawDurations.empty()) {
      ok = sendRaw(c.rawDurations);
    } else {
      String protocol = c.protocol;
      protocol.toUpperCase();
      if (protocol == "NEC") ok = sendNec(c.address, c.command);
      else if (protocol == "SAMSUNG") ok = sendSamsung(c.address, c.command);
      else if (protocol == "SONY" || protocol == "SIRC") ok = sendSony(c.address, c.command, c.bits);
      else if (protocol == "RC5") ok = sendRc5(c.address, c.command);
      else status = "unsupported IR protocol: " + c.protocol;
    }
    if (ok) status = String("sent ") + buttonLabel(index);
    Serial.printf("[IR] send %s -> %d (%s)\n", key.c_str(), ok, status.c_str());
    return ok;
  }

  bool startLearning(int index) {
    if (learning) return true;
    if (!ensureRx()) {
      status = "IR RX unavailable on GPIO1";
      return false;
    }
    // Discard anything the receiver heard before learning was armed.
    if (rxRing_) {
      size_t itemSize = 0;
      while (void* stale = xRingbufferReceive(rxRing_, &itemSize, 0)) {
        vRingbufferReturnItem(rxRing_, stale);
      }
    }
    if (rmt_rx_start(kRxChannel, true) != ESP_OK) {
      status = "could not start IR receiver";
      return false;
    }
    learning = true;
    learnButton = index;
    learnStartedMs_ = millis();
    status = String("point remote at RX, press ") + buttonLabel(index);
    Serial.printf("[IR] learning %s on GPIO%d\n", buttonKey(index), AEGIS_IR_RX_PIN);
    return true;
  }

  // Non-blocking. Returns true on the loop iteration that finishes a
  // capture (or times out), so the caller can refresh the display once.
  bool pollLearning() {
    if (!learning) return false;
    if (millis() - learnStartedMs_ > 15000) {
      stopLearning();
      status = "IR learn timed out (RX on GPIO1?)";
      return true;
    }
    if (!rxRing_) return false;
    size_t itemSize = 0;
    rmt_item32_t* items = (rmt_item32_t*)xRingbufferReceive(rxRing_, &itemSize, 0);
    if (!items) return false;

    std::vector<uint32_t> durations;
    const size_t count = itemSize / sizeof(rmt_item32_t);
    for (size_t i = 0; i < count; i++) {
      appendDuration(durations, items[i].level0, items[i].duration0);
      appendDuration(durations, items[i].level1, items[i].duration1);
      if (durations.size() > 1024) break;
    }
    vRingbufferReturnItem(rxRing_, items);
    stopLearning();

    // A real remote frame is dozens of pulses. A handful is almost
    // always ambient-light noise on an unconnected receiver input.
    if (durations.size() < 12) {
      status = "IR noise ignored; no remote frame";
      return true;
    }
    while (!durations.empty() && durations.back() > 20000) durations.pop_back();
    const String key = buttonKey(learnButton);
    const String filename = "learned_" + key + ".json";
    if (!saveRawFile(filename, durations)) {
      status = "learned signal but SD save failed";
      return true;
    }
    Code& c = codes_[key];
    c.present = true;
    c.protocol = "RAW";
    c.rawFile = filename;
    c.rawDurations = durations;
    profileLoaded = true;
    if (profileName == "no TV profile") profileName = "learned on Cardputer";
    status = "learned " + String(buttonLabel(learnButton)) + " (" + String((unsigned)durations.size()) + " pulses)";
    Serial.printf("[IR] %s\n", status.c_str());
    learnButton = -1;
    return true;
  }

 private:
  static constexpr rmt_channel_t kTxChannel = RMT_CHANNEL_0;
  static constexpr rmt_channel_t kRxChannel = RMT_CHANNEL_1;
  std::map<String, Code> codes_;
  bool txReady_ = false;
  bool rxReady_ = false;
  RingbufHandle_t rxRing_ = nullptr;
  uint32_t learnStartedMs_ = 0;

  static uint32_t parseCode(JsonVariant v) {
    if (v.is<const char*>()) return strtoul(v.as<const char*>(), nullptr, 0);
    if (v.is<String>()) return strtoul(v.as<String>().c_str(), nullptr, 0);
    return v.as<uint32_t>();
  }

  static void appendDuration(std::vector<uint32_t>& out, uint32_t level, uint32_t duration) {
    if (!duration) return;
    if (duration > 20000 && !out.empty()) return;
    // RMT items should start with a mark (level 1). Allow the hardware
    // to settle by ignoring any leading space.
    if (out.empty() && level == 0) return;
    out.push_back(duration);
  }

  bool loadRawFile(const String& filename, std::vector<uint32_t>& out) const {
    String path = filename;
    if (!path.startsWith("/")) path = "/AEGIS/ir/" + path;
    File f = SD.open(path.c_str(), FILE_READ);
    if (!f) return false;
    JsonDocument doc;
    DeserializationError err = deserializeJson(doc, f);
    f.close();
    if (err) return false;
    out.clear();
    for (JsonVariant v : doc.as<JsonArray>()) out.push_back(v.as<uint32_t>());
    return out.size() >= 4;
  }

  bool saveRawFile(const String& filename, const std::vector<uint32_t>& durations) const {
    if (!SD.exists("/AEGIS")) return false;
    if (!SD.exists("/AEGIS/ir")) SD.mkdir("/AEGIS/ir");
    File f = SD.open(("/AEGIS/ir/" + filename).c_str(), FILE_WRITE);
    if (!f) return false;
    JsonDocument doc;
    JsonArray arr = doc.to<JsonArray>();
    for (uint32_t d : durations) arr.add(d);
    const bool ok = serializeJson(doc, f) > 0;
    f.close();
    return ok;
  }

  bool ensureTx() {
    if (txReady_) return true;
    rmt_config_t config = RMT_DEFAULT_CONFIG_TX((gpio_num_t)AEGIS_IR_TX_PIN, kTxChannel);
    config.clk_div = 80;  // 1 tick = 1 us
    config.tx_config.loop_en = false;
    config.tx_config.carrier_en = true;
    config.tx_config.carrier_freq_hz = 38000;
    config.tx_config.carrier_duty_percent = 33;
    config.tx_config.idle_output_en = true;
    config.tx_config.idle_level = RMT_IDLE_LEVEL_LOW;
    if (rmt_config(&config) != ESP_OK) return false;
    if (rmt_driver_install(config.channel, 0, 0) != ESP_OK) return false;
    txReady_ = true;
    return true;
  }

  bool ensureRx() {
    if (rxReady_) return true;
    rmt_config_t config = RMT_DEFAULT_CONFIG_RX((gpio_num_t)AEGIS_IR_RX_PIN, kRxChannel);
    config.clk_div = 80;  // 1 tick = 1 us
    config.rx_config.idle_threshold = 12000;
    config.rx_config.filter_en = true;
    config.rx_config.filter_ticks_thresh = 100;
    if (rmt_config(&config) != ESP_OK) return false;
    if (rmt_driver_install(config.channel, 4096, 0) != ESP_OK) return false;
    if (rmt_get_ringbuf_handle(config.channel, &rxRing_) != ESP_OK || !rxRing_) return false;
    rxReady_ = true;
    return true;
  }

  void stopLearning() {
    if (rxReady_) rmt_rx_stop(kRxChannel);
    learning = false;
  }

  bool sendRaw(const std::vector<uint32_t>& durations) {
    if (!ensureTx()) {
      status = "IR transmitter init failed";
      return false;
    }
    std::vector<rmt_item32_t> items;
    items.reserve((durations.size() + 1) / 2);
    for (size_t i = 0; i < durations.size(); i += 2) {
      rmt_item32_t item = {};
      item.level0 = 1;
      item.duration0 = min<uint32_t>(durations[i], 32767);
      if (i + 1 < durations.size()) {
        item.level1 = 0;
        item.duration1 = min<uint32_t>(durations[i + 1], 32767);
      }
      items.push_back(item);
    }
    if (items.empty()) return false;
    return rmt_write_items(kTxChannel, items.data(), items.size(), true) == ESP_OK;
  }

  static void addPulse(std::vector<uint32_t>& out, uint32_t mark, uint32_t space) {
    out.push_back(mark);
    out.push_back(space);
  }
  static void addBit(std::vector<uint32_t>& out, bool one, uint32_t mark, uint32_t zeroSpace, uint32_t oneSpace) {
    addPulse(out, mark, one ? oneSpace : zeroSpace);
  }
  static void addByteLsb(std::vector<uint32_t>& out, uint8_t value, uint32_t mark, uint32_t zeroSpace, uint32_t oneSpace) {
    for (int i = 0; i < 8; i++) addBit(out, (value >> i) & 1, mark, zeroSpace, oneSpace);
  }

  bool rc5Toggle_ = false;
  bool sendRc5(uint32_t address, uint32_t command) {
    // RC5: S1=1, S2=1, toggle, 5-bit address, 6-bit command (MSB first),
    // Manchester at ~36 kHz: bit 1 = mark+space, bit 0 = space+mark,
    // half-bit 889 us. Consecutive half-bits are merged for the RMT.
    uint32_t frame = (1u << 13) | (1u << 12) | ((rc5Toggle_ ? 1u : 0u) << 11) |
                     ((address & 0x1F) << 6) | (command & 0x3F);
    rc5Toggle_ = !rc5Toggle_;
    std::vector<uint32_t> durations;
    int level = -1;
    uint32_t run = 0;
    auto half = [&](int lv) {
      if (lv == level) { run += 889; }
      else {
        if (level >= 0) durations.push_back(run);
        level = lv; run = 889;
      }
    };
    for (int i = 13; i >= 0; i--) {
      const bool bit = (frame >> i) & 1;
      if (bit) { half(1); half(0); } else { half(0); half(1); }
    }
    if (level >= 0) durations.push_back(run);
    return sendRaw(durations);
  }

  bool sendNec(uint32_t address, uint32_t command) {
    std::vector<uint32_t> d;
    addPulse(d, 9000, 4500);
    if (address <= 0xFF) {
      addByteLsb(d, (uint8_t)address, 560, 560, 1690);
      addByteLsb(d, (uint8_t)~address, 560, 560, 1690);
    } else {
      addByteLsb(d, (uint8_t)address, 560, 560, 1690);
      addByteLsb(d, (uint8_t)(address >> 8), 560, 560, 1690);
    }
    addByteLsb(d, (uint8_t)command, 560, 560, 1690);
    addByteLsb(d, (uint8_t)~command, 560, 560, 1690);
    d.push_back(560);
    return sendRaw(d);
  }

  bool sendSamsung(uint32_t address, uint32_t command) {
    std::vector<uint32_t> d;
    addPulse(d, 4500, 4500);
    addByteLsb(d, (uint8_t)address, 560, 560, 1690);
    addByteLsb(d, (uint8_t)address, 560, 560, 1690);
    addByteLsb(d, (uint8_t)command, 560, 560, 1690);
    addByteLsb(d, (uint8_t)~command, 560, 560, 1690);
    d.push_back(560);
    return sendRaw(d);
  }

  bool sendSony(uint32_t address, uint32_t command, int bits) {
    if (bits < 12) bits = 12;
    if (bits > 20) bits = 20;
    const uint32_t value = ((address & 0x1F) << 7) | (command & 0x7F);
    for (int repeat = 0; repeat < 3; repeat++) {
      std::vector<uint32_t> d;
      addPulse(d, 2400, 600);
      for (int i = 0; i < bits; i++) addPulse(d, ((value >> i) & 1) ? 1200 : 600, 600);
      if (!sendRaw(d)) return false;
      if (repeat < 2) delay(25);
    }
    return true;
  }
};
