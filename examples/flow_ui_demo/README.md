# Flow UI Demo

This demo bundles multiple flow files and runs the status UI with a realistic facts shape. It also includes a SQL scrape mock to show scheduled polling from a database source.

## Quick start
1. Start the UI demo:

```bash
just ui-flow-demo
```

2. Open the UI:

```bash
just ui-flow-demo-open
```

3. Ingest baseline facts:

```bash
just ui-flow-demo-seed
```

4. Stream simulated updates:

```bash
just ui-flow-demo-stream
```

## SQL scrape mock (Postgres)
Run a local Postgres container that a poller scrapes every 15s.

```bash
just ui-flow-demo-sql-up
just ui-flow-demo-sql-scrape
```

Insert an update row (the poller will pick it up on the next interval):

```bash
just ui-flow-demo-sql-bump
```

Stop the container:

```bash
just ui-flow-demo-sql-down
```

Defaults:
- Postgres port: `55432`
- DSN override: `SQL_SCRAPE_DSN`
- Poll interval override: `SQL_SCRAPE_POLL` (for example `5s`)
- Effectus target: `EFFECTUS_URL` + `EFFECTUS_TOKEN`
