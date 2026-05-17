# Pogo Scheduler

A small [Caddy](https://caddyserver.com) / [FrankenPHP](https://frankenphp.dev) module that acts as an embedded cron trigger for constrained or single-binary PHP deployments.

It runs a lightweight Go ticker and executes a configurable command, such as `php artisan schedule:run`, every minute aligned to the wall clock.

Pogo Scheduler is intentionally not a distributed scheduler. In Kubernetes production environments, prefer native `CronJob` resources.

## Production status

Pogo Scheduler is a small experimental module for constrained, single-binary,
and single-node PHP deployments. Its API may change.

It is not a distributed scheduler. In horizontally scaled deployments, run it as
a singleton service or rely on application-level shared locks. In Kubernetes
production environments, prefer native `CronJob` resources.

## Installation

### 1. Docker

Build a FrankenPHP binary that includes Pogo Scheduler with `xcaddy`. See the official
[FrankenPHP Docker documentation](https://frankenphp.dev/docs/docker/) for the
base image details.

Example Dockerfile from this repository root:

```dockerfile
FROM dunglas/frankenphp:builder AS builder

COPY --from=caddy:builder /usr/bin/xcaddy /usr/bin/xcaddy
COPY . /src/scheduler

RUN CGO_ENABLED=1 \
    XCADDY_SETCAP=1 \
    XCADDY_GO_BUILD_FLAGS="-ldflags='-w -s' -tags=nobadger,nomysql,nopgx" \
    CGO_CFLAGS="$(php-config --includes)" \
    CGO_LDFLAGS="$(php-config --ldflags) $(php-config --libs)" \
    xcaddy build \
        --output /usr/local/bin/frankenphp \
        --with github.com/dunglas/frankenphp=./ \
        --with github.com/dunglas/frankenphp/caddy=./caddy  \
        --with github.com/dunglas/caddy-cbrotli \
        --with github.com/y-l-g/scheduler/module=./src/scheduler/module

FROM dunglas/frankenphp AS runner

COPY --from=builder /usr/local/bin/frankenphp /usr/local/bin/frankenphp
```

Then copy your app and `Caddyfile` into the runner image as usual.

### 2. Configure Caddyfile

Add a `pogo_scheduler` block to your Caddyfile. Example for Laravel:

```caddy
{
    pogo_scheduler {
        command        php artisan schedule:run
        dir            /var/www/html
        timeout        5m
        overlap        allow
        shutdown_grace 30s
    }
}
```

- **command**: command to run every minute (default: `php artisan schedule:run`).
- **dir**: working directory for the command (optional).
- **timeout**: max duration per run (default: 5m).
- **overlap**: `allow` or `skip` (default: `allow`). When set to `skip`, a tick is skipped if the previous command is still running.
- **shutdown_grace**: max time to let an active command finish during shutdown before it is cancelled (default: 30s).

### 3. Run Octane

Start the server using the configured Caddyfile:

```bash
php artisan octane:frankenphp --caddyfile=Caddyfile
# or
./frankenphp run 
```

---

## How It Works

1. **The Ticker (Go)**: A goroutine wakes up every 60 seconds, aligned to the start of each minute (`:00`).
2. **The Command**: At each tick, the module runs the configured command in a subprocess (e.g. `php artisan schedule:run`).
3. **Timeout**: Each run is bounded by the configured `timeout`; the process is killed if it exceeds it.
4. **Overlap Mode**: By default, every minute tick starts a command. Use `overlap skip` to drop a tick while the previous command is still running.
5. **Graceful Shutdown**: On Caddy shutdown or reload, no new commands are started. The active command can finish within `shutdown_grace`; after that, it is cancelled.

## When To Use It

Good fits:

- Single VPS or single-server deployments.
- Docker Compose or small Docker setups where you want to avoid `cron`, `supervisord`, or a separate scheduler container.
- PaaS environments where the app has one long-running web process but no system cron.
- Development and staging environments where you want scheduler behavior without host setup.
- A dedicated singleton scheduler service using the same application image.

Poor fits:

- Kubernetes production. Use native `CronJob` resources instead.
- Horizontally scaled web replicas unless your application uses shared locks, such as Laravel `onOneServer()` / `withoutOverlapping()` with a shared cache backend.
- Critical exactly-once workflows without application-level idempotency and locking.

## Overlap Behavior

The default starts a command on every minute boundary:

```caddy
overlap allow
```

This matches Laravel's `schedule:run` behavior. Sub-minute tasks such as `everySecond()` keep `schedule:run` alive for most of the minute, and a small overrun at `:00` should not skip the whole next minute.

You can opt into a local overlap guard:

```caddy
overlap skip
```

Use `skip` only when the command itself must never overlap. If one run is still active at the next minute boundary, the new run is skipped and a warning is logged.
