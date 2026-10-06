#!/usr/bin/env python3
"""Download a random free Unsplash wallpaper and apply it on macOS (Python 3.9+).

No packages, login, API key, or browser required. Uses the website's Wallpapers
topic, samples its full page range, and handles Anubis preact/fast challenges
with a persistent cookie jar.
The website's private JSON endpoint may change; failures fall back to Bing's
daily images, then cached images. Downloads are JPEG, at most 32 MiB. Unsplash
images are never upscaled; Bing supplies its UHD version (usually 3840 px).

Run once from Terminal before scheduling in your own user's crontab. By default,
update all Spaces and displays via WallpaperAgent's store (macOS 14+). Use
--current-space for AppKit's active-desktop behavior, including on older macOS.
Use absolute paths for both Python and this script in cron (cron has a small PATH).
For example, after checking `command -v python3`, using Homebrew on Apple Silicon:
  0 */3 * * * /opt/homebrew/bin/python3 /absolute/path/unsplash_wallpaper.py --quiet

Cache: ~/Library/Caches/unsplash-wallpaper. Keep eight images and remove images
unused for 30 days, always protecting the current wallpaper. Cache fallback
prefers unused images, then the least recently used. State and cookies
are written atomically. A nonblocking process lock makes overlapping runs no-ops.
Exit codes: 0 success/overlap, 1 runtime failure, 2 invalid arguments, 130 interrupt.
"""

import argparse
import copy
import datetime
import email.utils
import fcntl
import hashlib
import http.client
import http.cookiejar
import json
import os
from pathlib import Path
import plistlib
import random
import re
import signal
import stat
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from contextlib import contextmanager


ORIGIN = "https://unsplash.com"
USER_AGENT = "unsplash-wallpaper/1.0 (Python urllib)"
IMAGE_NAME = re.compile(r"wallpaper-[A-Za-z0-9_-]{1,80}\.jpg\Z")
MAX_IMAGE = 32 * 1024 * 1024
MAX_PAGE = 2 * 1024 * 1024
RNG = random.SystemRandom()
PHOTOS_PER_PAGE = 30
TOPIC_REFRESH = 86400
FALLBACK_PAGES = 50
PAGE_ATTEMPTS = 5

# argv passes the filename as data, rather than interpolating it into source.
SET_WALLPAPER = r"""
ObjC.import('AppKit');
function run(argv) {
    var screens = $.NSScreen.screens;
    if (!screens.count) throw Error('No displays; run in a logged-in desktop session');
    var url = $.NSURL.fileURLWithPath(argv[0]);
    var image = $.NSImage.alloc.initWithContentsOfURL(url);
    if (!image || !image.isValid) throw Error('macOS could not decode the image');
    if (argv[1] === 'validate') return;
    var workspace = $.NSWorkspace.sharedWorkspace;
    for (var i = 0; i < screens.count; i++) {
        var error = Ref();
        if (!workspace.setDesktopImageURLForScreenOptionsError(
                url, screens.objectAtIndex(i), $({}), error)) {
            throw Error(error[0] ? ObjC.unwrap(error[0].localizedDescription)
                                : 'Could not set wallpaper');
        }
    }
}
"""


class WallpaperError(Exception):
    """An expected failure suitable for a concise cron log."""


def warn(message):
    print(f"unsplash-wallpaper: {message}", file=sys.stderr)


@contextmanager
def atomic_file(path, binary=False):
    """Keep readers from seeing partial images, state, or cookies."""
    fd, name = tempfile.mkstemp(prefix=f".{path.name}.", suffix=".tmp", dir=path.parent)
    temporary = Path(name)
    try:
        with os.fdopen(fd, "wb" if binary else "w", **({} if binary else {"encoding": "utf-8"})) as stream:
            yield stream, temporary
            stream.flush()
            os.fsync(stream.fileno())
        temporary.replace(path)
    finally:
        temporary.unlink(missing_ok=True)


def save_state(cache, state):
    with atomic_file(cache / "state.json") as (stream, _):
        json.dump(state, stream, ensure_ascii=False, indent=2)
        stream.write("\n")


def load_state(cache):
    try:
        state = json.loads((cache / "state.json").read_text(encoding="utf-8"))
        if (not isinstance(state, dict) or state.get("version") != 1
                or not isinstance(state.get("photos"), dict)
                or not isinstance(state.get("history"), list)
                or not isinstance(state.get("retry_after", 0), (int, float))
                or not isinstance(state.get("current", ""), str)):
            raise ValueError("unexpected state schema")
        return state
    except FileNotFoundError:
        pass
    except (ValueError, UnicodeError) as exc:
        # Do not guess which cached file is the active wallpaper after corruption.
        raise WallpaperError(f"invalid {cache / 'state.json'}: {exc}") from exc
    return {"version": 1, "photos": {}, "history": [], "current": "", "retry_after": 0}


def script_json(html, element_id):
    match = re.search(
        r'<script\b[^>]*\bid=[\"\']' + re.escape(element_id)
        + r'[\"\'][^>]*>(.*?)</script>', html, re.DOTALL | re.IGNORECASE)
    return json.loads(match.group(1)) if match else None


def retry_time(value):
    """Retry-After can be seconds or an HTTP date; default to a one-hour cooldown."""
    now = time.time()
    try:
        return now + max(60, int(value))
    except (ValueError, TypeError):
        try:
            return max(now + 60, email.utils.parsedate_to_datetime(value).timestamp())
        except (ValueError, TypeError, OverflowError):
            return now + 3600


class Unsplash:
    def __init__(self, cache, state, timeout):
        self.cache, self.state, self.timeout = cache, state, timeout
        self.cookies = http.cookiejar.MozillaCookieJar(str(cache / "cookies.txt"))
        try:
            self.cookies.load(ignore_discard=True)
        except FileNotFoundError:
            pass
        except (OSError, http.cookiejar.LoadError):
            warn("discarding unreadable cookies")
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.cookies))
        self.opener.addheaders = [("User-Agent", USER_AGENT), ("Accept-Language", "en-US,en;q=0.9")]

    def save_cookies(self):
        with atomic_file(self.cache / "cookies.txt") as (_, temporary):
            self.cookies.save(str(temporary), ignore_discard=True)

    def read(self, url, limit):
        """Retry transient failures with backoff; persist Unsplash's HTTP 429 cooldown."""
        # Connection-refused/reset blips often clear within seconds; back off and retry.
        for attempt in range(3):
            try:
                try:
                    response = self.opener.open(url, timeout=self.timeout)
                except urllib.error.HTTPError as exc:
                    response = exc  # Anubis uses HTTP 401, but its body is needed.
                with response:
                    status, headers, final_url = response.code, response.headers, response.url
                    if status == 429:
                        if urllib.parse.urlsplit(url).hostname in ("unsplash.com", "images.unsplash.com"):
                            self.state["retry_after"] = retry_time(headers.get("Retry-After"))
                        raise WallpaperError("image service rate limited requests")
                    data = response.read(limit + 1)
                if len(data) > limit:
                    raise WallpaperError("image service response exceeds the size limit")
                if status in (500, 502, 503, 504) and attempt < 2:
                    time.sleep(1)
                    continue
                return status, headers, final_url, data
            except (urllib.error.URLError, TimeoutError, ConnectionError, http.client.HTTPException) as exc:
                if attempt == 2:
                    raise WallpaperError(f"network request failed: {exc}") from exc
                time.sleep(2 ** attempt)

    def solve(self, html, url):
        """Answer only known Anubis algorithms, with bounded CPU work."""
        info = script_json(html, "preact_info")
        if info:
            target = urllib.parse.urljoin(url, info["redir"])
            parsed = urllib.parse.urlsplit(target)
            if parsed.scheme != "https" or parsed.netloc != "unsplash.com":
                raise WallpaperError("unexpected Anubis challenge destination")
            digest = hashlib.sha256(info["challenge"].encode()).hexdigest()
            time.sleep(min(max(float(info.get("difficulty", 1)) * 0.125, 0), 2))
            return target + ("&" if parsed.query else "?") + urllib.parse.urlencode({"result": digest})

        info = script_json(html, "anubis_challenge")
        if not info or info["rules"]["algorithm"] != "fast":
            raise WallpaperError("unsupported anti-bot challenge; try again later")
        challenge = info["challenge"]
        difficulty = info["rules"]["difficulty"]
        if not isinstance(difficulty, int) or not 1 <= difficulty <= 6:
            raise WallpaperError("Anubis difficulty exceeds the CPU budget")
        start, nonce = time.monotonic(), 0
        prefix = "0" * difficulty
        while True:
            digest = hashlib.sha256((challenge["randomData"] + str(nonce)).encode()).hexdigest()
            if digest.startswith(prefix):
                break
            nonce += 1
            if nonce % 4096 == 0 and time.monotonic() - start > 10:
                raise WallpaperError("Anubis challenge exceeded the 10-second CPU budget")
        return ORIGIN + "/.within.website/x/cmd/anubis/api/pass-challenge?" + urllib.parse.urlencode({
            "id": challenge["id"], "response": digest, "nonce": nonce, "redir": url,
            "elapsedTime": max(1, int((time.monotonic() - start) * 1000)),
        })

    def listing(self, url):
        """Read topic JSON through the same challenge flow as photo listings."""
        for attempt in range(3):
            status, _, final_url, data = self.read(url, MAX_PAGE)
            html = data.decode("utf-8", errors="replace")
            if "anubis_challenge" in html or "preact_info" in html:
                if attempt == 2:
                    raise WallpaperError("Anubis did not accept the challenge response")
                try:
                    url = self.solve(html, final_url)
                except (KeyError, TypeError, ValueError) as exc:
                    raise WallpaperError("unrecognized Anubis challenge format") from exc
                continue
            if status != 200:
                raise WallpaperError(f"wallpaper listing returned HTTP {status}")
            try:
                return json.loads(data)
            except ValueError as exc:
                raise WallpaperError("wallpaper listing is not JSON (website may have changed)") from exc

    def page_count(self):
        """Discover the whole topic; reuse its size for a day to avoid extra requests."""
        topic = self.state.get("topic", {})
        if not isinstance(topic, dict):
            topic = {}
        total, checked = topic.get("total_photos"), topic.get("checked_at", 0)
        known = type(total) is int and 0 < total <= 3_000_000
        if (known and isinstance(checked, (int, float))
                and 0 <= time.time() - checked < TOPIC_REFRESH):
            return (total + PHOTOS_PER_PAGE - 1) // PHOTOS_PER_PAGE
        try:
            details = self.listing(ORIGIN + "/napi/topics/wallpapers")
            count = details.get("total_photos") if isinstance(details, dict) else None
            if type(count) is not int or not 0 < count <= 3_000_000:
                raise WallpaperError("unexpected wallpaper topic size")
        except WallpaperError as exc:
            if self.state["retry_after"] > time.time():
                raise
            warn(f"could not refresh wallpaper topic size: {exc}; using known page range")
            return (total + PHOTOS_PER_PAGE - 1) // PHOTOS_PER_PAGE if known else FALLBACK_PAGES
        self.state["topic"] = {"total_photos": count, "checked_at": time.time()}
        return (count + PHOTOS_PER_PAGE - 1) // PHOTOS_PER_PAGE

    def photos(self, page):
        photos = self.listing(ORIGIN + "/napi/topics/wallpapers/photos?" + urllib.parse.urlencode({
            "page": page, "per_page": PHOTOS_PER_PAGE, "order_by": "latest",
        }))
        if not isinstance(photos, list):
            raise WallpaperError("unexpected wallpaper listing format")
        return photos

    def download(self, photo, width):
        raw = photo["urls"]["raw"]
        parsed = urllib.parse.urlsplit(raw)
        if parsed.scheme != "https" or parsed.netloc != "images.unsplash.com":
            raise WallpaperError("unexpected image host")
        query = dict(urllib.parse.parse_qsl(parsed.query))
        query.update(w=str(min(width, photo["width"])), fit="max", fm="jpg", q="85")
        query.pop("auto", None)
        url = urllib.parse.urlunsplit(parsed._replace(query=urllib.parse.urlencode(query)))
        status, headers, _, data = self.read(url, MAX_IMAGE)
        if status != 200:
            raise WallpaperError(f"image download returned HTTP {status}")
        if headers.get_content_type() != "image/jpeg" or not valid_jpeg(data):
            raise WallpaperError("download is not a complete JPEG")
        path = self.cache / f"wallpaper-{photo['id']}.jpg"
        with atomic_file(path, binary=True) as (stream, _):
            stream.write(data)
        self.state["photos"][path.name] = {
            "id": photo["id"], "credit": photo.get("user", {}).get("name", "Unknown"),
            "url": photo.get("links", {}).get("html", ORIGIN + "/photos/" + photo["id"]),
            "source": "Unsplash",
        }
        return path

    def bing(self):
        """Prefer today's Bing image, then unseen daily images from the last week."""
        url = "https://www.bing.com/HPImageArchive.aspx?format=js&idx=0&n=8&mkt=zh-CN"
        status, _, _, data = self.read(url, MAX_PAGE)
        if status != 200:
            raise WallpaperError(f"Bing daily listing returned HTTP {status}")
        try:
            images = json.loads(data)["images"]
            if not isinstance(images, list):
                raise ValueError("images is not a list")
            for image in images:
                base = image["urlbase"]
                if not isinstance(base, str) or not base.startswith("/th?id=OHR."):
                    continue
                photo_id = "bing-" + hashlib.sha256(base.encode()).hexdigest()[:16]
                if photo_id in self.state["history"]:
                    continue
                status, headers, _, data = self.read("https://www.bing.com" + base + "_UHD.jpg", MAX_IMAGE)
                if status != 200 or headers.get_content_type() != "image/jpeg" or not valid_jpeg(data):
                    raise WallpaperError("Bing did not return a complete JPEG")
                path = self.cache / f"wallpaper-{photo_id}.jpg"
                with atomic_file(path, binary=True) as (stream, _):
                    stream.write(data)
                self.state["photos"][path.name] = {
                    "id": photo_id, "source": "Bing", "credit": image.get("copyright", "Bing daily image"),
                    "url": image.get("copyrightlink", "https://www.bing.com"),
                }
                return path
        except (ValueError, KeyError, TypeError) as exc:
            raise WallpaperError("unexpected Bing daily listing format") from exc
        raise WallpaperError("all available Bing daily images were used recently")


def valid_jpeg(data):
    return data.startswith(b"\xff\xd8\xff") and data.endswith(b"\xff\xd9")


def eligible(photo):
    if not isinstance(photo, dict):
        return False
    try:
        return (not photo.get("premium") and not photo.get("plus")
                and re.fullmatch(r"[A-Za-z0-9_-]{1,80}", photo["id"]) is not None
                and photo["width"] >= 1920 and photo["height"] >= 1080
                and photo["width"] / photo["height"] >= 1.2
                and isinstance(photo["urls"]["raw"], str)
                and urllib.parse.urlsplit(photo["urls"]["raw"]).netloc == "images.unsplash.com")
    except (KeyError, TypeError, ValueError, ZeroDivisionError):
        return False


def fetch_wallpaper(client, state, width):
    # Sample the whole topic, rather than repeatedly visiting only its newest slice.
    count = client.page_count()
    pages = RNG.sample(range(1, count + 1), min(PAGE_ATTEMPTS, count)) + [1]
    excluded = set(state["history"])
    excluded.add(Path(state["current"]).stem.removeprefix("wallpaper-"))
    last_error = None
    for page in dict.fromkeys(pages):
        try:
            candidates = [p for p in client.photos(page) if eligible(p)]
        except WallpaperError as exc:
            if state["retry_after"] > time.time():
                raise
            last_error = exc
            continue
        unseen = [p for p in candidates if p["id"] not in excluded]
        RNG.shuffle(unseen)
        for photo in unseen[:3]:
            if photo["id"] in excluded:
                continue
            excluded.add(photo["id"])
            try:
                return client.download(photo, width)
            except WallpaperError as exc:
                if state["retry_after"] > time.time():
                    raise
                last_error = exc
    message = "no new free landscape wallpaper found"
    if last_error is not None:
        message += f"; last request failed: {last_error}"
    raise WallpaperError(message)


def cached_images(cache):
    images = []
    for path in cache.iterdir():
        if IMAGE_NAME.fullmatch(path.name) and not path.is_symlink() and path.is_file():
            with path.open("rb") as stream:
                start = stream.read(3)
                if path.stat().st_size < 4:
                    continue
                stream.seek(-2, os.SEEK_END)
                if valid_jpeg(start + stream.read(2)):
                    images.append(path)
    return sorted(images, key=lambda p: p.stat().st_mtime, reverse=True)


def choose_cached(cache, state):
    """Use every cached image before repeating, then rotate least recently used."""
    images = cached_images(cache)
    if not images:
        raise WallpaperError("no usable cached wallpapers; retry when the network is available")
    alternatives = [p for p in images if p.name != state["current"]] or images
    recency = {photo_id: index for index, photo_id in enumerate(state["history"])}

    def photo_id(path):
        return path.stem.removeprefix("wallpaper-")

    unseen = [p for p in alternatives if photo_id(p) not in recency]
    if unseen:
        return RNG.choice(unseen)
    return min(alternatives, key=lambda p: recency[photo_id(p)])


def cleanup(cache, state, keep, max_age, selected):
    protected = {state["current"]}
    if selected is not None:
        protected.add(selected.name)
    images = cached_images(cache)
    valid_names = {p.name for p in images}
    for path in cache.iterdir():
        if (IMAGE_NAME.fullmatch(path.name) and not path.is_symlink() and path.is_file()
                and path.name not in valid_names and path.name not in protected):
            path.unlink()
    retained = {p.name for p in images if p.name in protected}
    cutoff = time.time() - max_age * 86400
    for path in images:
        if path.name in retained:
            continue
        if len(retained) < keep and path.stat().st_mtime >= cutoff:
            retained.add(path.name)
        else:
            path.unlink()
    state["photos"] = {name: details for name, details in state["photos"].items() if name in retained}


def wallpaper_command(command, **kwargs):
    try:
        result = subprocess.run(
            command, text=True, capture_output=True, timeout=30, **kwargs)
    except subprocess.TimeoutExpired as exc:
        raise WallpaperError("macOS wallpaper update timed out") from exc
    return result


def wallpaper_store_for_image(store, path):
    """Replace desktop slots, keeping display/Space IDs and screensavers intact."""
    if not isinstance(store, dict) or not {"SystemDefault", "Spaces", "Displays"} <= store.keys():
        raise WallpaperError("unrecognized macOS wallpaper store")
    updated = copy.deepcopy(store)
    slots = []

    def add_slot(container, key):
        slot = container.get(key, "$null")
        if slot == "$null":
            slot = container[key] = {}
        if not isinstance(slot, dict) or slot.get("Type") not in (
                None, "desktop", "idle", "individual", "linked"):
            raise WallpaperError("unrecognized macOS wallpaper slot")
        slots.append(slot)

    def add_displays(container):
        displays = container.get("Displays", {})
        if not isinstance(displays, dict):
            raise WallpaperError("unrecognized macOS wallpaper displays")
        for display_id in displays:
            add_slot(displays, display_id)

    add_slot(updated, "AllSpacesAndDisplays")
    add_slot(updated, "SystemDefault")
    add_displays(updated)
    if not isinstance(updated["Spaces"], dict):
        raise WallpaperError("unrecognized macOS wallpaper Spaces")
    for space in updated["Spaces"].values():
        if not isinstance(space, dict):
            raise WallpaperError("unrecognized macOS wallpaper Space")
        add_slot(space, "Default")
        add_displays(space)

    now = datetime.datetime.now(datetime.timezone.utc).replace(tzinfo=None)
    # This is the configuration emitted by NSWorkspace on macOS 15. The image
    # provider requires backgroundColor as well as placement: omitting it makes
    # WallpaperAgent fail decoding with NSCocoaErrorDomain 4865 and render the
    # default wallpaper, even though desktopImageURLForScreen returns our URL.
    configuration = {
        "placement": 1,
        "backgroundColor": {
            "colorSpace": plistlib.dumps("kCGColorSpaceGenericRGB", fmt=plistlib.FMT_BINARY),
            "components": [0.0, 0.0, 0.0, 1.0],
        },
    }
    desktop = {
        "Content": {
            "Choices": [{
                "Provider": "com.apple.wallpaper.choice.image",
                "Files": [{"relative": path.resolve().as_uri()}],
                "Configuration": plistlib.dumps(configuration, fmt=plistlib.FMT_BINARY),
            }],
            "Shuffle": "$null",
        },
        "LastSet": now, "LastUse": now,
    }
    for slot in slots:
        # Linked aerials use their desktop content as the screensaver too.
        if slot.get("Type") == "linked" and "Idle" not in slot and "Desktop" in slot:
            slot["Idle"] = copy.deepcopy(slot["Desktop"])
        slot["Desktop"] = copy.deepcopy(desktop)
        slot["Type"] = "individual" if isinstance(slot.get("Idle"), dict) else "desktop"
    return updated


def set_all_spaces(path):
    """Pause only this user's agent so it cannot overwrite the atomic store edit."""
    store_path = Path.home() / "Library/Application Support/com.apple.wallpaper/Store/Index.plist"
    if not store_path.is_file():
        raise WallpaperError("all-Spaces wallpaper setting requires the macOS 14+ wallpaper store; "
                             "use --current-space for active desktops on older macOS")
    result = wallpaper_command(["/usr/bin/pgrep", "-u", str(os.getuid()), "-x", "WallpaperAgent"])
    if result.returncode or not result.stdout.strip():
        raise WallpaperError("WallpaperAgent is unavailable; run in a logged-in desktop session")
    try:
        pids = [int(pid) for pid in result.stdout.split()]
    except ValueError as exc:
        raise WallpaperError("could not identify WallpaperAgent") from exc
    stopped, original, written = [], None, False

    def write_store(data):
        with atomic_file(store_path, binary=True) as (stream, temporary):
            stream.write(data)
            temporary.chmod(stat.S_IMODE(store_path.stat().st_mode))

    try:
        for pid in pids:
            os.kill(pid, signal.SIGSTOP)
            stopped.append(pid)
        original = store_path.read_bytes()
        try:
            store = plistlib.loads(original)
        except (ValueError, TypeError, OverflowError) as exc:
            raise WallpaperError("invalid macOS wallpaper store") from exc
        updated = wallpaper_store_for_image(store, path)
        fmt = plistlib.FMT_BINARY if original.startswith(b"bplist00") else plistlib.FMT_XML
        write_store(plistlib.dumps(updated, fmt=fmt, sort_keys=False))
        written = True
        # SIGTERM is delivered once SIGCONT resumes the agent. launchd restarts it
        # with the new store; no desktop switching or Accessibility access needed.
        for pid in stopped:
            os.kill(pid, signal.SIGTERM)
    except BaseException:
        if written:
            write_store(original)
        raise
    finally:
        resume_error = None
        for pid in stopped:
            try:
                os.kill(pid, signal.SIGCONT)
            except ProcessLookupError:
                pass
            except OSError as exc:
                # Attempt to resume every paused agent even if one signal fails.
                resume_error = exc
        if resume_error is not None:
            raise resume_error


def set_wallpaper(path, current_space=False):
    result = wallpaper_command(
        ["/usr/bin/osascript", "-l", "JavaScript", "-", str(path),
         "apply" if current_space else "validate"], input=SET_WALLPAPER)
    if result.returncode:
        raise WallpaperError("macOS wallpaper update failed: " + result.stderr.strip())
    if not current_space:
        set_all_spaces(path)


def bounded_int(low, high):
    def parse(value):
        try:
            number = int(value)
        except ValueError as exc:
            raise argparse.ArgumentTypeError("must be an integer") from exc
        if not low <= number <= high:
            raise argparse.ArgumentTypeError(f"must be between {low} and {high}")
        return number
    return parse


def arguments(argv=None):
    parser = argparse.ArgumentParser(
        description="Random Unsplash wallpapers for macOS, with Bing fallback; no login or dependencies.",
        epilog="Run from Terminal once, then schedule with absolute paths in your user crontab.")
    parser.add_argument("--cache-dir", type=Path, default=Path.home() / "Library/Caches/unsplash-wallpaper",
                        metavar="DIR", help="image/state directory (default: ~/Library/Caches/unsplash-wallpaper)")
    parser.add_argument("--keep", type=bounded_int(2, 50), default=8, metavar="N",
                        help="maximum cached images, including the current wallpaper (default: 8)")
    parser.add_argument("--max-age", type=bounded_int(1, 3650), default=30, metavar="DAYS",
                        help="remove unused images older than this; protect current image (default: 30)")
    parser.add_argument("--width", type=bounded_int(640, 7680), default=3840, metavar="PX",
                        help="maximum Unsplash image width, without upscaling (default: 3840; Bing uses UHD)")
    parser.add_argument("--timeout", type=bounded_int(1, 120), default=20, metavar="SEC",
                        help="network socket timeout (default: 20)")
    parser.add_argument("--download-only", action="store_true", help="download/choose an image without applying it")
    parser.add_argument("--current-space", action="store_true",
                        help="only update active desktops on connected displays (default: all Spaces, macOS 14+)")
    parser.add_argument("--offline", action="store_true", help="rotate cached images without network requests")
    parser.add_argument("--quiet", action="store_true", help="suppress success messages; keep errors and warnings")
    return parser.parse_args(argv)


def run(args):
    if sys.platform != "darwin" and not args.download_only:
        raise WallpaperError("wallpaper setting requires macOS; use --download-only elsewhere")
    cache = args.cache_dir.expanduser().resolve()
    cache.mkdir(parents=True, exist_ok=True, mode=0o700)
    with (cache / ".lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            if not args.quiet:
                print("Another wallpaper run is active; skipped.")
            return
        # All our temporary files are abandoned once the exclusive lock is acquired.
        for path in cache.glob(".*.tmp"):
            if re.fullmatch(r"\.(?:state\.json|cookies\.txt|wallpaper-[A-Za-z0-9_-]+\.jpg)\..+\.tmp", path.name):
                path.unlink()
        state = load_state(cache)
        client, selected = Unsplash(cache, state, args.timeout), None
        from_cache = False
        try:
            if not args.offline:
                try:
                    if state["retry_after"] > time.time():
                        raise WallpaperError("Unsplash cooldown active")
                    selected = fetch_wallpaper(client, state, args.width)
                except WallpaperError as exc:
                    warn(f"{exc}; trying Bing daily images")
                    try:
                        selected = client.bing()
                    except WallpaperError as bing_exc:
                        warn(f"{bing_exc}; trying cached wallpapers")
            if selected is None:
                selected = choose_cached(cache, state)
                from_cache = True
            if not args.download_only:
                set_wallpaper(selected, current_space=args.current_space)
                state["current"] = selected.name
            os.utime(selected, None)  # Age means last use, not filesystem access time.
            photo_id = selected.stem.removeprefix("wallpaper-")
            state["history"] = [p for p in state["history"] if p != photo_id][-199:] + [photo_id]
            if not args.quiet:
                action = "Selected" if args.download_only else "Wallpaper set"
                if from_cache:
                    action += " (cached rotation)"
                print(f"{action}: {selected}")
                details = state["photos"].get(selected.name, {})
                if details:
                    print(f"{details.get('source', 'Unsplash')}: {details['credit']} — {details['url']}")
        finally:
            # Save cooldown/cookies even after failure. Never prune the previous active image.
            cleanup(cache, state, args.keep, args.max_age, selected)
            save_state(cache, state)
            if not args.offline:
                client.save_cookies()


def main(argv=None):
    args = arguments(argv)
    try:
        run(args)
    except (WallpaperError, OSError) as exc:
        warn(str(exc))
        return 1
    except KeyboardInterrupt:
        warn("interrupted")
        return 130
    return 0


if __name__ == "__main__":
    sys.exit(main())
