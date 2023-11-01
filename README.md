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


Development
-----------

The integration tests uses envtest to simulate a kubernetes cluster.
Using the `integration_test` target in make will configure envtest for you before running the tests.

If you would rather control the setup of envtest yourself, use the setup-envtest command to install and configure envtest.

```bash
go run sigs.k8s.io/controller-runtime/tools/setup-envtest list  # Get list of supported versions
source <(go run sigs.k8s.io/controller-runtime/tools/setup-envtest use -p env ${SUPPORTED_K8S_VERSION})  # Activate selected version in current shell
```
