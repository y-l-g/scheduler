# Pogo Scheduler

A [Caddy](https://caddyserver.com) / [FrankenPHP](https://frankenphp.dev) module that can replace the system `crond` in some scenarios.

It runs a lightweight Go ticker and executes a configurable command (e.g. `php artisan schedule:run`) every minute, aligned to the wall clock.

## Installation

### 1. Get the Binary

You have two options to get FrankenPHP with the scheduler module enabled:

#### Option A: Pre-built Binary or Docker (Recommended)

You can use the pre-compiled binaries or Docker images that already include the scheduler module.

* **Binaries:** Download from [FrankenPHP with websocket, queue, and scheduler releases](https://github.com/y-l-g/websocket/releases).
* **Docker:** Use the [docker image](https://github.com/y-l-g?tab=packages&repo_name=websocket).

#### Option B: Compile from Source

If you prefer to build it yourself, follow [the instructions to install a ZTS version of libphp and `xcaddy`](https://frankenphp.dev/docs/compile/#install-php). Then, use `xcaddy` to build FrankenPHP with the `pogo-scheduler` module:

```bash
CGO_ENABLED=1 \
CGO_CFLAGS=$(php-config --includes) \
CGO_LDFLAGS="$(php-config --ldflags) $(php-config --libs)" \
xcaddy build \
    --output frankenphp \
    --with github.com/y-l-g/scheduler/module \
    --with github.com/dunglas/frankenphp/caddy \
    --with github.com/dunglas/caddy-cbrotli
```

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