# FrankenPHP Native Scheduler

A [FrankenPHP](https://frankenphp.dev) extension and Laravel package that replaces the system `crond` and `php artisan schedule:run`.

It runs the scheduler entirely within the FrankenPHP binary, leveraging a lightweight Go ticker to trigger the PHP worker exactly every minute.

## Features

*   **Zero External Processes**: No `crond`, no `supervisord`, no sidecar containers.
*   **Memory Efficient**: Boots the Laravel application **once** and keeps it in memory.
*   **Precision**: Aligns triggers to the start of the minute (`:00` seconds).
*   **Safety**: Enforces a single-thread worker to prevent overlapping schedule runs.

### Installation

Follow [the instructions to install a ZTS version of libphp and `xcaddy`](https://frankenphp.dev/docs/compile/#install-php).
Then, use [`xcaddy`](https://github.com/caddyserver/xcaddy) to build FrankenPHP with the `pogo-queue` module:

```console
CGO_ENABLED=1 \
CGO_CFLAGS=$(php-config --includes) \
CGO_LDFLAGS="$(php-config --ldflags) $(php-config --libs)" \
xcaddy build \
    --output frankenphp \
    --with github.com/y-l-g/scheduler \
    --with github.com/dunglas/frankenphp/caddy \
    --with github.com/dunglas/caddy-cbrotli
```

### Install the Laravel Package

```bash
composer require pogo/scheduler
```

### Install the Worker Script

```bash
php artisan pogo:scheduler:install
```

This command publishes `public/scheduler-worker.php`.

## Configuration

Add the `pogo_scheduler` block to your Global Options in the `Caddyfile`.

```caddyfile
    pogo_scheduler {
        # Path to the published worker script
        worker public/scheduler-worker.php
    }
```

## How It Works

1.  **The Ticker (Go)**: A Goroutine wakes up every 60 seconds (aligned to the wall clock).
2.  **The Trigger**: It sends a signal to a dedicated FrankenPHP worker pool.
3.  **The Worker (PHP)**: The `scheduler-worker.php` script (running in a dedicated thread) receives the signal and calls `$kernel->call('schedule:run')`.

### Concurrency Note
The scheduler module forces `num_threads 1` for its worker pool. This guarantees that `schedule:run` is never executed in parallel with itself, effectively preventing overlapping runs at the process level.

If a scheduled task takes longer than 60 seconds:
1.  The Go ticker tries to send the next signal.
2.  The signal waits (up to 65s) for the PHP worker to become free.
3.  Once the previous run finishes, the next one starts immediately.