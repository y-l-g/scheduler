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

/*
|--------------------------------------------------------------------------
| Start The Octane Worker (Scheduler Mode)
|--------------------------------------------------------------------------
|
| This worker acts as the internal cron daemon. It boots the application
| once and then waits for "ticks" from the Go scheduler to execute
| the 'schedule:run' command.
|
*/

$frankenPhpClient = new FrankenPhpClient();

$worker = tap(new Worker(
    new ApplicationFactory($basePath),
    $frankenPhpClient
))->boot();

$requestCount = 0;
// Restart every hour (60 minutes) to prevent memory leaks in the scheduler process
$maxRequests = $_ENV['MAX_REQUESTS'] ?? $_SERVER['MAX_REQUESTS'] ?? 60;

try {
    $handleRequest = static function () use ($worker) {
        try {
            // Direct application access for Console commands
            $app = $worker->application();

            $kernel = $app->make(Kernel::class);
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