# Dependency lock — AEGIS PHOENIX (from platformio.ini, env cardputer-adv)
platform      espressif32 @ ^6.6.0        board  m5stack-stamps3 (ESP32-S3)
framework     arduino (bundled with the platform above)
partitions    default_8MB.csv (bundled with the platform)
libraries     m5stack/M5Cardputer @ ^1.0.3
              bblanchon/ArduinoJson @ ^7.0.4
              h2zero/NimBLE-Arduino @ ^2.1.0
              jgromes/RadioLib @ ^7.0.0
companion     arduino-cli + esp32:esp32 core @ 3.3.12 (aegis-c5-companion/)
Transitive libraries (e.g. M5Unified/M5GFX inside M5Cardputer) are resolved by
PlatformIO/arduino-cli at fetch time and recorded in offline/resolved-versions.txt.
