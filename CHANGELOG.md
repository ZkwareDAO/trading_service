# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

---

## [Unreleased]

### Added

- **Spot trading support** — orders can now be placed against spot markets in addition to futures and options.
- **Position query time filters** — position endpoints accept a close-time range for narrowing results.

### Changed

- **Position query performance** — reduced response latency on position list endpoints.
- **Position ordering** — position queries return results in descending ID order.
- **`signal_close` strategies skip default rule generation.**

### Fixed

- `close-all` no longer reports "No active positions" when active positions exist.
- Strategy names now match correctly in position query responses.
- Corrected abnormal data in position query responses.
- Deribit position sync now reloads `user_strategies` into PMS after syncing.
- Manual close notifications include the current ROI.

---

## [1.3.0] — 2026-07-31

### Added

- **Cross-position risk conditions** (`position_<symbol>`) — a rule can trigger based on whether another symbol holds a position.

### Fixed

- `position_<symbol>` conditions failed to trigger due to case-sensitivity in symbol comparison.
- `position_<symbol>` rules with `value=0` were parsed as a boolean, preventing risk control from firing.
- Deribit WebSocket could not recover after a disconnect; also resolved an associated data race.

---

## [1.2.0] — 2026-07-16

### Added

- **Upsert semantics for `POST /api/v1/rules`** — rules are matched on `user_strategy_id` + `condition_name` + `operator`:
  - No match → create (`201 Created`)
  - Existing `active` rule → update `value`, `sort`, `quantity_pct` (`200 OK`)
  - Existing `inactive` rule → create a new rule
  - Existing `in_use` rule → rejected with code `4003` (risk control is mid-execution)

- **Take-profit fallback rules (`fallback_rule`)** — create a primary rule and its drawdown-protection rule in a single request, linked automatically:

  ```json
  {
    "user_strategy_id": 103,
    "condition_name": "roi",
    "operator": ">=",
    "value": 0.3,
    "fallback_rule": { "value": 0.3 }
  }
  ```

  The primary rule's `action` points at the fallback rule's ID. Default fallback threshold is 5%.

### Changed

- **Deribit positions skip default rule generation** — the generic stop-loss + profit-drawdown defaults do not suit options, so PMS no longer auto-generates rules for Deribit positions. Binance and Hyperliquid behaviour is unchanged.

- **Order response fields vary by instrument type** — options (`pos_type=3`) return `quantity`; futures and spot return `cash` and `leverage`. Irrelevant fields are omitted.

### Fixed

- Option symbols such as `BTC-24JUL26-64000-P` were mangled into `BTC-24JUL26-64000-PUSD`, causing Deribit to reject the order as `Invalid params`. Symbol adaptation now recognises the four-part option format and leaves it untouched.
- Option orders reported `cash must be positive`; validation now checks `quantity > 0` for `pos_type=3`.

---

## [1.1.0] — 2026-07-14

### Added

- **Deribit price runtime** — `DeribitPriceRuntime` implements the `PriceRuntime` interface, subscribing to the `ticker.{instrument_name}.100ms` channel and using `mark_price` for option valuation. ROI and PnL calculations now receive live Deribit prices instead of a stale constant.

### Changed

- **Atomic rule creation** — `RuleStore.CreateRule()` performs ID allocation and rule insertion under a single lock, eliminating the race between `NextID()` and `AddRules()`. Verified under the race detector.
  - `NextID()` is deprecated but retained for backward compatibility.
  - `AddRules()` is unchanged and remains available for batch operations.

---

## [1.0.0] — 2026-07-06

### Added

- **CSV data protection** — multiple safeguards against data loss:
  - Shutdown aborts compaction when a reload has failed.
  - Compaction is blocked when memory is empty but files hold data.

### Fixed

- **Hyperliquid fill price accuracy** — the scanner queries `UserFills` for actual execution prices and computes a weighted average across partial fills, rather than relying on a potentially stale value.

### Notes

Log keywords worth monitoring:

```
SAFETY CHECK FAILED         # data-safety warning
CRITICAL: Skipping compact  # data-loss prevention triggered
INACCURATE                  # price fallback in use
```
