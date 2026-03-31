#!/usr/bin/env python3
import json
import os
import signal
import sys
import time
import tkinter as tk


STATE_PATH = "/tmp/llrdc-latency-probe.json"


class LatencyProbeApp:
    def __init__(self) -> None:
        self.root = tk.Tk()
        self.root.title("LLrdc Latency Probe")
        self.root.configure(background="black", cursor="none")
        self.root.attributes("-fullscreen", True)
        self.root.attributes("-topmost", True)
        self.root.focus_force()

        self.frame = tk.Frame(self.root, bg="black")
        self.frame.pack(fill=tk.BOTH, expand=True)

        self.color = "black"
        self.marker = 0
        self.requested_at_ms = 0.0
        self.drawn_at_ms = self.now_ms()

        self.root.bind("<space>", self.toggle)
        self.root.bind("<Return>", self.toggle)
        self.root.bind("<Button-1>", self.toggle)
        self.root.bind("<Escape>", self.exit_cleanly)

        self.write_state()
        self.root.after(50, self.ensure_focus)

    def now_ms(self) -> float:
        return time.time_ns() / 1_000_000

    def ensure_focus(self) -> None:
        try:
            self.root.lift()
            self.root.focus_force()
        except Exception:
            pass
        self.root.after(500, self.ensure_focus)

    def write_state(self) -> None:
        payload = {
            "marker": self.marker,
            "color": self.color,
            "requestedAtMs": self.requested_at_ms,
            "drawnAtMs": self.drawn_at_ms,
            "pid": os.getpid(),
        }
        with open(STATE_PATH, "w", encoding="utf-8") as handle:
            json.dump(payload, handle)
            handle.flush()
            os.fsync(handle.fileno())

    def toggle(self, _event=None) -> None:
        self.marker += 1
        self.requested_at_ms = self.now_ms()
        self.color = "white" if self.color == "black" else "black"
        self.root.configure(background=self.color)
        self.frame.configure(bg=self.color)
        self.root.update_idletasks()
        self.drawn_at_ms = self.now_ms()
        self.write_state()

    def exit_cleanly(self, _event=None) -> None:
        try:
            os.remove(STATE_PATH)
        except FileNotFoundError:
            pass
        self.root.destroy()

    def run(self) -> None:
        self.root.mainloop()


def main() -> int:
    app = LatencyProbeApp()
    signal.signal(signal.SIGTERM, lambda *_args: app.exit_cleanly())
    signal.signal(signal.SIGINT, lambda *_args: app.exit_cleanly())
    app.run()
    return 0


if __name__ == "__main__":
    sys.exit(main())
