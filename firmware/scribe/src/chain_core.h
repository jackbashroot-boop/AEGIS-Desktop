#pragma once
// Pure FNV-1a hash-chain core shared by AEGIS log producers/verifiers.
//
// This header is the ONE implementation of the chain math. SCRIBE's
// firmware uses it on-device (verifyChain in main.cpp is a thin SD
// wrapper), and firmware-host-tests/fuzz_chain.cpp fuzzes the exact
// same code on a desktop compiler with ASan/UBSan - so what gets
// fuzzed is what runs on the Cardputer, not a copy.
//
// Chain format (all families): a record line is a JSON object whose
// hashed body is the line with the trailing ,"chain":"xxxxxxxx"}
// removed and the closing brace restored; the chain value is
// FNV-1a-32 (offset 0x811c9dc5) continued from the previous line's
// chain, rendered as 8 lowercase hex digits. A line with no chain
// field resets the chain to the offset basis (legacy unchained logs).

#include <cstdint>
#include <cstdio>
#include <string>
#include <vector>

namespace aegis_chain {

static const uint32_t kBasis = 0x811c9dc5u;

static inline uint32_t fnv1a(uint32_t h, const char* data, size_t len) {
  for (size_t i = 0; i < len; i++) {
    h ^= (uint8_t)data[i];
    h *= 0x01000193u;
  }
  return h;
}

static inline uint32_t fnv1a(uint32_t h, const std::string& s) {
  return fnv1a(h, s.data(), s.size());
}

// Advance the chain over one record body (the producer side).
static inline uint32_t chainAdvance(uint32_t h, const std::string& bodyJson) {
  return fnv1a(h, bodyJson);
}

static inline std::string fnvHex(uint32_t h) {
  char b[9];
  snprintf(b, sizeof(b), "%08x", h);
  return std::string(b);
}

// Arduino String.trim(): strips leading/trailing whitespace.
static inline std::string ardTrim(const std::string& s) {
  size_t b = 0, e = s.size();
  while (b < e && (s[b] == ' ' || s[b] == '\t' || s[b] == '\n' || s[b] == '\v' || s[b] == '\f' || s[b] == '\r')) b++;
  while (e > b && (s[e - 1] == ' ' || s[e - 1] == '\t' || s[e - 1] == '\n' || s[e - 1] == '\v' || s[e - 1] == '\f' || s[e - 1] == '\r')) e--;
  return s.substr(b, e - b);
}

// Arduino String.substring(from, to): swaps the indices when from > to
// and clamps both to the length. The firmware computes the end as
// length()-2 in 32-bit unsigned arithmetic, which wraps for very
// short lines; keeping the indices uint32_t reproduces that exactly.
static inline std::string ardSub(const std::string& s, uint32_t from, uint32_t to) {
  if (from > to) {
    uint32_t t = from;
    from = to;
    to = t;
  }
  const uint32_t len = (uint32_t)s.size();
  if (from >= len) return std::string();
  if (to > len) to = len;
  return s.substr(from, to - from);
}

struct ChainStatCore {
  int lines = 0;
  int chained = 0;
  int bad = 0;
  int unchained = 0;
  uint32_t head = kBasis;  // chain value after the last chained line
};

// Verify a whole file's lines (raw lines, with or without '\n').
// Exactly the loop SCRIBE's verifyChain ran inline before this header
// existed - semantics preserved bit for bit, including the chain
// reset on unchained lines.
static inline ChainStatCore verifyChainLines(const std::vector<std::string>& rawLines) {
  ChainStatCore st;
  uint32_t h = kBasis;
  for (const std::string& rawLine : rawLines) {
    const std::string line = ardTrim(rawLine);
    if (line.empty()) continue;
    st.lines++;
    const size_t pos = line.rfind(",\"chain\":\"");
    if (pos == std::string::npos) {
      st.unchained++;
      h = kBasis;
      continue;
    }
    const std::string want = ardSub(line, (uint32_t)(pos + 10), (uint32_t)((uint32_t)line.size() - 2u));
    std::string body = ardSub(line, 0, (uint32_t)pos);
    body += "}";
    h = fnv1a(h, body);
    st.chained++;
    if (fnvHex(h) != want) st.bad++;
  }
  st.head = h;
  return st;
}

}  // namespace aegis_chain
