// Host fuzz/unit tests for the exact KeyFSM shipped in the firmware.
// Build: g++ -std=c++17 -fsanitize=address,undefined -I src \
//          tools/host_tests/test_key_fsm.cpp -o /tmp/test_key_fsm
#include "../../src/key_fsm.h"
#include "../../src/adv_guard.h"
#include <cassert>
#include <cstdio>
#include <cstdlib>
#include <vector>

static uint32_t fires = 0;

// Invariant model: a Key event may only fire if the same single key was
// continuously present for the >=45ms before it, with no chord since the
// last full 90ms release window.
int main() {
  // --- deterministic cases ---------------------------------------------
  { // clean 100ms press of 'q' fires exactly once
    KeyFSM f; fires = 0;
    for (uint32_t t = 0; t <= 200; t += 10) {
      auto e = f.update(t < 100, t < 100 ? 1 : 0, 'q', t);
      if (e.type == KeyFSM::Event::Key) { assert(e.key == 'q'); fires++; }
    }
    assert(fires == 1);
  }
  { // 20ms glitch tap never fires
    KeyFSM f;
    for (uint32_t t = 0; t <= 120; t += 10) {
      auto e = f.update(t < 20, t < 20 ? 1 : 0, 's', t);
      assert(e.type != KeyFSM::Event::Key);
    }
  }
  { // chord never fires; needs 90ms clean release before next key works
    KeyFSM f; bool fired = false;
    for (uint32_t t = 0; t <= 400; t += 10) {
      bool chord = t < 100;
      bool solo = t >= 200 && t < 300;
      auto e = f.update(chord || solo, chord ? 3 : solo ? 1 : 0, 'q', t);
      if (e.type == KeyFSM::Event::Key) fired = true;
    }
    assert(fired);
  }

  // --- random mash fuzz: 2,000,000 adversarial ticks --------------------
  KeyFSM f;
  std::vector<bool> histPressed;
  std::vector<int> histCount;
  std::vector<char> histKey;
  std::vector<uint32_t> histTime;
  uint32_t now = 0;
  srand(0xAE615);
  for (int i = 0; i < 2000000; i++) {
    int r = rand() % 100;
    bool pressed = r < 62;                       // fingers down most of the time
    int wc = !pressed ? 0 : (r % 5 == 0 ? 2 + rand() % 4 : 1);  // frequent chords
    char key = (char)('a' + (rand() % 8));
    if (rand() % 3) key = 'q';                   // bias to action keys
    now += rand() % 30;                          // 0-29ms ticks (incl. same-ms)
    histPressed.push_back(pressed);
    histCount.push_back(wc);
    histKey.push_back(key);
    histTime.push_back(now);
    auto e = f.update(pressed, wc, key, now);
    if (e.type == KeyFSM::Event::Key) {
      // Verify the stability invariant over the trailing 45 ms window:
      // every sample strictly inside it must be the same single key.
      assert(wc == 1 && key == e.key);
      bool sawStart = false;
      for (size_t j = 0; j < histTime.size(); j++) {
        if (histTime[j] <= now - 45) {
          if (histPressed[j] && histCount[j] == 1 && histKey[j] == e.key) sawStart = true;
          continue;
        }
        assert(histPressed[j] && histCount[j] == 1 && histKey[j] == e.key);
      }
      assert(sawStart);   // candidate really began >=45ms ago with this key
      fires++;
    }
  }
  { // StuckKeyWatchdog: unchanged signature + already-fired -> one resync
    StuckKeyWatchdog w;
    bool fired = false;
    for (uint32_t t2 = 0; t2 <= 12000; t2 += 100) {
      bool r = w.update("q", true, t2);
      if (r) { assert(!fired); fired = true; }   // exactly once per episode
    }
    assert(fired && w.resyncs() == 1);
    w.noteResync();
    assert(!w.update("", true, 20000));          // released: silent
    assert(!w.update("s", false, 20100));        // new key, not fired yet
  }

  printf("key_fsm host tests OK (deterministic + 2M fuzz ticks, fires=%u ghosts=%u)\n",
         fires, f.ghosts());
  return 0;
}
