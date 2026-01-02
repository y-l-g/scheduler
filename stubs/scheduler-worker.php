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

if (file_exists($basePath . '/vendor/autoload.php')) {
    require $basePath . '/vendor/autoload.php';
} else {
    fwrite(STDERR, "Could not find vendor/autoload.php at $basePath\n");
    exit(1);
}

if (!defined('STDERR')) {
    define('STDERR', fopen('php://stderr', 'w'));
}

// --------------------------------------------------------------------------
//  CLI Environment Shim
// --------------------------------------------------------------------------
// Laravel and Symfony Console expect these to exist.
if (!isset($_SERVER['PHP_SELF'])) {
    $_SERVER['PHP_SELF'] = 'artisan';
}
if (!isset($_SERVER['SCRIPT_NAME'])) {
    $_SERVER['SCRIPT_NAME'] = 'artisan';
}
if (!isset($_SERVER['argv'])) {
    $_SERVER['argv'] = ['artisan', 'schedule:run'];
}
if (!isset($_SERVER['argc'])) {
    $_SERVER['argc'] = count($_SERVER['argv']);
}

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