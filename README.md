# Pogo Scheduler

A [Caddy](https://caddyserver.com) / [FrankenPHP](https://frankenphp.dev) module that can replace the system `crond` in some scenarios.

It runs a lightweight Go ticker and executes a configurable command (e.g. `php artisan schedule:run`) every minute, aligned to the wall clock.

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
        command php artisan schedule:run
        dir     /var/www/html
        timeout 5m
    }
}
```

- **command**: command to run every minute (default: `php artisan schedule:run`).
- **dir**: working directory for the command (optional).
- **timeout**: max duration per run (default: 5m).

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

### Concurrency Note

Each tick runs the command in a new process. If a run lasts longer than one minute, the next tick will start another process, so runs can overlap. To avoid that, set a `timeout` shorter than 60s or ensure your scheduled tasks finish within a minute.
