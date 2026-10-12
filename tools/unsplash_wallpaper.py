#!/usr/bin/env python3
"""Download a random free Unsplash wallpaper and apply it on macOS (Python 3.9+).

No packages, login, API key, or browser required. Uses the website's Wallpapers
topic, samples its full page range, and handles Anubis preact/fast/slow
challenges with a persistent cookie jar. Each listing queues a few random photos
from its page, so most runs need only an image request, and consecutive
wallpapers mix several pages. The last 2000 photos are never repeated, and
photographers used recently are skipped while others are available.
The website's private JSON endpoint may change; failures fall back to queued
photos, Bing's daily images (about two weeks), then cached images. An
unreachable host ends that source at once, and each source has a two-minute
network budget. Downloads are JPEG, at most 32 MiB. Unsplash images are never
upscaled; Bing supplies its UHD version (usually 3840 px).

Run once from Terminal before scheduling in your own user's crontab. By default,
update all Spaces and displays via WallpaperAgent's store (macOS 14+). Use
--current-space for AppKit's active-desktop behavior, including on older macOS.
Use absolute paths for both Python and this script in cron (cron has a small PATH).
For example, after checking `command -v python3`, using Homebrew on Apple Silicon:
  0 */3 * * * /opt/homebrew/bin/python3 /absolute/path/unsplash_wallpaper.py --quiet

Cache: ~/Library/Caches/unsplash-wallpaper. Keep eight images and remove images
unused for 30 days, always protecting the current wallpaper. Cache fallback
prefers unused images, then the least recently used; images macOS cannot decode
are discarded. State and cookies are written atomically; invalid state is moved
to state.json.corrupt and rebuilt. A nonblocking process lock makes overlapping
runs no-ops, and SIGTERM/SIGHUP still save state and never leave WallpaperAgent paused.
Exit codes: 0 success/overlap, 1 runtime failure, 2 invalid arguments,
130 interrupt, 129/143 SIGHUP/SIGTERM.
"""

import argparse
import copy
import datetime
import email.utils
import fcntl
import hashlib
import http.client
import http.cookiejar
import itertools
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
UNSPLASH_HOSTS = ("unsplash.com", "images.unsplash.com")
USER_AGENT = "unsplash-wallpaper/1.0 (Python urllib)"
PHOTO_ID = re.compile(r"[A-Za-z0-9_-]{1,80}\Z")
IMAGE_NAME = re.compile(r"wallpaper-[A-Za-z0-9_-]{1,80}\.jpg\Z")
BING_BASE = re.compile(r"/th\?id=OHR\.[A-Za-z0-9_.-]{1,200}\Z")
MAX_IMAGE = 32 * 1024 * 1024
MAX_PAGE = 2 * 1024 * 1024
RNG = random.SystemRandom()
PHOTOS_PER_PAGE = 30
TOPIC_REFRESH = 86400
FALLBACK_PAGES = 50
PAGE_ATTEMPTS = 5
DOWNLOAD_ATTEMPTS = 4
QUEUE_LOW = 8           # List another page when fewer photos are queued.
QUEUE_TAKE = 6          # Random unseen photos queued from each listed page.
QUEUE_MAX_AGE = 14 * 86400
HISTORY_LIMIT = 2000
AUTHOR_MEMORY = 12
MIN_ASPECT, MAX_ASPECT = 1.2, 2.5
NETWORK_BUDGET = 120    # Seconds per source (Unsplash, then Bing).
MAX_COOLDOWN = 86400

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


class NetworkError(WallpaperError):
    """A host stayed unreachable after retries; stop using that source this run."""


class BadImageError(WallpaperError):
    """macOS cannot decode a cached image, so it should be discarded."""


class Terminated(BaseException):
    """SIGTERM/SIGHUP, raised like KeyboardInterrupt so cleanup still runs."""

    def __init__(self, signum):
        super().__init__(signum)
        self.signum = signum


def warn(message):
    print(f"unsplash-wallpaper: {message}", file=sys.stderr)


def image_id(name):
    return Path(name).stem.removeprefix("wallpaper-")


def remember(items, value, limit):
    """Move value to the most recent end of a bounded list."""
    return [item for item in items if item != value][-(limit - 1):] + [value]


def cooling_down(state):
    return state["retry_after"] > time.time()


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


def unsplash_image(url):
    parsed = urllib.parse.urlsplit(url)
    return parsed.scheme == "https" and parsed.netloc == "images.unsplash.com"


def queued_ok(entry, now):
    try:
        return (PHOTO_ID.match(entry["id"]) is not None and unsplash_image(entry["raw"])
                and type(entry["width"]) is int and entry["width"] > 0
                and all(isinstance(entry[key], str) for key in ("credit", "author", "url"))
                and 0 <= now - entry["queued_at"] < QUEUE_MAX_AGE)
    except (KeyError, TypeError, ValueError):
        return False


def load_state(cache):
    """Validate the core schema; repair optional fields; quarantine corrupt files."""
    path = cache / "state.json"
    fresh = {"version": 1, "photos": {}, "history": [], "current": "", "retry_after": 0,
             "queue": [], "authors": []}
    try:
        state = json.loads(path.read_text(encoding="utf-8"))
        if (not isinstance(state, dict) or state.get("version") != 1
                or not isinstance(state.get("photos"), dict)
                or not isinstance(state.get("history"), list)
                or not isinstance(state.get("retry_after", 0), (int, float))
                or not isinstance(state.get("current", ""), str)):
            raise ValueError("unexpected state schema")
    except FileNotFoundError:
        return fresh
    except (ValueError, UnicodeError, RecursionError) as exc:
        # A cron job must not stay broken forever. The most recently used image
        # is almost certainly the active wallpaper, so keep protecting it.
        path.replace(path.with_name("state.json.corrupt"))
        images = cached_images(cache)
        fresh["current"] = images[0].name if images else ""
        warn(f"moved invalid state to state.json.corrupt ({exc}); starting fresh")
        return fresh
    now = time.time()
    state["photos"] = {name: details for name, details in state["photos"].items()
                       if IMAGE_NAME.match(name) and isinstance(details, dict)}
    state["history"] = [p for p in state["history"] if isinstance(p, str)][-HISTORY_LIMIT:]
    state.setdefault("current", "")
    # A bogus far-future Retry-After or a clock jump must not disable Unsplash.
    state["retry_after"] = min(state.get("retry_after", 0), now + MAX_COOLDOWN)
    queue = state.get("queue")
    state["queue"] = [c for c in queue if queued_ok(c, now)] if isinstance(queue, list) else []
    authors = state.get("authors")
    state["authors"] = ([a for a in authors if isinstance(a, str) and a][-AUTHOR_MEMORY:]
                        if isinstance(authors, list) else [])
    return state


def script_json(html, element_id):
    match = re.search(
        r'<script\b[^>]*\bid=[\"\']' + re.escape(element_id)
        + r'[\"\'][^>]*>(.*?)</script>', html, re.DOTALL | re.IGNORECASE)
    return json.loads(match.group(1)) if match else None


def retry_time(value):
    """Retry-After can be seconds or an HTTP date; default to a one-hour cooldown."""
    now = time.time()
    try:
        delay = max(60, int(value))
    except (ValueError, TypeError):
        try:
            delay = max(60, email.utils.parsedate_to_datetime(value).timestamp() - now)
        except (ValueError, TypeError, OverflowError):
            delay = 3600
    return now + min(delay, MAX_COOLDOWN)


class Unsplash:
    """HTTP client for Unsplash and Bing, sharing cookies, retries, and budgets."""

    def __init__(self, cache, state, timeout):
        self.cache, self.state, self.timeout = cache, state, timeout
        self.start_budget()
        self.cookies = http.cookiejar.MozillaCookieJar(str(cache / "cookies.txt"))
        try:
            self.cookies.load(ignore_discard=True)
        except FileNotFoundError:
            pass
        except (OSError, http.cookiejar.LoadError):
            warn("discarding unreadable cookies")
            self.cookies.clear()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.cookies))
        self.opener.addheaders = [("User-Agent", USER_AGENT), ("Accept-Language", "en-US,en;q=0.9")]

    def start_budget(self):
        self.deadline = time.monotonic() + NETWORK_BUDGET

    def pause(self, seconds):
        """Jittered backoff that never sleeps past the network budget."""
        remaining = self.deadline - time.monotonic()
        time.sleep(max(0.0, min(seconds * RNG.uniform(0.75, 1.25), remaining)))

    def save_cookies(self):
        with atomic_file(self.cache / "cookies.txt") as (_, temporary):
            self.cookies.save(str(temporary), ignore_discard=True)

    def read(self, url, limit):
        """Retry transient failures with backoff; persist Unsplash's HTTP 429 cooldown."""
        host = urllib.parse.urlsplit(url).hostname
        for attempt in range(3):
            remaining = self.deadline - time.monotonic()
            if remaining <= 0:
                raise NetworkError(f"network time budget exhausted for {host}")
            try:
                try:
                    response = self.opener.open(url, timeout=min(self.timeout, remaining))
                except urllib.error.HTTPError as exc:
                    response = exc  # Anubis uses HTTP 401, but its body is needed.
                with response:
                    status, headers, final_url = response.code, response.headers, response.url
                    if status == 429:
                        if host in UNSPLASH_HOSTS:
                            self.state["retry_after"] = retry_time(headers.get("Retry-After"))
                        raise WallpaperError("image service rate limited requests")
                    length = headers.get("Content-Length", "")
                    if length.isdigit() and int(length) > limit:
                        raise WallpaperError("image service response exceeds the size limit")
                    data = response.read(limit + 1)
            # OSError covers URLError, SSL errors, resets, and Python 3.9's socket.timeout.
            except (OSError, http.client.HTTPException) as exc:
                if attempt == 2:
                    raise NetworkError(f"network request to {host} failed: {exc}") from exc
                self.pause(2 ** attempt)
                continue
            if len(data) > limit:
                raise WallpaperError("image service response exceeds the size limit")
            if status in (500, 502, 503, 504) and attempt < 2:
                self.pause(2 ** attempt)
                continue
            return status, headers, final_url, data

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
        # "slow" differs from "fast" only in the browser's worker strategy.
        if not info or info["rules"]["algorithm"] not in ("fast", "slow"):
            raise WallpaperError("unsupported anti-bot challenge; try again later")
        challenge = info["challenge"]
        difficulty = info["rules"]["difficulty"]
        if not isinstance(difficulty, int) or not 1 <= difficulty <= 6:
            raise WallpaperError("Anubis difficulty exceeds the CPU budget")
        # Compare raw digest bytes against leading zero nibbles and reuse the
        # hashed prefix, avoiding a hex conversion per nonce.
        zero_bytes, odd = divmod(difficulty, 2)
        zeros = bytes(zero_bytes)
        seed = hashlib.sha256(challenge["randomData"].encode())
        start = time.monotonic()
        for nonce in itertools.count():
            attempt = seed.copy()
            attempt.update(str(nonce).encode())
            digest = attempt.digest()
            if digest[:zero_bytes] == zeros and (not odd or digest[zero_bytes] < 16):
                break
            if nonce & 0xFFF == 0xFFF and time.monotonic() - start > 10:
                raise WallpaperError("Anubis challenge exceeded the 10-second CPU budget")
        return ORIGIN + "/.within.website/x/cmd/anubis/api/pass-challenge?" + urllib.parse.urlencode({
            "id": challenge["id"], "response": digest.hex(), "nonce": nonce, "redir": url,
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
        except NetworkError:
            raise
        except WallpaperError as exc:
            if cooling_down(self.state):
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

    def save_image(self, photo_id, data, details):
        path = self.cache / f"wallpaper-{photo_id}.jpg"
        with atomic_file(path, binary=True) as (stream, _):
            stream.write(data)
        self.state["photos"][path.name] = details
        return path

    def download(self, photo, width):
        """Download a queued candidate (see candidate()) at most `width` pixels wide."""
        parsed = urllib.parse.urlsplit(photo["raw"])
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
        return self.save_image(photo["id"], data, {
            "id": photo["id"], "source": "Unsplash", "credit": photo["credit"],
            "author": photo["author"], "url": photo["url"],
        })

    def bing(self):
        """Prefer today's Bing image, then unseen daily images from about two weeks."""
        seen = set(self.state["history"])
        seen.add(image_id(self.state["current"]))
        last_error = None
        # Bing serves at most eight images per request; idx=7 reaches back further.
        for offset in (0, 7):
            status, _, _, data = self.read(
                f"https://www.bing.com/HPImageArchive.aspx?format=js&idx={offset}&n=8&mkt=zh-CN", MAX_PAGE)
            if status != 200:
                raise WallpaperError(f"Bing daily listing returned HTTP {status}")
            try:
                images = json.loads(data)["images"]
                if not isinstance(images, list):
                    raise ValueError("images is not a list")
            except (ValueError, KeyError, TypeError) as exc:
                raise WallpaperError("unexpected Bing daily listing format") from exc
            for image in images:
                base = image.get("urlbase") if isinstance(image, dict) else None
                if not isinstance(base, str) or not BING_BASE.match(base):
                    continue
                photo_id = "bing-" + hashlib.sha256(base.encode()).hexdigest()[:16]
                if photo_id in seen:
                    continue
                seen.add(photo_id)
                status, headers, _, data = self.read("https://www.bing.com" + base + "_UHD.jpg", MAX_IMAGE)
                if status != 200 or headers.get_content_type() != "image/jpeg" or not valid_jpeg(data):
                    last_error = WallpaperError("Bing did not return a complete JPEG")
                    continue
                credit, link = image.get("copyright"), image.get("copyrightlink")
                return self.save_image(photo_id, data, {
                    "id": photo_id, "source": "Bing",
                    "credit": credit if isinstance(credit, str) else "Bing daily image",
                    "url": link if isinstance(link, str) else "https://www.bing.com",
                })
        if last_error is not None:
            raise last_error
        raise WallpaperError("all available Bing daily images were used recently")


def valid_jpeg(data):
    return data.startswith(b"\xff\xd8\xff") and data.endswith(b"\xff\xd9")


def candidate(photo, now=None):
    """Return the compact queue entry for a free high-resolution landscape, else None."""
    try:
        width, height, raw = photo["width"], photo["height"], photo["urls"]["raw"]
        if (photo.get("premium") or photo.get("plus") or PHOTO_ID.match(photo["id"]) is None
                or type(width) is not int or type(height) is not int
                or width < 1920 or height < 1080
                # Very wide panoramas look poor when cropped to fill a display.
                or not MIN_ASPECT <= width / height <= MAX_ASPECT
                or not isinstance(raw, str) or not unsplash_image(raw)):
            return None
        user = photo.get("user") if isinstance(photo.get("user"), dict) else {}
        links = photo.get("links") if isinstance(photo.get("links"), dict) else {}
        url = links.get("html")
        if not isinstance(url, str) or not url.startswith(ORIGIN + "/"):
            url = ORIGIN + "/photos/" + photo["id"]
        return {
            "id": photo["id"], "raw": raw, "width": width,
            "credit": str(user.get("name") or "Unknown"),
            "author": str(user.get("username") or user.get("id") or ""),
            "url": url, "queued_at": time.time() if now is None else now,
        }
    except (AttributeError, KeyError, TypeError, ValueError):
        return None


def topic_pages(client):
    """Lazily yield random pages across the whole topic, then the newest page."""
    count = client.page_count()
    yield from dict.fromkeys(RNG.sample(range(1, count + 1), min(PAGE_ATTEMPTS, count)) + [1])


def refill(client, state, queue, seen, pages):
    """Queue a few random unseen photos from the next usable page.

    Returns the last listing error, or None. Taking only a handful per page
    keeps consecutive wallpapers from coming from the same slice of the topic.
    """
    error = None
    for page in pages:
        try:
            listed = client.photos(page)
        except NetworkError:
            raise
        except WallpaperError as exc:
            if cooling_down(state):
                raise
            error = exc
            continue
        if not listed and page > 1:
            state.pop("topic", None)  # The topic shrank; rediscover its size next run.
        queued = {c["id"] for c in queue}
        fresh = {}
        for photo in listed:
            entry = candidate(photo)
            if entry and entry["id"] not in seen and entry["id"] not in queued:
                fresh.setdefault(entry["id"], entry)
        if fresh:
            entries = list(fresh.values())
            RNG.shuffle(entries)
            queue.extend(entries[:QUEUE_TAKE])
            return None
    return error


def fetch_wallpaper(client, state, width):
    """Download an unseen photo from the queue, listing pages only when it runs low."""
    seen = set(state["history"])
    seen.add(image_id(state["current"]))
    queue = state["queue"] = [c for c in state["queue"] if c["id"] not in seen]
    recent_authors = set(state["authors"])
    pages = topic_pages(client)
    needs_refill, last_error = len(queue) < QUEUE_LOW, None
    for _ in range(DOWNLOAD_ATTEMPTS):
        if needs_refill:
            needs_refill = False
            try:
                error = refill(client, state, queue, seen, pages)
            except NetworkError as exc:
                if not queue:
                    raise
                error, pages = exc, iter(())  # The image CDN may still be reachable.
            if error is not None:
                if queue:
                    warn(f"could not list new wallpapers: {error}; using queued photos")
                last_error = last_error or error
        if not queue:
            break
        photo = RNG.choice([c for c in queue if c["author"] not in recent_authors] or queue)
        queue.remove(photo)
        seen.add(photo["id"])
        try:
            return client.download(photo, width)
        except NetworkError:
            raise
        except WallpaperError as exc:
            if cooling_down(state):
                raise
            last_error, needs_refill = exc, not queue
    message = "no new free landscape wallpaper found"
    if last_error is not None:
        message += f"; last request failed: {last_error}"
    raise WallpaperError(message)


def download_new(client, state, width):
    """Try Unsplash, then Bing; None means the caller should rotate cached images."""
    try:
        if cooling_down(state):
            until = time.strftime("%Y-%m-%d %H:%M", time.localtime(state["retry_after"]))
            raise WallpaperError(f"Unsplash cooldown active until {until}")
        return fetch_wallpaper(client, state, width)
    except WallpaperError as exc:
        warn(f"{exc}; trying Bing daily images")
    client.start_budget()
    try:
        return client.bing()
    except WallpaperError as exc:
        warn(f"{exc}; trying cached wallpapers")
    return None


def cached_images(cache):
    """Owned, complete JPEGs (not symlinks), most recently used first."""
    images = []
    for path in cache.iterdir():
        if not IMAGE_NAME.match(path.name):
            continue
        try:
            info = path.lstat()
            if not stat.S_ISREG(info.st_mode) or info.st_size < 4:
                continue
            with path.open("rb") as stream:
                head = stream.read(3)
                stream.seek(-2, os.SEEK_END)
                tail = stream.read(2)
        except OSError:
            continue
        if valid_jpeg(head + tail):
            images.append((info.st_mtime, path))
    return [path for _, path in sorted(images, reverse=True)]


def choose_cached(cache, state):
    """Use every cached image before repeating, then rotate least recently used."""
    images = cached_images(cache)
    if not images:
        raise WallpaperError("no usable cached wallpapers; retry when the network is available")
    alternatives = [p for p in images if p.name != state["current"]] or images
    recency = {photo_id: index for index, photo_id in enumerate(state["history"])}
    unseen = [p for p in alternatives if image_id(p.name) not in recency]
    if unseen:
        return RNG.choice(unseen)
    return min(alternatives, key=lambda p: recency[image_id(p.name)])


def discard(state, path):
    """Delete an undecodable image and never download it again."""
    path.unlink(missing_ok=True)
    state["photos"].pop(path.name, None)
    state["history"] = remember(state["history"], image_id(path.name), HISTORY_LIMIT)


def cleanup(cache, state, keep, max_age, selected):
    protected = {state["current"]}
    if selected is not None:
        protected.add(selected.name)
    images = cached_images(cache)
    valid_names = {p.name for p in images}
    for path in cache.iterdir():
        if (IMAGE_NAME.match(path.name) and path.name not in valid_names
                and path.name not in protected and not path.is_symlink() and path.is_file()):
            path.unlink(missing_ok=True)
    retained = {p.name for p in images if p.name in protected}
    cutoff = time.time() - max_age * 86400
    for path in images:
        if path.name in retained:
            continue
        if len(retained) < keep and path.stat().st_mtime >= cutoff:
            retained.add(path.name)
        else:
            path.unlink(missing_ok=True)
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

    # Defer interrupts until every paused agent has been resumed; a signal in
    # this window could otherwise leave the user's wallpaper agent frozen.
    deferred = {signal.SIGINT, signal.SIGTERM, signal.SIGHUP}
    previous_mask = signal.pthread_sigmask(signal.SIG_BLOCK, deferred)
    try:
        for pid in pids:
            try:
                os.kill(pid, signal.SIGSTOP)
            except ProcessLookupError:
                continue  # Exited since pgrep; launchd will start it with the new store.
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
        signal.pthread_sigmask(signal.SIG_SETMASK, previous_mask)
        if resume_error is not None:
            raise resume_error


def set_wallpaper(path, current_space=False):
    result = wallpaper_command(
        ["/usr/bin/osascript", "-l", "JavaScript", "-", str(path),
         "apply" if current_space else "validate"], input=SET_WALLPAPER)
    if result.returncode:
        message = "macOS wallpaper update failed: " + result.stderr.strip()
        if "could not decode the image" in result.stderr:
            raise BadImageError(message)
        raise WallpaperError(message)
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
                path.unlink(missing_ok=True)
        state = load_state(cache)
        client, selected, from_cache = Unsplash(cache, state, args.timeout), None, False
        try:
            if not args.offline:
                selected = download_new(client, state, args.width)
            if selected is None:
                selected, from_cache = choose_cached(cache, state), True
            while not args.download_only:
                try:
                    set_wallpaper(selected, current_space=args.current_space)
                except BadImageError as exc:
                    # Each discard deletes a file, so this ends when the cache runs out.
                    warn(f"{exc}; discarding {selected.name}")
                    discard(state, selected)
                    selected = None
                    selected, from_cache = choose_cached(cache, state), True
                    continue
                state["current"] = selected.name
                break
            os.utime(selected, None)  # Age means last use, not filesystem access time.
            state["history"] = remember(state["history"], image_id(selected.name), HISTORY_LIMIT)
            details = state["photos"].get(selected.name, {})
            if details.get("author"):
                state["authors"] = remember(state["authors"], details["author"], AUTHOR_MEMORY)
            if not args.quiet:
                action = "Selected" if args.download_only else "Wallpaper set"
                if from_cache:
                    action += " (cached rotation)"
                print(f"{action}: {selected}")
                if details:
                    print(f"{details.get('source', 'Unsplash')}: {details.get('credit', 'Unknown')}"
                          f" — {details.get('url', ORIGIN)}")
        finally:
            # Save cooldown/cookies even after failure. Never prune the previous active image.
            # A cleanup or cookie problem must not prevent saving state.
            try:
                cleanup(cache, state, args.keep, args.max_age, selected)
            except OSError as exc:
                warn(f"cache cleanup failed: {exc}")
            save_state(cache, state)
            if not args.offline:
                try:
                    client.save_cookies()
                except OSError as exc:
                    warn(f"could not save cookies: {exc}")


def terminate(signum, _frame):
    raise Terminated(signum)


def main(argv=None):
    args = arguments(argv)
    for signum in (signal.SIGTERM, signal.SIGHUP):
        signal.signal(signum, terminate)
    try:
        run(args)
    except (WallpaperError, OSError) as exc:
        warn(str(exc))
        return 1
    except KeyboardInterrupt:
        warn("interrupted")
        return 130
    except Terminated as exc:
        warn("terminated")
        return 128 + exc.signum
    return 0


if __name__ == "__main__":
    sys.exit(main())
