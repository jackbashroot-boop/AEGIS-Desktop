// PHOENIX rule language — on-device evaluator.
// Same grammar as src/lib/rules.ts on the console so a bundle behaves identically in both places:
//   wifi.mgmt == 0xC0 && count(window=1s) > 40
//   wifi.ssid in corp_list && wifi.bssid notin known_bssids && rssi_delta > 6
#pragma once
#include <Arduino.h>
#include <ArduinoJson.h>
#include <vector>
#include <map>

struct Tok { enum T { NUM, STR, ID, OP, LP, RP, LB, RB, COMMA, END } t; String s; double n = 0; };

static std::vector<Tok> ruleTokenize(const String& in) {
  std::vector<Tok> out; size_t i = 0;
  while (i < in.length() && out.size() < 96) {
    char c = in[i];
    if (isspace(c)) { i++; continue; }
    if (c == '(') { out.push_back({Tok::LP, "", 0}); i++; continue; }
    if (c == ')') { out.push_back({Tok::RP, "", 0}); i++; continue; }
    if (c == '[') { out.push_back({Tok::LB, "", 0}); i++; continue; }
    if (c == ']') { out.push_back({Tok::RB, "", 0}); i++; continue; }
    if (c == ',') { out.push_back({Tok::COMMA, "", 0}); i++; continue; }
    if (c == '"' || c == '\'') { size_t j = i + 1; String v; while (j < in.length() && in[j] != c) v += in[j++]; out.push_back({Tok::STR, v}); i = j + 1; continue; }
    String two = in.substring(i, i + 2);
    if (two == "==" || two == "!=" || two == ">=" || two == "<=" || two == "&&" || two == "||") { out.push_back({Tok::OP, two}); i += 2; continue; }
    if (c == '>' || c == '<' || c == '!' || c == '=') { out.push_back({Tok::OP, c == '=' ? String("==") : String(c)}); i++; continue; }
    if (isdigit(c)) {
      size_t j = i; bool hex = in.substring(i, i + 2) == "0x";
      if (hex) { j += 2; while (j < in.length() && isxdigit(in[j])) j++; out.push_back({Tok::NUM, "", (double)strtoul(in.substring(i + 2, j).c_str(), nullptr, 16)}); }
      else { while (j < in.length() && (isdigit(in[j]) || in[j] == '.')) j++; out.push_back({Tok::NUM, "", atof(in.substring(i, j).c_str())}); }
      i = j; continue;
    }
    if (isalpha(c) || c == '_') {
      size_t j = i; while (j < in.length() && (isalnum(in[j]) || in[j] == '_' || in[j] == '.' || in[j] == '-')) j++;
      String w = in.substring(i, j);
      if (w == "in" || w == "notin" || w == "overlaps") out.push_back({Tok::OP, w});
      else if (w == "and") out.push_back({Tok::OP, "&&"}); else if (w == "or") out.push_back({Tok::OP, "||"}); else if (w == "not") out.push_back({Tok::OP, "!"});
      else out.push_back({Tok::ID, w});
      i = j; continue;
    }
    i++;
  }
  out.push_back({Tok::END, "", 0});
  return out;
}

// A value in the evaluator: number, string, bool, list-of-strings/numbers, or undefined.
struct Val { enum K { UNDEF, NUM, STR, BOOL, LIST } k = UNDEF; double n = 0; String s; bool b = false; std::vector<Val> list;
  static Val num(double v) { Val x; x.k = NUM; x.n = v; return x; }
  static Val str(const String& v) { Val x; x.k = STR; x.s = v; return x; }
  static Val boolean(bool v) { Val x; x.k = BOOL; x.b = v; return x; }
  bool truthy() const { switch (k) { case NUM: return n != 0; case STR: return s.length(); case BOOL: return b; case LIST: return !list.empty(); default: return false; } }
  bool eq(const Val& o) const { if (k == NUM && o.k == NUM) return n == o.n; if (k == STR && o.k == STR) return s == o.s; if (k == BOOL && o.k == BOOL) return b == o.b; if (k == NUM && o.k == STR) return n == atof(o.s.c_str()); if (k == STR && o.k == NUM) return atof(s.c_str()) == o.n; return false; }
};

struct Ctx { JsonObject fields; const std::vector<String>* corpList; const std::vector<String>* knownList; };

class Rule {
 public:
  String code, kind, severity, action, expression;
  std::vector<Tok> toks;
  static bool parses(const String& expr) { Rule r; r.expression = expr; r.toks = ruleTokenize(expr); size_t p = 0; r.parseFailed = false; r.parseOr(p, 0); return !r.parseFailed && p < r.toks.size() && r.toks[p].t == Tok::END; }
  bool evaluate(const Ctx& c) { pos = 0; ctx = &c; parseFailed = false; return parseOr(pos, 0).truthy(); }

 private:
  size_t pos = 0; const Ctx* ctx = nullptr; bool parseFailed = false;
  Val lookup(const String& path, const String& args) {
    if (!ctx) return Val();
    String full = args.length() ? path + "(" + args + ")" : path;
    if (ctx->fields[full].is<JsonVariant>() && !ctx->fields[full].isNull()) return fromJson(ctx->fields[full]);
    if (ctx->fields[path].is<JsonVariant>() && !ctx->fields[path].isNull()) return fromJson(ctx->fields[path]);
    if (args.length()) {                                   // whitespace-insensitive: unique(dst_ips, 10s)
      for (JsonPair kv : ctx->fields) { String k = kv.key().c_str(); k.replace(" ", ""); if (k == full) return fromJson(kv.value()); }
    }
    if (path == "corp_list" && ctx->corpList) { Val v; v.k = Val::LIST; for (auto& s : *ctx->corpList) v.list.push_back(Val::str(s)); return v; }
    if (path == "known_bssids" && ctx->knownList) { Val v; v.k = Val::LIST; for (auto& s : *ctx->knownList) v.list.push_back(Val::str(s)); return v; }
    if (path == "corp_subnets" || path == "trust_store") { Val v; v.k = Val::LIST; return v; }
    return Val();
  }
  static Val fromJson(JsonVariantConst v) {
    if (v.is<bool>()) return Val::boolean(v.as<bool>());
    if (v.is<double>() || v.is<int>()) return Val::num(v.as<double>());
    if (v.is<const char*>()) return Val::str(v.as<const char*>());
    return Val();
  }
  bool isOp(size_t p, const char* op) { return p < toks.size() && toks[p].t == Tok::OP && toks[p].s == op; }
  Val parseOr(size_t& p, int d) { Val l = parseAnd(p, d + 1); while (isOp(p, "||") && d < 16) { p++; Val r = parseAnd(p, d + 1); l = Val::boolean(l.truthy() || r.truthy()); } return l; }
  Val parseAnd(size_t& p, int d) { Val l = parseCmp(p, d + 1); while (isOp(p, "&&") && d < 16) { p++; Val r = parseCmp(p, d + 1); l = Val::boolean(l.truthy() && r.truthy()); } return l; }
  Val parseCmp(size_t& p, int d) {
    Val l = parseUnary(p, d + 1);
    if (p < toks.size() && toks[p].t == Tok::OP) {
      String op = toks[p].s;
      if (op == "==" || op == "!=" || op == ">" || op == "<" || op == ">=" || op == "<=" || op == "in" || op == "notin" || op == "overlaps") {
        p++; Tok rt = toks[p]; Val r = parseUnary(p, d + 1);
        if (r.k == Val::UNDEF && rt.t == Tok::ID && rt.s.indexOf('.') < 0) r = Val::str(rt.s);   // bare enum word on the right: dns.qtype == TXT
        return Val::boolean(compare(op, l, r));
      }
    }
    return l;
  }
  Val parseUnary(size_t& p, int d) { if (isOp(p, "!")) { p++; return Val::boolean(!parseUnary(p, d + 1).truthy()); } return parsePrimary(p, d + 1); }
  Val parsePrimary(size_t& p, int d) {
    if (p >= toks.size() || d > 20) { parseFailed = true; return Val(); }
    Tok t = toks[p++];
    if (t.t == Tok::END || t.t == Tok::OP) { parseFailed = true; return Val(); }  // operand expected here
    if (t.t == Tok::NUM) return Val::num(t.n);
    if (t.t == Tok::STR) return Val::str(t.s);
    if (t.t == Tok::LP) { Val v = parseOr(p, d + 1); if (p < toks.size() && toks[p].t == Tok::RP) p++; return v; }
    if (t.t == Tok::LB) { Val v; v.k = Val::LIST; int guard = 0; while (p < toks.size() && toks[p].t != Tok::RB && toks[p].t != Tok::END && guard++ < 32) { v.list.push_back(parseOr(p, d + 1)); if (p < toks.size() && toks[p].t == Tok::COMMA) p++; } if (p < toks.size() && toks[p].t == Tok::RB) p++; return v; }
    if (t.t == Tok::ID) {
      if (t.s == "true") return Val::boolean(true);
      if (t.s == "false") return Val::boolean(false);
      String args;
      if (p < toks.size() && toks[p].t == Tok::LP) { p++; int guard = 0; while (p < toks.size() && toks[p].t != Tok::RP && toks[p].t != Tok::END && guard++ < 16) { Tok a = toks[p++]; args += a.t == Tok::NUM ? String((long)a.n) : a.t == Tok::OP && a.s == "==" ? String("=") : a.t == Tok::COMMA ? String(",") : a.s; } if (p < toks.size() && toks[p].t == Tok::RP) p++; }
      return lookup(t.s, args);
    }
    return Val();
  }
  static bool compare(const String& op, const Val& a, const Val& b) {
    if (op == "in" || op == "notin" || op == "overlaps") {
      if (a.k == Val::UNDEF) return false;
      bool hit = false;
      if (b.k == Val::LIST) for (auto& item : b.list) { if (a.k == Val::LIST) { for (auto& x : a.list) if (x.eq(item)) hit = true; } else if (a.eq(item)) hit = true; }
      else { hit = a.eq(b); }
      return op == "notin" ? !hit : hit;
    }
    if (a.k == Val::UNDEF || b.k == Val::UNDEF) return false;
    if (op == "==") return a.eq(b);
    if (op == "!=") return !a.eq(b);
    double x = a.k == Val::NUM ? a.n : atof(a.s.c_str()), y = b.k == Val::NUM ? b.n : atof(b.s.c_str());
    if (op == ">") return x > y;
    if (op == "<") return x < y;
    if (op == ">=") return x >= y;
    if (op == "<=") return x <= y;
    return false;
  }
};

class RuleSet {
 public:
  void clear() { rules.clear(); }
  size_t size() const { return rules.size(); }
  bool hasKind(const String& kind) const { for (auto& r : rules) if (r.kind == kind) return true; return false; }
  void loadRules(JsonArrayConst arr) {
    for (JsonObjectConst r : arr) {
      Rule rule; rule.code = r["code"].as<String>(); rule.kind = r["kind"].as<String>(); rule.severity = r["severity"].as<String>();
      rule.action = r["action"].as<String>(); rule.expression = r["expression"].as<String>(); rule.toks = ruleTokenize(rule.expression);
      bool replaced = false; for (auto& e : rules) if (e.code == rule.code) { e = rule; replaced = true; }
      if (!replaced && rules.size() < 128) rules.push_back(rule);
    }
  }
  void loadFromBundle(JsonDocument& bundle) { loadRules(bundle["rules"].as<JsonArrayConst>()); }
  /** SD rule-pack loader: only rules with code+kind and an expression that
      parses are armed. Returns rules accepted; bumps `rejected` otherwise. */
  int loadRulesValidated(JsonArrayConst arr, int& rejected) {
    int added = 0;
    for (JsonObjectConst r : arr) {
      Rule rule; rule.code = r["code"].as<String>(); rule.kind = r["kind"].as<String>(); rule.severity = r["severity"].as<String>();
      rule.action = r["action"].as<String>(); rule.expression = r["expression"].as<String>();
      if (!rule.code.length() || !rule.kind.length() || !Rule::parses(rule.expression)) { rejected++; continue; }
      rule.toks = ruleTokenize(rule.expression);
      bool replaced = false; for (auto& e : rules) if (e.code == rule.code) { e = rule; replaced = true; }
      if (!replaced && rules.size() < 128) rules.push_back(rule);
      added++;
    }
    return added;
  }
  /** True when any rule for this kind fires on the capture (adds kind to fields so `kind == "..."` works). */
  bool evaluate(const String& kind, JsonObject fields, const std::vector<String>& corpList, const std::vector<String>& knownList) {
    fields["kind"] = kind;
    Ctx c{ fields, &corpList, &knownList };
    for (auto& r : rules) if (r.kind == kind && !isDisabled(r.code) && r.evaluate(c)) return true;
    return false;
  }
  // On-device manager support: list + persistent enable/disable by code.
  const std::vector<Rule>& all() const { return rules; }
  std::vector<String> disabled;
  bool isDisabled(const String& code) const {
    for (auto& c : disabled) if (c == code) return true;
    return false;
  }
  void setDisabled(const String& code, bool off) {
    for (size_t i = 0; i < disabled.size(); i++)
      if (disabled[i] == code) { if (!off) disabled.erase(disabled.begin() + i); return; }
    if (off) disabled.push_back(code);
  }

 private:
  std::vector<Rule> rules;
};
