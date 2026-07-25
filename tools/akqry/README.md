# akqry

`akqry` is a small, auditable CLI for discovering and calling public AkShare data interfaces. It preserves the returned table unless the caller explicitly selects columns, writes complete datasets to an artifact, and records query provenance in a sidecar JSON file.

## Install

```bash
uv tool install --editable 'tools/akqry[parquet]'
akqry doctor --json
```

`doctor` reports the AkShare version and module path, whether a documentation checkout is present, which output formats can be written, and the active cache directory. Reinstall after pulling changes unless the install is editable.

To test an unreleased AkShare checkout while keeping the CLI's isolated dependencies, point the CLI at the checkout root:

```bash
AKQRY_AKSHARE_PATH=/path/to/akshare akqry describe stock_zh_a_hist --json
```

## Discovery

```bash
akqry search '历史行情' --domain a-share --json
akqry describe stock_zh_a_hist --json
akqry describe stock_zh_a_hist --probe --arg symbol=000001 --arg start_date=20250101 --arg end_date=20250110 --json
```

`search` matches Chinese queries through character bigrams and a synonym table, because AkShare's own wording rarely matches the wording of a question — 每日行情 versus 历史行情. Results are ranked by how many query terms a record covers, then by weight, and each result carries the description, data source and URL parsed from the docstring, so no documentation checkout is required. `--full` returns complete records; `match_reasons` explains every hit.

`describe` reports the runtime signature plus, per parameter, whether it is required, its default, and the values the docstring documents. `--probe` calls the interface once and reports the real columns, dtypes, row count and date range — use it to learn column names before committing to `--require-columns`.

## Retrieval

```bash
akqry fetch fund_etf_hist_em \
  --arg symbol=513500 \
  --arg start_date=20250101 \
  --arg end_date=20251231 \
  --require-columns 日期,收盘,成交量 \
  --output /tmp/513500.parquet \
  --json
```

Several symbols in one process, which pays the AkShare import once:

```bash
akqry fetch stock_zh_a_hist \
  --for-each symbol=000001,600519,300750 \
  --arg start_date=20250101 --arg end_date=20250630 --arg adjust=qfq \
  --output '/tmp/a/{}.parquet' --json
```

`{}` is required in a batch `--output` and is replaced by the value. Each artifact gets its own sidecar. One failing value does not discard the others: the envelope reports `partial_failure` and lists what succeeded.

Reuse identical queries while iterating on an analysis, instead of hitting a throttled endpoint again:

```bash
akqry fetch stock_zh_a_hist --arg symbol=000001 --cache-dir ~/.cache/akqry --cache-ttl 3600 ...
```

A served entry keeps the timestamp of the *original* retrieval and is marked `cache.hit: true` with `served_at_utc`, so a cache hit can never be reported as fresh data. `AKQRY_CACHE_DIR` sets the directory globally; a negative TTL never expires.

## Data contract

- `--arg symbol=000001` remains a string because the target signature declares `symbol` as `str`.
- Values that contradict a documented `choice of {...}` set are rejected **before** the network call; pass `--allow-unknown-values` to downgrade that to a warning when the docstring is stale.
- No automatic adjustment, unit conversion, currency conversion or filling is applied.
- Parquet is preferred for typed artifacts; JSONL preserves leading-zero codes without optional dependencies. CSV is for interoperability and must be read with the sidecar schema in mind. A missing parquet engine fails before the query, not after it.
- `--require-columns` is the caller's schema-drift guard; `schema_fingerprint` in the sidecar detects drift after the fact.
- `fetch` refuses an empty result, and refuses to overwrite an existing artifact or sidecar, unless told otherwise. `--no-sidecar` deletes a sidecar that would otherwise misdescribe overwritten data.
- Data calls run in a worker process. `--timeout` is the per-call budget (a batch multiplies it by the number of calls) and retryable transport failures are retried up to `--retries` times. The worker reports incrementally, so a batch that times out keeps what already completed.

## Error codes

Every failure is a stable `code` with a fixed exit status, and `details` carries what is needed to recover.

| code | exit | recover by |
| --- | --- | --- |
| `usage_error` | 2 | Fix the flags; `details` names the offending argument. |
| `dependency_missing` / `akshare_import_failed` | 3 | Install the missing engine or dependency named in `details.remedy`. |
| `function_not_found` | 4 | `akqry search` for the current name; interfaces are renamed between versions. |
| `unsafe_callable` | 4 | Choose a data interface; state and token setters are excluded. |
| `invalid_parameter` / `missing_parameter` | 4 | Read `details.allowed_values`, `accepted_parameters` or `signature`. |
| `upstream_error` / `query_timeout` | 5 | Read `details.exception_message`; retry, narrow the range, or switch data source. |
| `empty_result` | 6 | Check the code format, the date range and whether the market was open. |
| `missing_required_columns` | 6 | Re-run using `details.available_columns`. |
| `partial_failure` | 6 | Only some batch values failed; `details.failed_labels` lists them. |
| `output_exists` / `write_failed` / `serialization_failed` | 7 | Choose another path, pass `--overwrite`, or create the parent directory. |

## Development

```bash
uv run --extra dev pytest
uv run --extra dev ruff check .
uv build
```
