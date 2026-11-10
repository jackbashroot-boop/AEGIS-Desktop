// AEGIS PHOENIX - single source of truth for Cardputer ADV wiring.
// Verified against M5Stack docs (Cardputer-Adv, Cap LoRa-1262) and
// cross-checked with two independent open-source ADV+Cap firmwares.
#pragma once

// Shared SPI bus (FSPI): microSD + SX1262, separate chip-selects.
#define AEGIS_SPI_SCK   40
#define AEGIS_SPI_MISO  39
#define AEGIS_SPI_MOSI  14
#define AEGIS_SD_CS     12

// Cap LoRa-1262 - Semtech SX1262.
#define AEGIS_LORA_NSS   5
#define AEGIS_LORA_RST   3
#define AEGIS_LORA_BUSY  6
#define AEGIS_LORA_DIO1  4
#define AEGIS_LORA_FREQ_MHZ 915.0f   // US915 (device locale). EU868 users: 868.0
#define AEGIS_LORA_BW_KHZ   125.0f
#define AEGIS_LORA_SF         9
#define AEGIS_LORA_CR         7      // 4/7
#define AEGIS_LORA_SYNC       0x12   // private networks
#define AEGIS_LORA_PWR_DBM    22     // v0.12.9: SX1262 max (+22 dBm). Battery math is kind: TX only fires for a
                                     // ~0.3 s beacon every 10 min (~120 mA peaks) - about 0.6 mAh/day.
                                     // Listening (the dominant draw) is unchanged. Legal on US915; check your region.
#define AEGIS_LORA_TCXO_V     1.8f

// Cap LoRa-1262 - ATGM336H-6N GNSS, UART1 @ 115200 8N1.
// M5 pin table maps G13<->GPS-RX (ESP transmits) and G15<->GPS-TX (ESP
// receives); a hardware-verified community firmware agrees (TX=13/RX=15),
// while at least one other project has them swapped - if you get zero
// NMEA sentences with a known-good Cap, swap these two numbers.
#define AEGIS_GPS_RX_PIN 15  // ESP RX <- GPS TX
#define AEGIS_GPS_TX_PIN 13  // ESP TX -> GPS RX
#define AEGIS_GPS_BAUD   115200

// ESP32-C5 hot-swap companion on the Grove port (Option A). The C5 is
// powered separately (its own USB-C is power-only here); this UART is
// 3.3 V logic: ADV RX=G1 <- C5 TX, ADV TX=G2 -> C5 RX, GND common.
#define AEGIS_C5_RX_PIN   1
#define AEGIS_C5_TX_PIN   2
#define AEGIS_C5_BAUD     115200

// Cardputer ADV onboard.
#define AEGIS_IR_TX_PIN  44  // built-in infrared emitter
#define AEGIS_IR_RX_PIN   1  // OPTIONAL external 38 kHz receiver on Grove G1;
// NOTE: G1 is shared with the C5 link. IR learning is available when C5
// hot-swap is disabled in SETUP; the C5 takes priority when enabled.
#define AEGIS_I2C_SDA     8
#define AEGIS_I2C_SCL     9
