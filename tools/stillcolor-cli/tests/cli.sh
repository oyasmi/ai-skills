#!/bin/sh
set -eu

cli=$1
tmp=$(mktemp -d)
watch_pid=
cleanup() {
    if [ -n "$watch_pid" ]; then
        kill "$watch_pid" 2>/dev/null || :
        wait "$watch_pid" 2>/dev/null || :
    fi
    rm -rf "$tmp"
}
trap cleanup EXIT

export HOME="$tmp/user with spaces"
export FAKE_DISPLAY="$tmp/display"
mkdir "$HOME"
printf 'on\n' > "$FAKE_DISPLAY"
settings="$HOME/Library/Application Support/stillcolor-cli/dithering"

expect_exit() {
    expected=$1
    shift
    code=0
    "$@" > "$tmp/out" 2> "$tmp/err" || code=$?
    if [ "$code" -ne "$expected" ]; then
        printf 'Expected exit %s, got %s from %s\n' "$expected" "$code" "$*" >&2
        cat "$tmp/out" "$tmp/err" >&2
        exit 1
    fi
}

wait_for_log() {
    attempts=0
    until grep -qx "$1" "$tmp/watch.log"; do
        if [ "$attempts" -ge 100 ]; then
            cat "$tmp/watch.log" >&2
            exit 1
        fi
        attempts=$((attempts + 1))
        sleep 0.05
    done
}

expect_exit 0 "$cli" --help
expect_exit 2 "$cli" toggle
expect_exit 2 "$cli" set off
expect_exit 2 "$cli" watch extra
expect_exit 0 "$cli" status
grep -qx 'Dithering policy: off' "$tmp/out"
grep -qx 'fake enableDither=on' "$tmp/out"
test "$(cat "$FAKE_DISPLAY")" = on

expect_exit 0 "$cli" apply
test "$(cat "$FAKE_DISPLAY")" = off
test ! -e "$settings"
expect_exit 0 "$cli" on
test "$(cat "$settings")" = on
test "$(cat "$FAKE_DISPLAY")" = on
ln "$settings" "$tmp/saved-on"
expect_exit 0 "$cli" on
test "$settings" -ef "$tmp/saved-on"
printf 'off\n' > "$FAKE_DISPLAY"
expect_exit 0 "$cli" on
test "$settings" -ef "$tmp/saved-on"
test "$(cat "$FAKE_DISPLAY")" = on

expect_exit 1 env FAKE_DISPLAY_FAIL=1 "$cli" off
grep -qx 'simulated display failure' "$tmp/err"
test "$(cat "$settings")" = off
test "$(cat "$FAKE_DISPLAY")" = on
ln "$settings" "$tmp/saved-off"
expect_exit 0 "$cli" off
test "$settings" -ef "$tmp/saved-off"
test "$(cat "$FAKE_DISPLAY")" = off
expect_exit 0 "$cli" off
test "$settings" -ef "$tmp/saved-off"
printf 'on\n' > "$FAKE_DISPLAY"

"$cli" watch > "$tmp/watch.log" 2>&1 &
watch_pid=$!
wait_for_log ready
test "$(cat "$FAKE_DISPLAY")" = off
expect_exit 0 "$cli" on
printf 'off\n' > "$FAKE_DISPLAY"
kill -USR1 "$watch_pid"
wait_for_log reconciled
test "$(cat "$FAKE_DISPLAY")" = on
kill -TERM "$watch_pid"
wait "$watch_pid"
watch_pid=
test "$(cat "$FAKE_DISPLAY")" = on

printf 'off\nextra\n' > "$settings"
expect_exit 1 "$cli" apply
test "$(cat "$FAKE_DISPLAY")" = on
expect_exit 0 "$cli" off
test "$(cat "$settings")" = off
test "$(cat "$FAKE_DISPLAY")" = off

printf 'CLI tests passed\n'
