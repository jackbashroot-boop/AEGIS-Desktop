// AEGIS PHOENIX - AES-256-GCM forensic vault (native, on-device)
// -------------------------------------------------------------------
// Enterprise-standard authenticated encryption for everything the suite
// persists: forensic log entries are encrypted BEFORE they touch the SD
// card, and decrypted on read/export. The ESP32-S3 hardware AES/SHA
// accelerators are used via mbedTLS (MBEDTLS hardware drivers are enabled
// by default in the Arduino-ESP32 core).
//
// Wire format (4 fields, colon-separated, no ':' inside base64):
//   AES256GCMv1:<b64 salt[16]>:<b64 nonce[12]>:<b64 ciphertext||tag[16]>
//
// Key: PBKDF2-HMAC-SHA256(passphrase, salt, 60000 iterations, 32 bytes),
// derived once per salt and cached - per-message cost is one GCM pass.
// A fresh random salt is generated on first use and kept in RAM only;
// changing the passphrase or rebooting re-derives from the envelope salt,
// so envelopes stay decryptable with the correct passphrase alone.

#pragma once
#include <Arduino.h>
#include <mbedtls/gcm.h>
#include <mbedtls/md.h>
#include <mbedtls/base64.h>
#include <esp_random.h>
#include <cstring>

class AesVault {
 public:
  bool ready = false;
  String lastError = "";

  // Derive (or re-derive) the cached key for `passphrase` + `salt`.
  bool beginWithSalt(const String& passphrase, const uint8_t salt[16]) {
    if (passphrase.length() < 8) { lastError = "passphrase too short"; ready = false; return false; }
    pass_ = passphrase;
    memcpy(salt_, salt, 16);
    if (!derive(pass_.c_str(), salt_, key_)) { lastError = "kdf failed"; ready = false; return false; }
    ready = true;
    return true;
  }

  // First-use helper: random salt, derive, cache.
  bool begin(const String& passphrase) {
    uint8_t salt[16];
    esp_fill_random(salt, sizeof(salt));
    return beginWithSalt(passphrase, salt);
  }

  // Encrypt one log line. Returns "" on failure (caller must NOT fall
  // back to plaintext silently - log an error marker instead).
  String encryptLine(const String& plain) {
    if (!ready) { lastError = "vault not initialised"; return ""; }
    uint8_t nonce[12];
    esp_fill_random(nonce, sizeof(nonce));
    std::string ct(plain.length() + 16, '\0');
    mbedtls_gcm_context gcm;
    mbedtls_gcm_init(&gcm);
    String out;
    if (mbedtls_gcm_setkey(&gcm, MBEDTLS_CIPHER_ID_AES, key_, 256) == 0 &&
        mbedtls_gcm_crypt_and_tag(&gcm, MBEDTLS_GCM_ENCRYPT, plain.length(),
                                  nonce, sizeof(nonce), nullptr, 0,
                                  (const uint8_t*)plain.c_str(),
                                  (uint8_t*)ct.data(),
                                  16, (uint8_t*)ct.data() + plain.length()) == 0) {
      out = "AES256GCMv1:" + b64(salt_, 16) + ":" + b64(nonce, sizeof(nonce)) + ":" +
            b64((const uint8_t*)ct.data(), ct.size());
    } else {
      lastError = "gcm encrypt failed";
    }
    mbedtls_gcm_free(&gcm);
    return out;
  }

  // Decrypt an envelope produced by encryptLine(). Verifies the GCM tag;
  // any tampering, truncation, or wrong passphrase returns false.
  bool decryptLine(const String& env, String& plainOut) {
    int p1 = env.indexOf(':');
    int p2 = p1 < 0 ? -1 : env.indexOf(':', p1 + 1);
    int p3 = p2 < 0 ? -1 : env.indexOf(':', p2 + 1);
    if (p1 < 0 || p2 < 0 || p3 < 0 || env.substring(0, p1) != "AES256GCMv1") {
      lastError = "bad envelope"; return false;
    }
    uint8_t salt[16], nonce[12];
    std::string ct;
    if (!unb64(env.substring(p1 + 1, p2), salt, sizeof(salt)) ||
        !unb64(env.substring(p2 + 1, p3), nonce, sizeof(nonce)) ||
        !unb64str(env.substring(p3 + 1), ct) || ct.size() < 16) {
      lastError = "bad base64/length"; return false;
    }
    // Re-derive if this envelope's salt differs from the cached key's.
    if (!ready || memcmp(salt, salt_, 16) != 0) {
      if (pass_.isEmpty() || !derive(pass_.c_str(), salt, key_)) { lastError = "kdf failed"; return false; }
      memcpy(salt_, salt, 16);
      ready = true;
    }
    std::string pt(ct.size() - 16, '\0');
    mbedtls_gcm_context gcm;
    mbedtls_gcm_init(&gcm);
    int rc = mbedtls_gcm_setkey(&gcm, MBEDTLS_CIPHER_ID_AES, key_, 256);
    if (rc == 0) {
      rc = mbedtls_gcm_auth_decrypt(&gcm, pt.size(), nonce, sizeof(nonce), nullptr, 0,
                                    (const uint8_t*)ct.data() + pt.size(), 16,
                                    (const uint8_t*)ct.data(), (uint8_t*)pt.data());
    }
    mbedtls_gcm_free(&gcm);
    if (rc != 0) { lastError = "gcm auth failed (tampered or wrong key)"; return false; }
    plainOut = String(pt.c_str());
    return true;
  }

  // Remember the passphrase so decryptLine() can re-derive for other salts.
  void setPassphrase(const String& passphrase) { pass_ = passphrase; }

 private:
  String pass_ = "";
  uint8_t key_[32] = {0};
  uint8_t salt_[16] = {0};

  static bool derive(const char* pass, const uint8_t salt[16], uint8_t out[32]) {
    const mbedtls_md_info_t* md = mbedtls_md_info_from_type(MBEDTLS_MD_SHA256);
    if (!md) return false;
    const uint32_t iterations = 60000;
    uint8_t u[32], t[32];
    // U1 = HMAC(pass, salt || INT32_BE(1))
    uint8_t block[20];
    memcpy(block, salt, 16);
    block[16] = 0; block[17] = 0; block[18] = 0; block[19] = 1;
    if (mbedtls_md_hmac(md, (const uint8_t*)pass, strlen(pass), block, sizeof(block), u) != 0) return false;
    memcpy(t, u, 32);
    for (uint32_t i = 1; i < iterations; i++) {
      if (mbedtls_md_hmac(md, (const uint8_t*)pass, strlen(pass), u, 32, u) != 0) return false;
      for (int j = 0; j < 32; j++) t[j] ^= u[j];
    }
    memcpy(out, t, 32);
    return true;
  }

  static String b64(const uint8_t* data, size_t len) {
    size_t olen = 0;
    mbedtls_base64_encode(nullptr, 0, &olen, data, len);
    std::string s(olen, '\0');
    if (mbedtls_base64_encode((uint8_t*)s.data(), olen, &olen, data, len) != 0) return "";
    s.resize(olen);
    return String(s.c_str());
  }

  static bool unb64(const String& in, uint8_t* out, size_t outLen) {
    size_t olen = 0;
    if (mbedtls_base64_decode(out, outLen, &olen,
                              (const uint8_t*)in.c_str(), in.length()) != 0) return false;
    return olen == outLen;
  }

  static bool unb64str(const String& in, std::string& out) {
    size_t olen = 0;
    mbedtls_base64_decode(nullptr, 0, &olen, (const uint8_t*)in.c_str(), in.length());
    if (olen == 0) return false;
    out.assign(olen, '\0');
    return mbedtls_base64_decode((uint8_t*)out.data(), olen, &olen,
                                 (const uint8_t*)in.c_str(), in.length()) == 0;
  }
};
