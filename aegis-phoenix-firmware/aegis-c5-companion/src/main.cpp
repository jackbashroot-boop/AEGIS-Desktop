// AEGIS ESP32-C5 companion firmware
// -------------------------------------------------------------------
// Runs on an ESP32-C5 board wired to the Cardputer ADV Grove port.
// The ADV auto-detects this firmware (AEGIS_HELLO -> +AEGIS:C5,...),
// then commands it to monitor 5 GHz with no user input:
//   * promiscuous deauth/disassoc detection with channel hopping
//   * periodic 5 GHz AP scans (results streamed back as +AP5G lines)
//   * optional 5 GHz station join via console command
//
// Wiring (3.3 V logic - never connect 5 V to a GPIO):
//   C5 TX -> ADV G1 (RX)     C5 RX <- ADV G2 (TX)     GND <-> GND
// Power the C5 separately (its own USB-C charger/power bank is fine).
//
// Set LINK_RX_PIN / LINK_TX_PIN below (or via -D build flags) to the
// C5 pins you actually wired. Defaults suit many dev boards; on a
// XIAO ESP32C5 use the D-pins you solder/jumper to the Grove cable.

#include <Arduino.h>
#include <WiFi.h>
#include <esp_wifi.h>

#ifndef LINK_RX_PIN
#define LINK_RX_PIN 8
#endif
#ifndef LINK_TX_PIN
#define LINK_TX_PIN 9
#endif
#define LINK_BAUD 115200
#define COMPANION_FW "aegis-c5-0.1.0"

static HardwareSerial Link(1);
static bool sniffEnabled = false;
static bool periodicScanEnabled = false;
static bool connectReported = false;
static uint32_t lastPeriodicScanMs = 0;
static uint32_t lastHopMs = 0;
static size_t hopIndex = 0;

// US 5 GHz channels (adjust to your regulatory domain if needed).
static const uint8_t kChannels5G[] = {
  36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108, 112, 116, 120, 124,
  128, 132, 136, 140, 144, 149, 153, 157, 161, 165
};

static volatile uint32_t deauthThisSecond = 0;
static String lastDeauthMac = "";

static String macString(const uint8_t* mac) {
  char buf[18];
  snprintf(buf, sizeof(buf), "%02x:%02x:%02x:%02x:%02x:%02x",
           mac[0], mac[1], mac[2], mac[3], mac[4], mac[5]);
  return String(buf);
}

static void IRAM_ATTR sniffer(void* buf, wifi_promiscuous_pkt_type_t type) {
  const wifi_promiscuous_pkt_t* pkt = (wifi_promiscuous_pkt_t*)buf;
  const uint8_t* frame = pkt->payload;
  if (pkt->rx_ctrl.sig_len < 24) return;
  const uint8_t subtype = frame[0] & 0xF0;
  if (type == WIFI_PKT_MGMT && (subtype == 0xC0 || subtype == 0xA0)) {
    deauthThisSecond++;
    lastDeauthMac = macString(frame + 10);
  }
}

static void sendScanResults() {
  const int n = WiFi.scanNetworks(false, true, false, 150);
  int sent = 0;
  for (int i = 0; i < n; i++) {
    const int channel = WiFi.channel(i);
    if (channel < 32) continue;  // 5 GHz only (2.4 GHz channels are 1-14)
    String ssid = WiFi.SSID(i);
    ssid.replace("\"", "'");
    ssid.replace("\n", " ");
    Link.printf("+AP5G:\"%s\",\"%s\",%d,%d\n", ssid.c_str(),
                WiFi.BSSIDstr(i).c_str(), WiFi.RSSI(i), channel);
    sent++;
  }
  WiFi.scanDelete();
  Link.printf("+SCAN5G_DONE:%d\n", sent);
}

static void handleCommand(String line) {
  line.trim();
  if (line == "AEGIS_HELLO" || line == "AT") {
    Link.printf("+AEGIS:C5,%s,%s\n", COMPANION_FW, WiFi.macAddress().c_str());
    Link.println("OK");
  } else if (line == "AT+PING") {
    Link.printf("+PONG:%lu\n", (unsigned long)millis());
    Link.println("OK");
  } else if (line == "AT+WIFI5G=1") {
    WiFi.mode(WIFI_STA);
    Link.println("OK");
  } else if (line == "AT+SNIFF5G=1") {
    sniffEnabled = true;
    esp_wifi_set_promiscuous_rx_cb(&sniffer);
    esp_wifi_set_promiscuous(true);
    Link.println("OK");
  } else if (line == "AT+SNIFF5G=0") {
    sniffEnabled = false;
    esp_wifi_set_promiscuous(false);
    Link.println("OK");
  } else if (line == "AT+SCAN5G=1") {
    periodicScanEnabled = true;
    lastPeriodicScanMs = 0;
    Link.println("OK");
  } else if (line == "AT+SCAN5G=0") {
    periodicScanEnabled = false;
    Link.println("OK");
  } else if (line == "AT+CWLAP5G") {
    sendScanResults();
  } else if (line.startsWith("AT+CWJAP5G=")) {
    int q1 = line.indexOf('"');
    int q2 = q1 >= 0 ? line.indexOf('"', q1 + 1) : -1;
    int q3 = q2 >= 0 ? line.indexOf('"', q2 + 1) : -1;
    int q4 = q3 >= 0 ? line.indexOf('"', q3 + 1) : -1;
    if (q1 >= 0 && q4 > q3) {
      WiFi.begin(line.substring(q1 + 1, q2).c_str(), line.substring(q3 + 1, q4).c_str());
      connectReported = false;
    }
    Link.println("OK");
  } else if (line.length()) {
    Link.println("ERROR");
  }
}

void setup() {
  Serial.begin(115200);
  Link.begin(LINK_BAUD, SERIAL_8N1, LINK_RX_PIN, LINK_TX_PIN);
  WiFi.mode(WIFI_STA);
  WiFi.disconnect();
  Serial.printf("[AEGIS-C5] %s link UART RX=%d TX=%d @%d\n",
                COMPANION_FW, LINK_RX_PIN, LINK_TX_PIN, LINK_BAUD);
  // Announce ourselves once; the ADV also probes every 2.5 s, so a
  // board plugged in later is still detected automatically.
  Link.printf("+AEGIS:C5,%s,%s\n", COMPANION_FW, WiFi.macAddress().c_str());
}

void loop() {
  while (Link.available()) {
    String line = Link.readStringUntil('\n');
    handleCommand(line);
  }

  const uint32_t now = millis();

  // 1-second deauth accounting -> unsolicited detector events.
  static uint32_t windowStart = 0;
  if (now - windowStart >= 1000) {
    windowStart = now;
    const uint32_t count = deauthThisSecond;
    deauthThisSecond = 0;
    if (sniffEnabled && count > 0) {
      Link.printf("+5G_DEAUTH:%lu,%s\n", (unsigned long)count, lastDeauthMac.c_str());
    }
  }

  // Channel-hop the 5 GHz monitor (a joined station locks the channel).
  if (sniffEnabled && WiFi.status() != WL_CONNECTED && now - lastHopMs > 350) {
    lastHopMs = now;
    hopIndex = (hopIndex + 1) % (sizeof(kChannels5G) / sizeof(kChannels5G[0]));
    esp_wifi_set_channel(kChannels5G[hopIndex], WIFI_SECOND_CHAN_NONE);
  }

  if (periodicScanEnabled && now - lastPeriodicScanMs > 30000) {
    lastPeriodicScanMs = now;
    sendScanResults();
  }

  // Report a completed 5 GHz join exactly once per association.
  if (WiFi.status() == WL_CONNECTED && !connectReported) {
    connectReported = true;
    Link.printf("+WIFI5G:CONNECTED,\"%s\",%d\n", WiFi.SSID().c_str(), WiFi.RSSI());
  } else if (WiFi.status() != WL_CONNECTED && connectReported) {
    connectReported = false;
    Link.println("+WIFI5G:DISCONNECTED");
  }

  delay(5);
}
