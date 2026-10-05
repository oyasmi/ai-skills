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
        self.assertFalse(w.eligible("unexpected listing entry"))
        for changes in ({}, {"plus": True}, {"premium": True}, {"width": 1000},
                        {"height": 5000}, {"id": "../escape"}, {"urls": {}},
                        {"urls": {"raw": None}},
                        {"urls": {"raw": "https://plus.unsplash.com/premium_photo-x"}}):
            with self.subTest(changes=changes):
                self.assertEqual(w.eligible(photo(**changes)), not changes)

    def test_selection_avoids_recent_ids(self):
        self.state["history"] = ["recent"]
        client = Mock()
        client.photos.return_value = [photo("recent"), photo("new"), photo("paid", premium=True)]
        expected = self.image("new")
        client.download.return_value = expected
        self.assertEqual(w.fetch_wallpaper(client, self.state, 3840), expected)
        self.assertEqual(client.download.call_args.args[0]["id"], "new")

    def test_selection_falls_back_to_first_page(self):
        client = Mock()
        client.photos.side_effect = [[], [], [photo()]]
        with patch.object(w.RNG, "sample", return_value=[20, 40]):
            w.fetch_wallpaper(client, self.state, 3840)
        self.assertEqual([call.args[0] for call in client.photos.call_args_list], [20, 40, 1])

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

    def test_image_validation_and_no_upscaling(self):
        client = w.Unsplash(self.cache, self.state, 20)
        headers = email.message.Message()
        headers["Content-Type"] = "image/jpeg"
        client.read = Mock(return_value=(200, headers, "", JPEG))
        path = client.download(photo(width=2000), 3840)
        self.assertEqual(path.read_bytes(), JPEG)
        query = w.urllib.parse.parse_qs(w.urllib.parse.urlsplit(client.read.call_args.args[0]).query)
        self.assertEqual(query["w"], ["2000"])
        self.assertEqual(query["ixid"], ["track"])
        client.read.return_value = (200, headers, "", JPEG[:-2])
        with self.assertRaises(w.WallpaperError):
            client.download(photo("truncated"), 3840)
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

    def test_overlapping_run_is_a_noop(self):
        args = w.arguments(["--cache-dir", str(self.cache), "--quiet"])
        with (self.cache / ".lock").open("a") as held:
            fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with patch.object(w, "load_state", side_effect=AssertionError("should skip")):
                w.run(args)

    def test_invalid_state_and_cli_arguments(self):
        (self.cache / "state.json").write_text("{}")
        with self.assertRaises(w.WallpaperError):
            w.load_state(self.cache)
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
