<?php

// Ensure we are in worker mode
if (!function_exists('frankenphp_handle_request')) {
    exit(1);
}

$logFile = __DIR__ . '/scheduler_test.log';
$stderr = fopen('php://stderr', 'w');

fwrite($stderr, "[Worker] Booted at " . time() . "\n");

// The handler logic - runs only when a signal is received
// FIX: Added default value for $payload to prevent ArgumentCountError
$handler = function ($payload = null) use ($logFile, $stderr) {
    fwrite($stderr, "[Worker] Received Tick at " . time() . "\n");

    $fp = fopen($logFile, 'a');
    if ($fp) {
        fwrite($fp, time() . "\n");
        fclose($fp);
    }
};

// Enter the request loop
while (frankenphp_handle_request($handler)) {
    // Prevent memory leaks in long-running tests
    gc_collect_cycles();
}