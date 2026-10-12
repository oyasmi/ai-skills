"""Standard-library regression tests: python3 -m unittest discover -s tools/tests."""

import email.message
import copy
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import plistlib
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import Mock, patch

sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location("wallpaper", Path(__file__).parents[1] / "unsplash_wallpaper.py")
w = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(w)
JPEG = b"\xff\xd8\xff\xe0fixture\xff\xd9"


def photo(photo_id="free", **changes):
    result = {"id": photo_id, "width": 4000, "height": 2500, "premium": False,
              "urls": {"raw": "https://images.unsplash.com/photo-test?ixid=track"}}
    result.update(changes)
    return result


class WallpaperTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.cache = Path(self.temporary.name)
        self.state = w.load_state(self.cache)

    def image(self, name, age=0):
        path = self.cache / f"wallpaper-{name}.jpg"
        path.write_bytes(JPEG)
        timestamp = time.time() - age * 86400
        os.utime(path, (timestamp, timestamp))
        return path

    def test_only_free_high_resolution_landscapes(self):
        self.assertIsNone(w.candidate("unexpected listing entry"))
        for changes in ({}, {"plus": True}, {"premium": True}, {"width": 1000},
                        {"height": 5000}, {"width": 12000}, {"width": 4000.5}, {"id": "../escape"},
                        {"id": None}, {"urls": {}}, {"urls": {"raw": None}},
                        {"urls": {"raw": "https://plus.unsplash.com/premium_photo-x"}}):
            with self.subTest(changes=changes):
                self.assertEqual(w.candidate(photo(**changes)) is not None, not changes)

    def test_candidate_tolerates_null_metadata(self):
        entry = w.candidate(photo(user=None, links=None))
        self.assertEqual((entry["credit"], entry["author"]), ("Unknown", ""))
        self.assertEqual(entry["url"], w.ORIGIN + "/photos/free")
        entry = w.candidate(photo(user={"name": "Ann", "username": "ann"},
                                  links={"html": "javascript:alert(1)"}))
        self.assertEqual((entry["credit"], entry["author"]), ("Ann", "ann"))
        self.assertTrue(entry["url"].startswith(w.ORIGIN + "/"))

    def test_selection_avoids_recent_ids(self):
        self.state["history"] = ["recent"]
        client = Mock()
        client.page_count.return_value = 50
        client.photos.return_value = [photo("recent"), photo("new"), photo("paid", premium=True)]
        expected = self.image("new")
        client.download.return_value = expected
        self.assertEqual(w.fetch_wallpaper(client, self.state, 3840), expected)
        self.assertEqual(client.download.call_args.args[0]["id"], "new")

    def test_selection_falls_back_to_first_page(self):
        client = Mock()
        client.page_count.return_value = 608
        client.photos.side_effect = [[], [], [], [], [], [photo()]]
        with patch.object(w.RNG, "sample", return_value=[20, 40, 80, 160, 320]) as sample:
            w.fetch_wallpaper(client, self.state, 3840)
        sample.assert_called_once_with(range(1, 609), 5)
        self.assertEqual([call.args[0] for call in client.photos.call_args_list], [20, 40, 80, 160, 320, 1])
        self.assertNotIn("topic", self.state)

    def test_topic_size_is_discovered_cached_and_rounded_up(self):
        client = w.Unsplash(self.cache, self.state, 20)
        client.listing = Mock(return_value={"total_photos": 18236})
        self.assertEqual(client.page_count(), 608)
        self.assertEqual(client.page_count(), 608)
        client.listing.assert_called_once_with(w.ORIGIN + "/napi/topics/wallpapers")
        w.save_state(self.cache, self.state)
        restored = w.Unsplash(self.cache, w.load_state(self.cache), 20)
        restored.listing = Mock(side_effect=AssertionError("should use persisted size"))
        self.assertEqual(restored.page_count(), 608)

    def test_topic_size_refreshes_after_a_day(self):
        self.state["topic"] = {"total_photos": 1500, "checked_at": time.time() - 2 * w.TOPIC_REFRESH}
        client = w.Unsplash(self.cache, self.state, 20)
        client.listing = Mock(return_value={"total_photos": 18236})
        self.assertEqual(client.page_count(), 608)
        self.assertEqual(self.state["topic"]["total_photos"], 18236)

    def test_topic_size_failure_retains_known_range_or_default(self):
        client = w.Unsplash(self.cache, self.state, 20)
        for topic, pages in (({}, 50), ({"total_photos": 18236}, 608),
                             (None, 50), ({"total_photos": "invalid"}, 50)):
            for response in ({"total_photos": 0}, [], w.WallpaperError("unavailable")):
                with self.subTest(topic=topic, response=response):
                    self.state["topic"] = topic
                    if isinstance(response, Exception):
                        client.listing = Mock(side_effect=response)
                    else:
                        client.listing = Mock(return_value=response)
                    with patch.object(w, "warn"):
                        self.assertEqual(client.page_count(), pages)

    def test_topic_size_rate_limit_stops_requests(self):
        client = w.Unsplash(self.cache, self.state, 20)
        self.state["retry_after"] = time.time() + 3600
        client.listing = Mock(side_effect=w.WallpaperError("rate limited"))
        with self.assertRaises(w.WallpaperError):
            client.page_count()

    def test_selection_continues_after_listing_and_download_failures(self):
        client = Mock()
        client.page_count.return_value = 608
        client.photos.side_effect = [w.WallpaperError("HTTP 503"),
                                     [photo("broken")], [photo("new")]]
        expected = self.image("new")
        client.download.side_effect = [w.WallpaperError("bad JPEG"), expected]
        with patch.object(w.RNG, "sample", return_value=[100, 200, 300, 400, 500]):
            self.assertEqual(w.fetch_wallpaper(client, self.state, 3840), expected)
        self.assertEqual([call.args[0] for call in client.photos.call_args_list], [100, 200, 300])
        self.assertEqual([call.args[0]["id"] for call in client.download.call_args_list], ["broken", "new"])

    def test_selection_rate_limit_does_not_try_another_page(self):
        client = Mock()
        client.page_count.return_value = 608
        client.photos.return_value = [photo()]

        def limited(*args):
            self.state["retry_after"] = time.time() + 3600
            raise w.WallpaperError("rate limited")

        client.download.side_effect = limited
        with self.assertRaises(w.WallpaperError):
            w.fetch_wallpaper(client, self.state, 3840)
        client.photos.assert_called_once()
        client.download.assert_called_once()

    def test_selection_excludes_current_even_if_missing_from_history(self):
        self.state["current"] = "wallpaper-current.jpg"
        client = Mock()
        client.page_count.return_value = 1
        client.photos.return_value = [photo("current"), photo("new")]
        w.fetch_wallpaper(client, self.state, 3840)
        self.assertEqual(client.download.call_args.args[0]["id"], "new")
        client.photos.assert_called_once_with(1)

    def test_repeated_listing_does_not_retry_broken_photo(self):
        client = Mock()
        client.page_count.return_value = 10
        client.photos.return_value = [photo("broken"), photo("broken")]
        client.download.side_effect = w.WallpaperError("bad JPEG")
        with self.assertRaisesRegex(w.WallpaperError, "bad JPEG"):
            w.fetch_wallpaper(client, self.state, 3840)
        client.download.assert_called_once()

    def test_queue_avoids_listing_on_most_runs(self):
        client = Mock()
        client.page_count.return_value = 608
        client.photos.side_effect = lambda page: [photo(f"p{page}-{i}") for i in range(30)]
        client.download.side_effect = lambda photo, width: self.cache / f"wallpaper-{photo['id']}.jpg"
        for _ in range(12):
            path = w.fetch_wallpaper(client, self.state, 3840)
            self.state["history"].append(w.image_id(path.name))
        self.assertLessEqual(client.photos.call_count, 4)
        self.assertEqual(len(set(self.state["history"])), 12)
        # Queued entries survive a state round trip.
        w.save_state(self.cache, self.state)
        self.assertEqual(w.load_state(self.cache)["queue"], self.state["queue"])

    def test_queue_prefers_photographers_not_used_recently(self):
        self.state["authors"] = ["alice"]
        self.state["queue"] = [w.candidate(photo(f"a{i}", user={"username": "alice"})) for i in range(9)]
        self.state["queue"].append(w.candidate(photo("b", user={"username": "bob"})))
        client = Mock()
        w.fetch_wallpaper(client, self.state, 3840)
        self.assertEqual(client.download.call_args.args[0]["id"], "b")
        client.photos.assert_not_called()

    def test_stale_or_invalid_queue_entries_are_dropped(self):
        fresh = w.candidate(photo("fresh"))
        stale = w.candidate(photo("stale"), now=time.time() - w.QUEUE_MAX_AGE - 1)
        hostile = dict(fresh, id="hostile", raw="https://evil.example/x")
        self.state["queue"] = [fresh, stale, hostile, "junk", {}]
        w.save_state(self.cache, self.state)
        self.assertEqual(w.load_state(self.cache)["queue"], [fresh])

    def test_network_failure_stops_listing_immediately(self):
        client = Mock()
        client.page_count.return_value = 608
        client.photos.side_effect = w.NetworkError("offline")
        with self.assertRaises(w.NetworkError):
            w.fetch_wallpaper(client, self.state, 3840)
        client.photos.assert_called_once()
        client.download.assert_not_called()

    def test_listing_failure_still_uses_queued_photos(self):
        self.state["queue"] = [w.candidate(photo("queued"))]
        client = Mock()
        client.page_count.return_value = 608
        client.photos.side_effect = w.NetworkError("unsplash.com blocked")
        with patch.object(w, "warn") as warned:
            w.fetch_wallpaper(client, self.state, 3840)
        self.assertEqual(client.download.call_args.args[0]["id"], "queued")
        self.assertIn("using queued photos", warned.call_args.args[0])

    def test_download_network_failure_does_not_try_more_photos(self):
        self.state["queue"] = [w.candidate(photo(f"q{i}")) for i in range(10)]
        client = Mock()
        client.download.side_effect = w.NetworkError("offline")
        with self.assertRaises(w.NetworkError):
            w.fetch_wallpaper(client, self.state, 3840)
        client.download.assert_called_once()

    def test_read_raises_network_error_after_retries_within_budget(self):
        client = w.Unsplash(self.cache, self.state, 20)
        client.opener.open = Mock(side_effect=w.urllib.error.URLError("unreachable"))
        with patch.object(w.time, "sleep") as sleep, self.assertRaises(w.NetworkError):
            client.read(w.ORIGIN, 100)
        self.assertEqual(client.opener.open.call_count, 3)
        self.assertEqual(sleep.call_count, 2)
        client.opener.open.reset_mock()
        client.deadline = time.monotonic() - 1
        with self.assertRaisesRegex(w.NetworkError, "budget"):
            client.read(w.ORIGIN, 100)
        client.opener.open.assert_not_called()

    def test_online_sampling_reaches_older_pages_without_repeating(self):
        client = Mock()
        client.page_count.return_value = 608
        client.photos.side_effect = lambda page: [photo(f"p{page}-{i}") for i in range(30)]
        client.download.side_effect = lambda photo, width: self.cache / f"wallpaper-{photo['id']}.jpg"
        ids = []
        with patch.object(w.RNG, "sample", return_value=[100, 200, 300, 400, 500]):
            for _ in range(120):
                path = w.fetch_wallpaper(client, self.state, 3840)
                photo_id = path.stem.removeprefix("wallpaper-")
                ids.append(photo_id)
                self.state["history"].append(photo_id)
        self.assertEqual(len(set(ids)), 120)

    def test_preact_response_matches_challenge(self):
        info = {"challenge": "known data", "difficulty": 1,
                "redir": "/.within.website/x/cmd/anubis/api/pass-challenge?id=abc"}
        html = '<script type="application/json" id="preact_info">' + json.dumps(info) + '</script>'
        with patch.object(w.time, "sleep"):
            result = w.Unsplash(self.cache, self.state, 20).solve(html, w.ORIGIN)
        params = w.urllib.parse.parse_qs(w.urllib.parse.urlsplit(result).query)
        self.assertEqual(params["result"], [hashlib.sha256(b"known data").hexdigest()])
        self.assertEqual(params["id"], ["abc"])

    def test_fast_response_is_valid_proof_of_work(self):
        info = {"rules": {"algorithm": "fast", "difficulty": 2},
                "challenge": {"id": "abc", "randomData": "seed"}}
        html = '<script id="anubis_challenge">' + json.dumps(info) + '</script>'
        result = w.Unsplash(self.cache, self.state, 20).solve(html, w.ORIGIN + "/wallpapers")
        params = w.urllib.parse.parse_qs(w.urllib.parse.urlsplit(result).query)
        expected = hashlib.sha256(("seed" + params["nonce"][0]).encode()).hexdigest()
        self.assertTrue(expected.startswith("00"))
        self.assertEqual(params["response"], [expected])

    def test_unknown_and_expensive_challenges_fail(self):
        client = w.Unsplash(self.cache, self.state, 20)
        for algorithm, difficulty in (("captcha", 2), ("fast", 9)):
            info = {"rules": {"algorithm": algorithm, "difficulty": difficulty}, "challenge": {}}
            html = '<script id="anubis_challenge">' + json.dumps(info) + '</script>'
            with self.subTest(algorithm=algorithm), self.assertRaises(w.WallpaperError):
                client.solve(html, w.ORIGIN)

    def test_challenge_round_trip_to_listing(self):
        info = {"challenge": "data", "redir": "/pass?id=abc"}
        challenge = ('<script id="preact_info">' + json.dumps(info) + '</script>').encode()
        client = w.Unsplash(self.cache, self.state, 20)
        client.read = Mock(side_effect=[(401, {}, w.ORIGIN, challenge),
                                        (200, {}, w.ORIGIN, json.dumps([photo()]).encode())])
        with patch.object(w.time, "sleep"):
            self.assertEqual(client.photos(1)[0]["id"], "free")
        self.assertIn("result=", client.read.call_args_list[1].args[0])

    def test_rate_limit_persists_and_does_not_retry(self):
        client = w.Unsplash(self.cache, self.state, 20)
        response = Mock()
        response.__enter__ = Mock(return_value=response)
        response.__exit__ = Mock(return_value=False)
        response.code, response.headers, response.url = 429, {"Retry-After": "7200"}, w.ORIGIN
        client.opener.open = Mock(return_value=response)
        with self.assertRaises(w.WallpaperError):
            client.read(w.ORIGIN, 100)
        self.assertGreater(self.state["retry_after"], time.time() + 7190)
        client.opener.open.assert_called_once()
        response.read.assert_not_called()
        self.assertGreater(w.retry_time("invalid"), time.time() + 3590)
        future = w.email.utils.formatdate(time.time() + 5000, usegmt=True)
        self.assertGreater(w.retry_time(future), time.time() + 4990)
        far_future = w.email.utils.formatdate(time.time() + 365 * 86400, usegmt=True)
        self.assertLessEqual(w.retry_time(far_future), time.time() + w.MAX_COOLDOWN)
        self.assertLessEqual(w.retry_time("99999999"), time.time() + w.MAX_COOLDOWN)

    def test_image_validation_and_no_upscaling(self):
        client = w.Unsplash(self.cache, self.state, 20)
        headers = email.message.Message()
        headers["Content-Type"] = "image/jpeg"
        client.read = Mock(return_value=(200, headers, "", JPEG))
        path = client.download(w.candidate(photo(width=2000, height=1200)), 3840)
        self.assertEqual(path.read_bytes(), JPEG)
        query = w.urllib.parse.parse_qs(w.urllib.parse.urlsplit(client.read.call_args.args[0]).query)
        self.assertEqual(query["w"], ["2000"])
        self.assertEqual(query["ixid"], ["track"])
        client.read.return_value = (200, headers, "", JPEG[:-2])
        with self.assertRaises(w.WallpaperError):
            client.download(w.candidate(photo("truncated")), 3840)
        self.assertFalse((self.cache / "wallpaper-truncated.jpg").exists())

    def test_bing_prefers_unseen_daily_image(self):
        client = w.Unsplash(self.cache, self.state, 20)
        images = [{"urlbase": "/th?id=OHR.Today", "copyright": "today"},
                  {"urlbase": "/th?id=OHR.Yesterday", "copyright": "yesterday"}]
        today = "bing-" + hashlib.sha256(images[0]["urlbase"].encode()).hexdigest()[:16]
        self.state["history"] = [today]
        headers = email.message.Message()
        headers["Content-Type"] = "image/jpeg"
        client.read = Mock(side_effect=[(200, headers, "", json.dumps({"images": images}).encode()),
                                       (200, headers, "", JPEG)])
        path = client.bing()
        self.assertIn("OHR.Yesterday_UHD.jpg", client.read.call_args.args[0])
        self.assertEqual(self.state["photos"][path.name]["source"], "Bing")

    def test_bing_skips_bad_images_and_reaches_older_days(self):
        client = w.Unsplash(self.cache, self.state, 20)
        recent = [{"urlbase": f"/th?id=OHR.Day{i}"} for i in range(8)]
        older = recent[-1:] + [{"urlbase": "/th?id=OHR.Older"}, {"urlbase": "/evil?x"}]
        self.state["history"] = ["bing-" + hashlib.sha256(i["urlbase"].encode()).hexdigest()[:16]
                                 for i in recent[:-1]]
        headers = email.message.Message()
        headers["Content-Type"] = "image/jpeg"
        client.read = Mock(side_effect=[(200, headers, "", json.dumps({"images": recent}).encode()),
                                       (200, headers, "", JPEG[:-2]),
                                       (200, headers, "", json.dumps({"images": older}).encode()),
                                       (200, headers, "", JPEG)])
        path = client.bing()
        urls = [call.args[0] for call in client.read.call_args_list]
        self.assertIn("idx=7", urls[2])
        self.assertIn("OHR.Older_UHD.jpg", urls[3])
        self.assertEqual(self.state["photos"][path.name]["credit"], "Bing daily image")

    def test_cleanup_keeps_current_and_selected_within_limit(self):
        current, selected = self.image("current", age=100), self.image("selected")
        self.state["current"] = current.name
        for i in range(10):
            self.image(str(i), age=i + 1)
        unrelated = self.cache / "family.jpg"
        unrelated.write_bytes(JPEG)
        w.cleanup(self.cache, self.state, 2, 30, selected)
        self.assertEqual(set(w.cached_images(self.cache)), {current, selected})
        self.assertTrue(unrelated.exists())

    def test_cleanup_removes_old_images_and_ignores_symlinks(self):
        selected, old = self.image("selected"), self.image("old", age=40)
        target = self.cache / "external.jpg"
        target.write_bytes(JPEG)
        link = self.cache / "wallpaper-link.jpg"
        link.symlink_to(target)
        w.cleanup(self.cache, self.state, 8, 30, selected)
        self.assertFalse(old.exists())
        self.assertTrue(link.is_symlink())
        self.assertTrue(target.exists())

    def test_cleanup_discards_corrupt_owned_images_even_without_selection(self):
        corrupt = self.cache / "wallpaper-corrupt.jpg"
        corrupt.write_bytes(b"partial")
        protected = self.cache / "wallpaper-current.jpg"
        protected.write_bytes(b"partial")
        self.state["current"] = protected.name
        w.cleanup(self.cache, self.state, 8, 30, None)
        self.assertFalse(corrupt.exists())
        self.assertTrue(protected.exists())

    def test_atomic_write_retains_old_file_on_failure(self):
        path = self.cache / "state.json"
        path.write_text("old")
        with self.assertRaises(RuntimeError):
            with w.atomic_file(path) as (stream, _):
                stream.write("partial")
                raise RuntimeError("interrupted")
        self.assertEqual(path.read_text(), "old")
        self.assertEqual(list(self.cache.glob(".*.tmp")), [])

    def test_offline_never_requests_network(self):
        selected = self.image("cached")
        args = w.arguments(["--cache-dir", str(self.cache), "--offline", "--download-only", "--quiet"])
        with patch.object(w.Unsplash, "read", side_effect=AssertionError("network")):
            w.run(args)
        self.assertEqual(w.load_state(self.cache)["history"], ["cached"])
        self.assertTrue(selected.exists())

    def test_cache_prefers_unused_then_least_recently_used(self):
        current, oldest, newest, unused = [self.image(name) for name in ("current", "oldest", "newest", "unused")]
        self.state.update(current=current.name, history=["oldest", "newest", "current"])
        self.assertEqual(w.choose_cached(self.cache, self.state), unused)
        self.state["history"].append("unused")
        self.assertEqual(w.choose_cached(self.cache, self.state), oldest)

    def test_cache_rotation_uses_all_six_images_before_repeating(self):
        initial = copy.deepcopy(self.state)
        for offline in (True, False):
            with self.subTest(offline=offline):
                w.save_state(self.cache, initial)
                for i in range(6):
                    self.image(str(i))
                argv = ["--cache-dir", str(self.cache), "--download-only", "--quiet"]
                if offline:
                    argv.append("--offline")
                args, ids = w.arguments(argv), []
                with patch.object(w.Unsplash, "read", side_effect=AssertionError("network")), \
                        patch.object(w, "fetch_wallpaper", side_effect=w.WallpaperError("blocked")), \
                        patch.object(w.Unsplash, "bing", side_effect=w.WallpaperError("exhausted")), \
                        patch.object(w, "warn"):
                    for _ in range(18):
                        w.run(args)
                        ids.append(w.load_state(self.cache)["history"][-1])
                self.assertEqual(len(set(ids[:6])), 6)
                self.assertEqual(ids[:6], ids[6:12])
                self.assertEqual(ids[:6], ids[12:])

    def test_cache_rotation_reports_cached_selection(self):
        self.image("cached")
        args = w.arguments(["--cache-dir", str(self.cache), "--offline", "--download-only"])
        with patch("builtins.print") as output:
            w.run(args)
        self.assertIn("cached rotation", output.call_args_list[0].args[0])

    def test_unsplash_failure_uses_bing_and_saves_cooldown(self):
        selected = self.image("bing-example")
        self.state["retry_after"] = time.time() + 3600
        w.save_state(self.cache, self.state)
        args = w.arguments(["--cache-dir", str(self.cache), "--download-only", "--quiet"])
        with patch.object(w, "fetch_wallpaper") as fetch, patch.object(w.Unsplash, "bing", return_value=selected) as bing:
            with patch.object(w, "warn"):
                w.run(args)
            fetch.assert_not_called()
            bing.assert_called_once()
        self.assertGreater(w.load_state(self.cache)["retry_after"], time.time())

    def test_both_services_fail_then_cache_is_used(self):
        self.image("offline")
        args = w.arguments(["--cache-dir", str(self.cache), "--download-only", "--quiet"])
        with patch.object(w, "fetch_wallpaper", side_effect=w.WallpaperError("blocked")), \
                patch.object(w.Unsplash, "bing", side_effect=w.WallpaperError("offline")), \
                patch.object(w, "warn"):
            w.run(args)
        self.assertEqual(w.load_state(self.cache)["history"], ["offline"])

    def test_apply_failure_preserves_previous_wallpaper(self):
        current, selected = self.image("current", 100), self.image("new")
        self.state["current"] = current.name
        w.save_state(self.cache, self.state)
        args = w.arguments(["--cache-dir", str(self.cache), "--keep", "2", "--quiet"])
        with patch.object(w.sys, "platform", "darwin"), \
                patch.object(w, "fetch_wallpaper", return_value=selected), \
                patch.object(w, "set_wallpaper", side_effect=w.WallpaperError("failed")):
            with self.assertRaises(w.WallpaperError):
                w.run(args)
        self.assertEqual(w.load_state(self.cache)["current"], current.name)
        self.assertTrue(current.exists())
        self.assertTrue(selected.exists())

    def test_undecodable_image_is_discarded_and_cache_used(self):
        good, bad = self.image("good", age=1), self.image("bad")
        args = w.arguments(["--cache-dir", str(self.cache), "--quiet"])

        def apply(path, current_space):
            if path == bad:
                raise w.BadImageError("macOS could not decode the image")

        with patch.object(w.sys, "platform", "darwin"), \
                patch.object(w, "download_new", return_value=bad), \
                patch.object(w, "set_wallpaper", side_effect=apply), patch.object(w, "warn"):
            w.run(args)
        state = w.load_state(self.cache)
        self.assertEqual(state["current"], good.name)
        self.assertFalse(bad.exists())
        self.assertIn("bad", state["history"])

    def test_bad_image_error_is_distinguished(self):
        with patch.object(w.subprocess, "run", return_value=Mock(
                returncode=1, stderr="Error: macOS could not decode the image (-2700)")), \
                self.assertRaises(w.BadImageError):
            w.set_wallpaper(self.cache / "wallpaper-x.jpg")

    def test_history_and_authors_are_bounded(self):
        self.state["history"] = [str(i) for i in range(w.HISTORY_LIMIT + 50)]
        self.state["authors"] = [f"a{i}" for i in range(50)]
        self.state["retry_after"] = time.time() + 10 * w.MAX_COOLDOWN
        w.save_state(self.cache, self.state)
        state = w.load_state(self.cache)
        self.assertEqual(len(state["history"]), w.HISTORY_LIMIT)
        self.assertEqual(state["history"][-1], str(w.HISTORY_LIMIT + 49))
        self.assertEqual(len(state["authors"]), w.AUTHOR_MEMORY)
        self.assertLessEqual(state["retry_after"], time.time() + w.MAX_COOLDOWN)
        self.assertEqual(w.remember(["a", "b", "c"], "a", 3), ["b", "c", "a"])
        self.assertEqual(w.remember(["a", "b", "c"], "d", 3), ["b", "c", "d"])

    def test_run_records_photographer(self):
        selected = self.image("new")
        self.state["photos"][selected.name] = {"source": "Unsplash", "credit": "Ann", "author": "ann",
                                               "url": w.ORIGIN}
        w.save_state(self.cache, self.state)
        args = w.arguments(["--cache-dir", str(self.cache), "--download-only", "--quiet"])
        with patch.object(w, "download_new", return_value=selected):
            w.run(args)
        self.assertEqual(w.load_state(self.cache)["authors"], ["ann"])

    def test_overlapping_run_is_a_noop(self):
        args = w.arguments(["--cache-dir", str(self.cache), "--quiet"])
        with patch.object(w.sys, "platform", "darwin"), (self.cache / ".lock").open("a") as held:
            fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with patch.object(w, "load_state", side_effect=AssertionError("should skip")):
                w.run(args)

    def test_invalid_state_is_quarantined_and_newest_image_protected(self):
        older, newest = self.image("older", age=2), self.image("newest")
        for content in ("{}", "not json", '{"version": 1, "photos": [], "history": []}'):
            with self.subTest(content=content):
                (self.cache / "state.json").write_text(content)
                with patch.object(w, "warn") as warned:
                    state = w.load_state(self.cache)
                warned.assert_called_once()
                self.assertEqual(state["current"], newest.name)
                self.assertEqual(state["history"], [])
                self.assertEqual((self.cache / "state.json.corrupt").read_text(), content)
                self.assertFalse((self.cache / "state.json").exists())
        self.assertTrue(older.exists())

    def test_invalid_cli_arguments(self):
        with patch.object(w.sys, "stderr"), self.assertRaises(SystemExit) as result:
            w.arguments(["--keep", "1"])
        self.assertEqual(result.exception.code, 2)

    def test_osascript_receives_path_as_argument_and_has_timeout(self):
        path = self.cache / 'quotes"and spaces.jpg'
        with patch.object(w.subprocess, "run", return_value=Mock(returncode=0)) as run, \
                patch.object(w, "set_all_spaces") as all_spaces:
            w.set_wallpaper(path)
        all_spaces.assert_called_once_with(path)
        self.assertEqual(run.call_args.args[0][-2:], [str(path), "validate"])
        self.assertNotIn(str(path), run.call_args.kwargs["input"])
        self.assertEqual(run.call_args.kwargs["timeout"], 30)
        with patch.object(w.subprocess, "run", side_effect=subprocess.TimeoutExpired("osascript", 30)):
            with self.assertRaises(w.WallpaperError):
                w.set_wallpaper(path)

    def test_terminate_signal_raises_after_cleanup(self):
        previous = signal.signal(signal.SIGTERM, w.terminate)
        self.addCleanup(signal.signal, signal.SIGTERM, previous)
        with patch.object(w, "run", side_effect=lambda args: signal.raise_signal(signal.SIGTERM)), \
                patch.object(w, "warn"):
            self.assertEqual(w.main(["--cache-dir", str(self.cache)]), 128 + signal.SIGTERM)


class AllSpacesTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.home = Path(self.temporary.name)
        self.store_path = self.home / "Library/Application Support/com.apple.wallpaper/Store/Index.plist"
        self.store_path.parent.mkdir(parents=True)
        self.path = self.home / 'wallpaper quotes"中文 #%.jpg'
        self.idle = {"Content": {"Choices": [{"Provider": "screensaver", "Configuration": b"keep"}]}}
        slot = {"Type": "individual", "Desktop": {"old": True}, "Idle": self.idle, "extra": 42}
        self.store = {
            "AllSpacesAndDisplays": {"Type": "idle", "Idle": self.idle},
            "SystemDefault": copy.deepcopy(slot),
            "Displays": {"display-1": copy.deepcopy(slot)},
            "Spaces": {
                "space-1": {"Default": copy.deepcopy(slot),
                            "Displays": {"display-1": copy.deepcopy(slot), "display-2": "$null"},
                            "extra": "keep"},
                "space-2": {"Default": {"Type": "linked", "Desktop": self.idle}},
            },
            "unknown": b"keep",
        }
        self.original = plistlib.dumps(self.store, fmt=plistlib.FMT_BINARY)
        self.store_path.write_bytes(self.original)
        self.store_path.chmod(0o640)

    def test_updates_all_slots_and_defaults_preserving_screensavers(self):
        original = copy.deepcopy(self.store)
        updated = w.wallpaper_store_for_image(self.store, self.path)
        slots = [updated["AllSpacesAndDisplays"], updated["SystemDefault"],
                 updated["Displays"]["display-1"], updated["Spaces"]["space-1"]["Default"],
                 *updated["Spaces"]["space-1"]["Displays"].values(),
                 updated["Spaces"]["space-2"]["Default"]]
        for slot in slots:
            choice = slot["Desktop"]["Content"]["Choices"][0]
            self.assertEqual(choice["Provider"], "com.apple.wallpaper.choice.image")
            self.assertEqual(choice["Files"], [{"relative": self.path.resolve().as_uri()}])
            configuration = plistlib.loads(choice["Configuration"])
            self.assertEqual(configuration["placement"], 1)
            # Required by macOS 15's image provider. A placement-only plist
            # passed the old tests but failed native decoding with error 4865.
            background = configuration["backgroundColor"]
            self.assertEqual(plistlib.loads(background["colorSpace"]), "kCGColorSpaceGenericRGB")
            self.assertEqual(background["components"], [0.0, 0.0, 0.0, 1.0])
            self.assertEqual(slot["Desktop"]["Content"]["Shuffle"], "$null")
            if "Idle" in slot:
                self.assertEqual(slot["Idle"], self.idle)
                self.assertEqual(slot["Type"], "individual")
            else:
                self.assertEqual(slot["Type"], "desktop")
        self.assertEqual(updated["SystemDefault"]["extra"], 42)
        self.assertEqual(updated["Spaces"]["space-1"]["extra"], "keep")
        self.assertEqual(updated["unknown"], b"keep")
        self.assertEqual(self.store, original)

    def test_null_global_and_empty_overrides_get_defaults(self):
        self.store.update(AllSpacesAndDisplays="$null", Spaces={}, Displays={})
        updated = w.wallpaper_store_for_image(self.store, self.path)
        self.assertEqual(updated["AllSpacesAndDisplays"]["Type"], "desktop")
        self.assertIn("Desktop", updated["SystemDefault"])

    def test_unknown_schema_is_rejected(self):
        for changes in ({"Spaces": []}, {"Displays": []}, {"SystemDefault": {"Type": "new-schema"}},
                        {"Spaces": {"space": []}}):
            with self.subTest(changes=changes), self.assertRaises(w.WallpaperError):
                w.wallpaper_store_for_image(dict(self.store, **changes), self.path)

    def apply(self, kill):
        with patch.object(w.Path, "home", return_value=self.home), \
                patch.object(w.subprocess, "run", return_value=Mock(returncode=0, stdout="123\n456\n")) as run, \
                patch.object(w.os, "kill", side_effect=kill):
            w.set_all_spaces(self.path)
        self.assertEqual(run.call_args.args[0],
                         ["/usr/bin/pgrep", "-u", str(os.getuid()), "-x", "WallpaperAgent"])

    def test_pauses_agents_before_write_then_restarts_and_resumes(self):
        events = []

        def kill(pid, sig):
            if sig == signal.SIGSTOP:
                self.assertEqual(self.store_path.read_bytes(), self.original)
            if sig == signal.SIGTERM:
                updated = plistlib.loads(self.store_path.read_bytes())
                self.assertIn("Desktop", updated["AllSpacesAndDisplays"])
            events.append((pid, sig))

        self.apply(kill)
        self.assertEqual(events, [(123, signal.SIGSTOP), (456, signal.SIGSTOP),
                                  (123, signal.SIGTERM), (456, signal.SIGTERM),
                                  (123, signal.SIGCONT), (456, signal.SIGCONT)])
        self.assertEqual(self.store_path.stat().st_mode & 0o777, 0o640)
        self.assertEqual(list(self.store_path.parent.glob(".*.tmp")), [])

    def test_invalid_store_is_untouched_and_agents_are_resumed(self):
        self.store_path.write_bytes(b"invalid plist")
        kill = Mock()
        with self.assertRaises(w.WallpaperError):
            self.apply(kill)
        self.assertEqual(self.store_path.read_bytes(), b"invalid plist")
        self.assertEqual([call.args[1] for call in kill.call_args_list],
                         [signal.SIGSTOP, signal.SIGSTOP, signal.SIGCONT, signal.SIGCONT])

    def test_restart_failure_restores_store_and_resumes_agents(self):
        def fail_restart(pid, sig):
            if sig == signal.SIGTERM:
                raise PermissionError("cannot restart")

        kill = Mock(side_effect=fail_restart)
        with self.assertRaises(PermissionError):
            self.apply(kill)
        self.assertEqual(self.store_path.read_bytes(), self.original)
        self.assertEqual([call.args for call in kill.call_args_list][-2:],
                         [(123, signal.SIGCONT), (456, signal.SIGCONT)])

    def test_partial_pause_failure_resumes_already_stopped_agent(self):
        def fail_pause(pid, sig):
            if pid == 456 and sig == signal.SIGSTOP:
                raise PermissionError("cannot pause")

        kill = Mock(side_effect=fail_pause)
        with self.assertRaises(PermissionError):
            self.apply(kill)
        self.assertEqual(self.store_path.read_bytes(), self.original)
        self.assertEqual(kill.call_args.args, (123, signal.SIGCONT))

    def test_write_failure_keeps_store_and_resumes_agents(self):
        kill = Mock()
        with patch.object(w, "atomic_file", side_effect=OSError("disk full")), \
                self.assertRaises(OSError):
            self.apply(kill)
        self.assertEqual(self.store_path.read_bytes(), self.original)
        self.assertEqual([call.args[1] for call in kill.call_args_list],
                         [signal.SIGSTOP, signal.SIGSTOP, signal.SIGCONT, signal.SIGCONT])

    def test_resume_failure_still_attempts_to_resume_remaining_agents(self):
        def fail_resume(pid, sig):
            if pid == 123 and sig == signal.SIGCONT:
                raise PermissionError("cannot resume")

        kill = Mock(side_effect=fail_resume)
        with self.assertRaises(PermissionError):
            self.apply(kill)
        self.assertEqual(kill.call_args.args, (456, signal.SIGCONT))

    def test_termination_is_deferred_until_agents_resume(self):
        previous = signal.signal(signal.SIGTERM, w.terminate)
        self.addCleanup(signal.signal, signal.SIGTERM, previous)
        events = []

        def kill(pid, sig):
            events.append((pid, sig))
            if (pid, sig) == (123, signal.SIGSTOP):
                signal.raise_signal(signal.SIGTERM)  # Pending until the mask is restored.

        with self.assertRaises(w.Terminated):
            self.apply(kill)
        self.assertEqual(events[-2:], [(123, signal.SIGCONT), (456, signal.SIGCONT)])
        self.assertIn("Desktop", plistlib.loads(self.store_path.read_bytes())["AllSpacesAndDisplays"])

    def test_vanished_agent_is_skipped(self):
        def kill(pid, sig):
            if pid == 123:
                raise ProcessLookupError

        self.apply(Mock(side_effect=kill))
        self.assertIn("Desktop", plistlib.loads(self.store_path.read_bytes())["AllSpacesAndDisplays"])

    def test_xml_store_format_is_preserved(self):
        self.store_path.write_bytes(plistlib.dumps(self.store, fmt=plistlib.FMT_XML))
        self.apply(Mock())
        self.assertTrue(self.store_path.read_bytes().startswith(b"<?xml"))

    def test_unavailable_agent_leaves_store_untouched(self):
        with patch.object(w.Path, "home", return_value=self.home), \
                patch.object(w.subprocess, "run", return_value=Mock(returncode=1, stdout="")), \
                patch.object(w.os, "kill") as kill, self.assertRaises(w.WallpaperError):
            w.set_all_spaces(self.path)
        kill.assert_not_called()
        self.assertEqual(self.store_path.read_bytes(), self.original)

    def test_missing_store_fails_without_silent_current_space_fallback(self):
        self.store_path.unlink()
        with patch.object(w.Path, "home", return_value=self.home), \
                patch.object(w.subprocess, "run") as run, self.assertRaises(w.WallpaperError):
            w.set_all_spaces(self.path)
        run.assert_not_called()

    def test_current_space_uses_appkit_without_editing_store(self):
        self.assertFalse(w.arguments([]).current_space)
        self.assertTrue(w.arguments(["--current-space"]).current_space)
        with patch.object(w.subprocess, "run", return_value=Mock(returncode=0)) as run, \
                patch.object(w, "set_all_spaces") as all_spaces:
            w.set_wallpaper(self.path, current_space=True)
        self.assertEqual(run.call_args.args[0][-1], "apply")
        all_spaces.assert_not_called()

    def test_invalid_image_never_edits_store(self):
        with patch.object(w.subprocess, "run", return_value=Mock(returncode=1, stderr="invalid image")), \
                patch.object(w, "set_all_spaces") as all_spaces, self.assertRaises(w.WallpaperError):
            w.set_wallpaper(self.path)
        all_spaces.assert_not_called()


if __name__ == "__main__":
    unittest.main()
