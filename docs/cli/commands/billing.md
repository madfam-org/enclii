# enclii billing

View current-period spend and manage per-project budgets.

## Synopsis

```bash
enclii billing <subcommand> [flags]
```

## Description

The `billing` subtree exposes platform spend, per-project budgets, and budget threshold alerts. Alerts are delivered through Dhanam (email and Slack) at 50/80/100% of a budget. A 100% crossing additionally **hard-throttles non-production deploys** until an operator clears the throttle.

Spend is reported in minor currency units (cents). All amounts are displayed in the budget's currency (default `USD`).

## Subcommands

### `show`

Show current-period spend vs. budgets.

```bash
enclii billing show [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project`, `-p` | string | | Project slug |
| `--period` | string | `30d` | Period window (`7d`, `14d`, `30d`, `90d`, `1y`, `mtd`) |

### `alerts`

List recent budget threshold crossings.

```bash
enclii billing alerts [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project`, `-p` | string | | Project slug |

### `budgets`

Manage per-project budgets.

```bash
enclii billing budgets <create|list|update|delete> [flags]
```

#### `budgets create`

```bash
enclii billing budgets create [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project`, `-p` | string | | Project slug |
| `--amount` | int | | Budget amount in minor currency units (cents) |
| `--currency` | string | `USD` | ISO currency code |
| `--period` | string | `monthly` | `monthly`, `weekly`, or `quarterly` |
| `--thresholds` | string | | Comma-separated percent thresholds (e.g. `50,80,100`) |
| `--hard-throttle` | bool | `true` | Auto-throttle non-production deploys at 100% |

#### `budgets list`

```bash
enclii billing budgets list [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project`, `-p` | string | | Project slug |

#### `budgets update`

```bash
enclii billing budgets update <budget_id> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project`, `-p` | string | | Project slug |
| `--amount` | int | | New amount in cents |
| `--thresholds` | string | | New percent thresholds |
| `--hard-throttle` | bool | `true` | Enable/disable hard throttle |

#### `budgets delete`

```bash
enclii billing budgets delete <budget_id> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project`, `-p` | string | | Project slug |
| `--force` | bool | `false` | Skip confirmation |

## Examples

### Show spend for the current month

```bash
enclii billing show --project my-api --period mtd
```

### Create a $500/month budget with default thresholds

```bash
enclii billing budgets create --project my-api --amount 50000 --period monthly
```

`--amount 50000` = 50000 cents = $500.00.

### Create a budget without the hard throttle

```bash
enclii billing budgets create \
  --project my-api \
  --amount 100000 \
  --period monthly \
  --thresholds 50,80,100 \
  --hard-throttle=false
```

### List recent threshold crossings

```bash
enclii billing alerts --project my-api
```

### Delete a budget without prompting

```bash
enclii billing budgets delete bdg_abc123 --project my-api --force
```

## Notes

- Hard-throttle blocks deploys to non-production environments only. Production deploys continue (so a budget overrun never takes a paying tenant offline).
- Threshold percentages are inclusive — a 100 alert fires the first time spend ≥ 100%.
- Spend is computed daily; `enclii billing show` reflects the most recent rollup.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `10` | Validation error (missing project/amount, invalid period) |
| `50` | Authentication error |

## See Also

- [`enclii projects`](./projects.md) - Inspect projects whose spend you want to budget
- [`enclii deploy`](./deploy.md) - Deploys throttled by 100% budget crossings
- [`enclii audit`](./audit.md) - Audit log of budget changes
