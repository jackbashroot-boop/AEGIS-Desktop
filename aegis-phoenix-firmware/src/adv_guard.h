// Cardputer ADV Guard - pure-logic workarounds for ADV hardware quirks.
// No Arduino dependencies: this exact code is host-tested under
// ASan/UBSan (tools/host_tests/test_key_fsm.cpp).
//
// Quirk covered: the TCA8418 keyboard FIFO holds only 10 events and the
// M5 reader drains 1/update, so sustained key mashing loses release
// events and strands a phantom key as "pressed" forever. This watchdog
// detects an unchanged pressed-signature that outlives its action and
// tells the firmware to reinitialise the keyboard reader.
#pragma once
#include <cstdint>
#include <cstring>

class StuckKeyWatchdog {
 public:
  // Call every loop with the current pressed-signature ("" when no key),
  // whether the key FSM has already fired for this press, and the clock.
  // Returns true exactly once per stuck episode: caller should resync
  // the keyboard reader, then call noteResync().
  bool update(const char* signature, bool actionAlreadyFired, uint32_t nowMs) {
    if (!signature || !signature[0]) {
      last_[0] = 0;
      fired_ = false;
      return false;
    }
    if (strcmp(signature, last_) != 0) {
      strncpy(last_, signature, sizeof(last_) - 1);
      last_[sizeof(last_) - 1] = 0;
      since_ = nowMs;
      fired_ = false;
      return false;
    }
    if (actionAlreadyFired && !fired_ && nowMs - since_ > 5000) {
      fired_ = true;
      resyncs_++;
      return true;
    }
    return false;
  }

  void noteResync() { last_[0] = 0; fired_ = false; }
  uint32_t resyncs() const { return resyncs_; }

 private:
  char last_[40] = {0};
  uint32_t since_ = 0;
  uint32_t resyncs_ = 0;
  bool fired_ = false;
};

// Loop-stall detector: reports a stall when the gap between calls
// exceeds a threshold the caller does not expect (long scans should
// call expectStall() first so they are not misreported).
class LoopStallWatchdog {
 public:
  void tick(uint32_t nowMs) {
    if (last_ && nowMs - last_ > worst_) worst_ = nowMs - last_;
    last_ = nowMs;
  }
  void expectStall() { last_ = 0; }
  uint32_t worstGapMs() const { return worst_; }

 private:
  uint32_t last_ = 0, worst_ = 0;
};
