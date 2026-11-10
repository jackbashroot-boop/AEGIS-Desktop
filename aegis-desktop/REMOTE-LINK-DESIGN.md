# AEGIS Remote Link — design (v1.2.18, design-only)

Goal: a Cardputer in the field (on a phone hotspot) reaches the home
Companion over the internet, with the same safety rules as the LAN
link: the device asks, the human on the device approves, updates need
a physical ENTER, and nothing about TALON changes (never linked).

## Recommended shape: Tailscale on the phone + home PC

1. Phone runs Tailscale and shares its hotspot. The Cardputer joins
   the phone Wi-Fi like any saved network (SETUP > Home WiFi setup
   already stores multiple profiles' primary; a "field profile" slot
   is the only firmware addition needed).
2. The home PC also runs Tailscale. The Companion listener already
   binds all interfaces on port 8791, so it is reachable at the PC's
   Tailscale IP (100.x.y.z) or MagicDNS name with **no Companion code
   change** — AEGIS_LINK/desktop.txt just carries the Tailscale
   address instead of the LAN one.
3. Discovery over the internet: the current UDP broadcast discovery
   is LAN-only by design. Remote mode skips discovery: desktop.txt
   gives the exact host, the existing token proof (HMAC over nonce,
   90 s window) already authenticates both ends, and TLS is
   unnecessary inside the WireGuard tunnel Tailscale provides.
4. Data flow stays store-and-forward: the live link streams state,
   PHX sync uploads logs when the link is up, and the SD card remains
   the source of truth when it is not. Wardriving offline loses
   nothing; a field day syncs on return or opportunistically via the
   phone.

## Why not the alternatives

- **Public port-forward / DDNS**: exposes the listener to the whole
  internet. Rejected — AEGIS stays off the public net.
- **Companion-initiated MQTT/cloud broker**: adds a third party and
  an account dependency; the token model would need rework. Rejected
  for now; revisit only if Tailscale proves unreliable in the field.
- **Meshtastic/MeshCore as transport**: LoRa text meshes move bytes
  at ~kbps with duty-cycle limits — fine for a beacon or a "found
  something" flag (the GPX/GeoJSON/CSV share exports already cover
  data exchange), wrong tool for log sync. Kept as a signalling
  side-channel idea only.

## Budgets and safety

- Phone data: live frames are ~200 B every 2 s ≈ 0.35 MB/h; sync is
  delta-only (SHA-256 dedup already). A field day is a few MB.
- Battery: Wi-Fi via phone hotspot costs the same as home Wi-Fi;
  passive-offline still wins over everything and still blocks joins.
- Approvals unchanged: link prompt on device, ENTER for firmware,
  token proof both ways, reject diagnostics on the Dashboard.

## Build order when green-lit

1. Firmware: second saved Wi-Fi profile ("field") + desktop.txt host
   already free-form — mostly a SETUP label change. (½ day)
2. Companion: Settings hint text + a "remote (Tailscale)" example in
   the AEGIS_LINK writer; census/Live already address-agnostic. (½ day)
3. Field test: phone hotspot + Tailscale, verify discovery-less
   connect, sync-while-tethered, and the reject path with a wrong
   token. (one outing)
