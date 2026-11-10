// AEGIS anti-ghost keyboard state machine - PURE logic, no Arduino deps.
// Host-testable: tools/host_tests/test_key_fsm.cpp fuzzes this exact code
// under ASan/UBSan. Rules:
//   * a key fires only after the SAME single key is stable >= 45 ms
//   * one action per physical press (HELD until release)
//   * any chord/ghost forces SETTLE: ~90 ms of full release is required
//     before a new candidate can start
#pragma once
#include <cstdint>

class KeyFSM {
 public:
  enum class State { Idle, Candidate, Held, Settle };
  struct Event {
    enum Type { None, Key, Ghost } type = None;
    char key = 0;
  };

  Event update(bool pressed, int wordCount, char onlyKey, uint32_t nowMs) {
    switch (state_) {
      case State::Idle:
        if (pressed && wordCount == 1) {
          cand_ = onlyKey; since_ = nowMs; state_ = State::Candidate;
        } else if (pressed && wordCount > 1) {
          ghosts_++; rel_ = nowMs; state_ = State::Settle;
          return {Event::Ghost, 0};
        }
        break;
      case State::Candidate:
        if (!pressed) {
          state_ = State::Idle;   // glitch tap: dropped silently
        } else if (wordCount != 1 || onlyKey != cand_) {
          ghosts_++; rel_ = nowMs; state_ = State::Settle;
          return {Event::Ghost, 0};
        } else if (nowMs - since_ >= 45) {
          state_ = State::Held;
          return {Event::Key, cand_};
        }
        break;
      case State::Held:
        if (!pressed) { rel_ = nowMs; state_ = State::Settle; }
        break;
      case State::Settle:
        if (pressed) rel_ = nowMs;
        else if (nowMs - rel_ > 90) state_ = State::Idle;
        break;
    }
    return {Event::None, 0};
  }

  void forceSettle(uint32_t nowMs) { rel_ = nowMs; state_ = State::Settle; }
  State state() const { return state_; }
  bool busy() const { return state_ == State::Held || state_ == State::Settle; }
  uint32_t ghosts() const { return ghosts_; }

 private:
  State state_ = State::Idle;
  char cand_ = 0;
  uint32_t since_ = 0, rel_ = 0, ghosts_ = 0;
};
