Elector
=======

Simple leader election using Leases.

Elector elects a leader by acquiring a Lease with OwnerReference set to the owning pod.

This ensures that only a single leader exists at any given time.
As long as the leader pod exists, it will continue to be leader.
*This means that a leader in CrashLoopBackoff state will still be leader*.

This choice is made on the basis that it is better with no leader than two leaders.

Users should make sure to have alerts to detect when a leader is stuck.

API
---

When running, elector can be queried on the election port to get the name of the current leader.

Default election port is 6060 (override with `--http`).
Metrics are available on port 9090 (override with `--metrics-address`).
Probes are available on port 8080 (override with `--probe-address`).
