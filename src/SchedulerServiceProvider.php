<?php

namespace Pogo\Scheduler;

use Illuminate\Support\ServiceProvider;
use Pogo\Scheduler\Console\InstallCommand;

class SchedulerServiceProvider extends ServiceProvider
{
    public function boot()
    {
        if ($this->app->runningInConsole()) {
            $this->commands([
                InstallCommand::class,
            ]);
        }
    }
}