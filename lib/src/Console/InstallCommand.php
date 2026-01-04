<?php

namespace Pogo\Scheduler\Console;

use Illuminate\Console\Command;

class InstallCommand extends Command
{
    protected $signature = 'pogo:scheduler:install';
    protected $description = 'Install the Pogo Scheduler worker and instructions';

    public function handle()
    {
        $this->info('Installing Pogo Scheduler...');

        $this->publishWorker();

        $this->newLine();
        $this->info('Installation complete.');

        $this->displayCaddyInstructions();
    }

    protected function publishWorker()
    {
        $stubPath = __DIR__ . '/../../stubs/scheduler-worker.php';
        $destination = public_path('scheduler-worker.php');

        if (!file_exists($stubPath)) {
            $stubPath = base_path('stubs/scheduler-worker.php');
        }

        if (file_exists($destination)) {
            if (!$this->confirm('public/scheduler-worker.php already exists. Overwrite?', false)) {
                return;
            }
        }

        if (file_exists($stubPath)) {
            copy($stubPath, $destination);
            $this->comment('Created public/scheduler-worker.php');
        } else {
            $this->error('Could not find scheduler-worker.php stub.');
        }
    }

    protected function displayCaddyInstructions()
    {
        $this->newLine();
        $this->warn('ACTION REQUIRED: Update Caddyfile');
        $this->line('Add the following block to your global Caddyfile options:');
        $this->newLine();

        $snippet = <<<CADDY
            {
                pogo_scheduler {
                    worker public/scheduler-worker.php
                }
            }
            CADDY;

        $this->line('<fg=gray>' . $snippet . '</>');
        $this->newLine();
    }
}
