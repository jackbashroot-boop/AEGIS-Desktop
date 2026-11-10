// PCAP evidence capture — promiscuous frames buffered in the sniffer
// callback (no SD I/O in interrupt context) and written from the main loop
// in classic libpcap format (LINKTYPE_IEEE802_11 = 105), readable by
// Wireshark/tshark. Off by default; toggle with `AEGIS PCAP ON|OFF`.
// Timestamps are boot-relative milliseconds (no trusted RTC on-device);
// the filename and STATUS output say so. Capture auto-stops at ~8 MB.
#pragma once

#include <SD.h>

struct PcapFrame {
  uint32_t tsMs = 0;
  uint16_t len = 0;
  uint8_t data[512];
};

static PcapFrame g_pcapRing[8];
static volatile uint8_t g_pcapHead = 0;   // written by the sniffer callback
static volatile uint8_t g_pcapTail = 0;   // drained by pcapService()
static bool g_pcapEnabled = false;
static uint32_t g_pcapFrames = 0;
static uint32_t g_pcapDropped = 0;
static const uint32_t kPcapMaxBytes = 8UL * 1024UL * 1024UL;

// Called from the promiscuous callback: copy only, never touch SD here.
static inline void pcapCapture(const uint8_t* frame, uint16_t len, uint32_t nowMs) {
  if (!g_pcapEnabled || !frame || len == 0) return;
  uint8_t next = (uint8_t)((g_pcapHead + 1) & 7);
  if (next == g_pcapTail) { g_pcapDropped++; return; }
  PcapFrame& slot = g_pcapRing[g_pcapHead];
  slot.tsMs = nowMs;
  slot.len = len > sizeof(slot.data) ? (uint16_t)sizeof(slot.data) : len;
  memcpy(slot.data, frame, slot.len);
  g_pcapHead = next;
}

static void pcapService() {
  static File pcapFile;
  static uint32_t written = 0;
  if (!g_pcapEnabled) {
    if (pcapFile) { pcapFile.close(); }
    while (g_pcapTail != g_pcapHead) g_pcapTail = (uint8_t)((g_pcapTail + 1) & 7);
    return;
  }
  if (!sdLogMgr.mounted) return;
  if (!pcapFile) {
    if (!SD.exists("/AEGIS/logs")) SD.mkdir("/AEGIS/logs");
    pcapFile = SD.open("/AEGIS/logs/capture.pcap", FILE_WRITE);
    if (!pcapFile) { g_pcapEnabled = false; return; }
    written = 24;
    auto w32 = [&](uint32_t v) { pcapFile.write((uint8_t*)&v, 4); };
    auto w16 = [&](uint16_t v) { pcapFile.write((uint8_t*)&v, 2); };
    w32(0xa1b2c3d4); w16(2); w16(4);          // magic, version 2.4
    w32(0); w32(0);                            // tz, sigfigs
    w32(65535); w32(105);                      // snaplen, IEEE 802.11
  }
  while (g_pcapTail != g_pcapHead) {
    const PcapFrame& fr = g_pcapRing[g_pcapTail];
    uint32_t hdr[4] = { fr.tsMs / 1000, (fr.tsMs % 1000) * 1000, fr.len, fr.len };
    pcapFile.write((uint8_t*)hdr, sizeof(hdr));
    pcapFile.write(fr.data, fr.len);
    written += sizeof(hdr) + fr.len;
    g_pcapFrames++;
    g_pcapTail = (uint8_t)((g_pcapTail + 1) & 7);
    if (written >= kPcapMaxBytes) {            // bounded evidence file
      pcapFile.close();
      g_pcapEnabled = false;
      break;
    }
  }
  pcapFile.flush();
}
