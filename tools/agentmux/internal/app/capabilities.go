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
		"version",
	}
}

func featureNames() []string {
	return []string{
		// `run` performs summon + prompt + wait + capture in one call.
		"run",
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
		// Stopped instances stay queryable as tombstones, and `list --all`
		// shows them.
		"tombstones",
	}
}
