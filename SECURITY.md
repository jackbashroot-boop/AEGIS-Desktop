# Security & responsible use

AEGIS is a defensive, owner-operated toolkit: passive scanning,
wardriving logs of your own movements, home-network watching, and
tamper-evident notes. It contains no deauthentication, jamming,
frame injection, credential attacks, or exploitation features, and
contributions adding them will be rejected.

## Trust model
- Devices and the Companion authenticate with HMAC-SHA256
  challenge-response using a per-install token generated on the PC;
  the token never crosses the wire and never lives in this repo.
- A device asks its human (on its own screen) before first linking
  to a PC host; approval is per host+port and revocable by denying.
- Synced files are SHA-256 verified end-to-end; device logs carry a
  FNV-1a hash chain the Companion re-verifies on filing.
- Map packs are HMAC-signed; foreign/tampered packs are refused.

## Reporting a vulnerability
Open a private security advisory on this repository (Security tab)
rather than a public issue. Please include affected component +
version and reproduction steps.
