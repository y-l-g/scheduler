# Changelog

## Unreleased

- Provides an embedded minute-aligned scheduler for FrankenPHP/Caddy.
- Supports configurable command, working directory, timeout, overlap mode, and
  shutdown grace period.
- Current limits: experimental API, single-process trigger only, no distributed
  locking, and not recommended for Kubernetes production scheduling.
