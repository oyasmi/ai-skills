# akqry

`akqry` is a small, auditable CLI for discovering and calling public AkShare data interfaces. It preserves the returned table unless the caller explicitly selects columns, writes complete datasets to an artifact, and records query provenance in a sidecar JSON file.

## Install

```bash
cd tools/akqry
uv tool install --editable '.[parquet]'
akqry doctor --json
```

To test an unreleased AkShare checkout while keeping the CLI's isolated dependencies, point the CLI at the checkout root:

```bash
AKQRY_AKSHARE_PATH=/path/to/akshare akqry describe stock_zh_a_hist --json
```

## Core commands

```bash
akqry search 'ETF 历史行情' --domain etf --json
akqry describe fund_etf_hist_em --json

akqry fetch fund_etf_hist_em \
  --arg symbol=513500 \
  --arg start_date=20250101 \
  --arg end_date=20251231 \
  --require-columns 日期,收盘,成交量 \
  --output /tmp/513500.parquet \
  --json
```

`fetch` refuses an empty result and an existing output by default. It writes `/tmp/513500.parquet.meta.json` alongside a successful artifact. Use `--allow-empty` or `--overwrite` only when that behavior is intentional.

## Data contract

- `--arg symbol=000001` remains a string because the target signature declares `symbol` as `str`.
- No automatic adjustment, unit conversion, currency conversion, filling, or data cache is applied.
- Parquet is preferred for typed artifacts; JSONL preserves leading-zero codes without optional dependencies. CSV is for interoperability and must be read with the sidecar schema in mind.
- `--require-columns` is the caller's schema-drift guard.
- Data calls run in a worker process. `--timeout` applies to the whole call; transient connection timeouts are retried at most `--retries + 1` times.

Run the local checks with:

```bash
uv run --extra dev pytest
uv run --extra dev ruff check .
uv build
```
