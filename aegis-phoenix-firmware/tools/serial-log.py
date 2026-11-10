#!/usr/bin/env python3
"""Aegis serial logger: reconnects automatically across ESP32 USB resets."""
import sys
import time

try:
    import serial
except ImportError:
    sys.exit("pyserial missing: python -m pip install --user pyserial")

BAUD = int(sys.argv[2]) if len(sys.argv) > 2 else 115200

def find_port():
    if len(sys.argv) > 1:
        return sys.argv[1]
    import glob
    hits = sorted(glob.glob("/dev/ttyACM*") + glob.glob("/dev/ttyUSB*"))
    return hits[0] if hits else "/dev/ttyACM0"

print(f"[logger] auto-watching for a ttyACM/ttyUSB port @ {BAUD} - Ctrl+C to stop", flush=True)
while True:
    try:
        with serial.Serial(find_port(), BAUD, timeout=1) as s:
            print("[connected]", flush=True)
            while True:
                data = s.readline()
                if data:
                    sys.stdout.write(data.decode(errors="replace"))
                    sys.stdout.flush()
    except Exception as e:
        print(f"[waiting: {e}]", flush=True)
        time.sleep(1)
