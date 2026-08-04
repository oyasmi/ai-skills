package app

// The capability manifest exists so an orchestrating agent can tell what this
// binary supports without probing commands and interpreting failures. Every
// entry here is a promise about observable behaviour; add one when behaviour a
// caller can branch on changes, not on every release.

func commandNames() []string {
	return []string{
		"template list",
		"list",
		"summon",
		"run",
		"inspect",
		"prompt",
		"capture",
		"wait",
		"attach",
		"halt",
		"doctor",
		"version",
	}
}

func featureNames() []string {
	return []string{
		// `run` performs summon + prompt + wait + capture in one call.
		"run",
		// `doctor` checks binary/PATH/config/templates/tmux in one pass.
		"doctor",
		// `version --json` reports build_time and binary_path so a caller can
		// detect a stale binary shadowing a fresh install.
		"version-provenance",
		// `run` on a busy, reused instance waits inside its own --timeout
		// budget instead of failing or racing into unrelated work; the wait
		// is reported as data.queued_ms. `prompt --wait-if-busy` exposes the
		// same wait standalone.
		"run-wait-if-busy",
		// `wait` accepts several instance names plus --mode any|all.
		"wait-multi",
		// Reaching --timeout returns ok with data.timed_out instead of failing.
		"wait-timeout-ok",
		// `wait` reports data.saw_busy and data.elapsed_ms.
		"wait-observability",
		// A prompt confirms that a TUI harness started before returning.
		"prompt-ack",
		// `capture --since <cursor>` reads only what is new, and every
		// structured capture returns data.next_cursor.
		"capture-since",
		// `capture --raw` restores full protocol payloads; without it messages
		// are projected and bounded.
		"capture-raw",
		// A structured capture's data.messages trace is opt-in: it appears
		// only with --trace, --raw, an explicit --history, or --since. The
		// default response carries content and the harness's status fields,
		// not a copy of every protocol event.
		"lean-capture",
		// `capture --new` reads from the instance's own remembered position
		// and advances it, so watching a long run does not require the
		// caller to carry a --since cursor across calls by hand.
		"capture-new-cursor",
		// `run --detach` sends the prompt and returns immediately (still
		// honoring a busy-wait first); `wait --collect` reads back a lean
		// capture for every instance that finished. Together a parallel
		// fan-out is summon x N with --detach, then one wait --mode any
		// --collect, instead of one capture call per shard.
		"run-detach",
		"wait-collect",
		// `summon` and `run` report data.warnings with "cwd_shared:<name>"
		// when another active instance already points at the same cwd.
		// Non-blocking: it does not stop the summon.
		"cwd-shared-warning",
		// Stopped instances stay queryable as tombstones, and `list --all`
		// shows them.
		"tombstones",
		// A template's `model:` and `effort:` are turned into whatever flags the
		// harness actually accepts, so a role's strength is portable across
		// harnesses instead of needing a hand-written $MODEL placeholder.
		// `summon`/`run --effort <level>` override it per call.
		"role-effort",
	}
}
