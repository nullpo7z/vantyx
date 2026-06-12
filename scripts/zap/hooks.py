# ZAP scan hooks: session cookie + SPA route seeds for broader coverage.
# https://www.zaproxy.org/docs/docker/scan-hooks/
import os

ZAP_COOKIE = os.environ.get("VANTYX_ZAP_SESSION_COOKIE", "").strip()
SEED_PATHS = [
    p.strip()
    for p in os.environ.get(
        "VANTYX_ZAP_SEED_PATHS",
        "/,/terminal,/vnc,/rdp,/files,/tftp-console,/docs",
    ).split(",")
    if p.strip()
]


def zap_started(zap, target):
    """Pre-seed known SPA routes so the spider starts beyond /."""
    base = target.rstrip("/")
    for path in SEED_PATHS:
        url = base + (path if path.startswith("/") else "/" + path)
        try:
            zap.urlopen(url)
        except Exception:
            pass


def zap_http_request(msg):
    if not ZAP_COOKIE:
        return
    hdr = msg.getRequestHeader()
    if hdr.getHeader("Cookie") is None:
        hdr.setHeader("Cookie", ZAP_COOKIE)
