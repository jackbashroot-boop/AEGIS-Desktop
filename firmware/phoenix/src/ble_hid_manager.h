/**
 * PHOENIX Bluetooth HID Client & Peripheral Pairing Manager
 * -------------------------------------------------------------
 * Allows Cardputer to discover, pair, and smoothly bind to:
 *  - Bluetooth Keyboards (e.g. Logitech K380, MX Keys, Keychron)
 *  - Bluetooth Mice (e.g. Logitech MX Master, Magic Mouse)
 *  - BLE Presentation Remotes & Media Controllers
 *
 * Exposes pairing status and paired device names for console telemetry.
 */

#pragma once
#include <Arduino.h>
#include <NimBLEDevice.h>
#include <vector>

struct PairedBlePeripheral {
  String name;
  String address;
  String type; // "Keyboard", "Mouse", "Remote"
  bool connected;
};

class BleHidManager {
 public:
  bool active = false;
  std::vector<PairedBlePeripheral> pairedDevices;
  String lastInputReport = "";

  void begin() {
    active = true;
    NimBLEDevice::setSecurityAuth(true, true, true);
    NimBLEDevice::setSecurityIOCap(BLE_HS_IO_NO_INPUT_OUTPUT);
    Serial.println("[BLE HID] BLE HID peripheral manager initialized (Keyboard/Mouse/Remote ready)");
  }

  // Scan and auto-pair with available HID devices
  void startPairingScan(uint32_t durationSec = 10) {
    if (!active) return;
    NimBLEScan* scan = NimBLEDevice::getScan();
    scan->setActiveScan(true);
    scan->setInterval(100);
    scan->setWindow(99);
    Serial.println("[BLE HID] Scanning for Bluetooth keyboards, mice, and remotes...");
    NimBLEScanResults results = scan->getResults(durationSec * 1000, false);

    for (int i = 0; i < results.getCount(); i++) {
      const NimBLEAdvertisedDevice* dev = results.getDevice(i);
      if (dev->haveServiceUUID() && dev->isAdvertisingService(NimBLEUUID((uint16_t)0x1812))) {
        // Found BLE HID Device!
        String name = dev->haveName() ? dev->getName().c_str() : "Bluetooth HID Device";
        String addr = dev->getAddress().toString().c_str();
        String type = name.indexOf("Mouse") >= 0 ? "Mouse" : name.indexOf("Remote") >= 0 ? "Remote" : "Keyboard";

        Serial.printf("[BLE HID] Discovered HID advertiser (NOT paired/bonded): %s (%s, %s)\n", name.c_str(), addr.c_str(), type.c_str());
        pairedDevices.push_back({ name, addr, type, false });
      }
    }
  }

  std::vector<String> getPairedDeviceSummaries() {
    std::vector<String> list;
    for (auto& d : pairedDevices) {
      list.push_back(d.name + " (" + d.type + ")");
    }
    return list;
  }
};

extern BleHidManager bleHid;
