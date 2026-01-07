# Pogo Scheduler

A [FrankenPHP](https://frankenphp.dev) extension and Laravel package that replaces the system `crond` and `php artisan schedule:run`.

It runs the scheduler entirely within the FrankenPHP binary, leveraging a lightweight Go ticker to trigger the PHP worker exactly every minute.

## Features

* **Zero External Processes**: No `crond`, no `supervisord`, no sidecar containers.
* **Memory Efficient**: Boots the Laravel application **once** and keeps it in memory.
* **Precision**: Aligns triggers to the start of the minute (`:00` seconds).
* **Safety**: Enforces a single-thread worker to prevent overlapping schedule runs.

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

### 2. Install Laravel Dependencies

Ensure Laravel Octane is installed and configured for FrankenPHP:

```bash
php artisan octane:install --server=frankenphp
```

### 3. Create Worker Script

Create a new file at `public/scheduler-worker.php`.
This script is a dedicated entry point that runs the scheduler command.

```php
<?php

use Illuminate\Contracts\Console\Kernel;
use Laravel\Octane\ApplicationFactory;
use Laravel\Octane\FrankenPhp\FrankenPhpClient;
use Laravel\Octane\Worker;

if ((!($_SERVER['FRANKENPHP_WORKER'] ?? false)) || !function_exists('frankenphp_handle_request')) {
    echo 'FrankenPHP must be in worker mode to use this script.';
    exit(1);
}

ignore_user_abort(true);

$basePath = $_SERVER['APP_BASE_PATH'] ?? $_ENV['APP_BASE_PATH'] ?? dirname(__DIR__);

require_once $basePath . '/vendor/autoload.php';

$frankenPhpClient = new FrankenPhpClient();

$worker = tap(new Worker(
    new ApplicationFactory($basePath),
    $frankenPhpClient
))->boot();

$requestCount = 0;
$maxRequests = $_ENV['MAX_REQUESTS'] ?? $_SERVER['MAX_REQUESTS'] ?? 60;

try {
    $handleRequest = static function ($payload = null) use ($worker) {
        try {
            $app = $worker->application();
            $kernel = $app->make(Kernel::class);

            // Execute the schedule run command
            $kernel->call('schedule:run');

        } catch (Throwable $e) {
            if ($worker) {
                try {
                    report($e);
                } catch (Throwable $ex) {
                    // Silent fail
                }
            }
            fwrite(STDERR, "[Scheduler] Error: " . $e->getMessage() . "\n");
        }
    };

    while ($requestCount < $maxRequests && frankenphp_handle_request($handleRequest)) {
        $requestCount++;
    }
} finally {
    $worker?->terminate();
    gc_collect_cycles();
}
```

### 4. Configure Caddyfile

Update your `Caddyfile` (usually at the project root) to include the `pogo_scheduler` block.
Below is a complete example based on the official Octane configuration.

```caddy
{
    {$CADDY_GLOBAL_OPTIONS}

    admin {$CADDY_SERVER_ADMIN_HOST}:{$CADDY_SERVER_ADMIN_PORT}

    frankenphp {
        worker {
            file "{$APP_PUBLIC_PATH}/frankenphp-worker.php"
            {$CADDY_SERVER_WORKER_DIRECTIVE}
            {$CADDY_SERVER_WATCH_DIRECTIVES}
        }
    }
    
    # Scheduler Configuration
    pogo_scheduler {
        # Path to the published worker script
        worker {$APP_PUBLIC_PATH}/scheduler-worker.php
    }
}

{$CADDY_EXTRA_CONFIG}

{$CADDY_SERVER_SERVER_NAME} {
    log {
        level {$CADDY_SERVER_LOG_LEVEL}

        # Redact the authorization query parameter that can be set by Mercure...
        format filter {
            wrap {$CADDY_SERVER_LOGGER}
            fields {
                uri query {
                    replace authorization REDACTED
                }
            }
        }
    }
    
    route {
        root * "{$APP_PUBLIC_PATH}"
        encode zstd br gzip

        # Mercure configuration is injected here...
        {$CADDY_SERVER_EXTRA_DIRECTIVES}

        php_server {
            index frankenphp-worker.php
            try_files {path} frankenphp-worker.php
            # Required for the public/storage/ directory...
            resolve_root_symlink
        }
    }
}
```

### 5. Run Octane

Start the server using the configured Caddyfile:

```bash
php artisan octane:frankenphp --caddyfile=Caddyfile
```

---

## How It Works

1. **The Ticker (Go)**: A Goroutine wakes up every 60 seconds (aligned to the wall clock).
2. **The Trigger**: It sends a signal to a dedicated FrankenPHP worker pool.
3. **The Worker (PHP)**: The `scheduler-worker.php` script (running in a dedicated thread) receives the signal and calls `$kernel->call('schedule:run')`.

### Concurrency Note

The scheduler module forces `num_threads 1` for its worker pool. This guarantees that `schedule:run` is never executed in parallel with itself, effectively preventing overlapping runs at the process level.

If a scheduled task takes longer than 60 seconds:

1. The Go ticker tries to send the next signal.
2. The signal waits (up to 65s) for the PHP worker to become available.
3. If the previous minute's task is still running after this wait, the new tick is skipped.