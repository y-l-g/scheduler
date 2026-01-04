package tests

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSchedulerEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	_, currentFile, _, _ := runtime.Caller(0)
	testDir := filepath.Dir(currentFile)
	rootDir := filepath.Dir(testDir)

	binPath := filepath.Join(rootDir, "frankenphp")
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		t.Fatalf("FrankenPHP binary not found at %s. Build it first.", binPath)
	}

	logFile := filepath.Join(testDir, "scheduler_test.log")
	_ = os.Remove(logFile)

	workerPath := filepath.Join(testDir, "worker.php")

	caddyfileContent := fmt.Sprintf(`
	{
		auto_https off
		frankenphp
		order php_server before file_server
		
		pogo_scheduler {
			worker "%s"
		}
	}

	:8080 {
		php_server
	}
	`, workerPath)

	tmpCaddyfile, err := os.CreateTemp("", "Caddyfile_Scheduler.*")
	if err != nil {
		t.Fatalf("Failed to create temp Caddyfile: %v", err)
	}
	defer func() {
		_ = os.Remove(tmpCaddyfile.Name())
	}()

	if _, err := tmpCaddyfile.WriteString(caddyfileContent); err != nil {
		t.Fatalf("Failed to write Caddyfile: %v", err)
	}
	if err := tmpCaddyfile.Close(); err != nil {
		t.Fatalf("Failed to close Caddyfile: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, "run", "--config", tmpCaddyfile.Name())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	t.Log("Server started. Waiting for scheduler alignment...")

	now := time.Now()
	nextMinute := now.Truncate(time.Minute).Add(time.Minute)
	// Wait until the minute mark + 2 seconds for the tick to process
	waitDuration := time.Until(nextMinute) + 2*time.Second

	t.Logf("Current: %s. Target: %s. Sleeping %s...", now.Format(time.TimeOnly), nextMinute.Format(time.TimeOnly), waitDuration)

	time.Sleep(waitDuration)

	// Verification
	content, err := os.ReadFile(logFile)
	if err != nil {
		// Retry once after 5s
		time.Sleep(5 * time.Second)
		content, err = os.ReadFile(logFile)
		if err != nil {
			t.Fatalf("Log file not found. Worker failed to trigger. Error: %v", err)
		}
	}

	logs := strings.TrimSpace(string(content))
	lines := strings.Split(logs, "\n")

	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		t.Fatalf("Log file empty. Worker did not write anything.")
	}

	t.Logf("Worker Triggered! Logs: %v", lines)

	// Ensure we don't have excessive writes (which would indicate a crash loop)
	if len(lines) > 2 {
		t.Fatalf("Too many log entries (%d). Worker might be crashing/restarting instead of waiting for ticks.", len(lines))
	}

	cancel()
	_ = cmd.Wait()
}
