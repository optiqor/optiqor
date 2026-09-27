# Kubernetes version below the supported floor

The pre-flight checker rejected the cluster because the apiserver
reports a version below 1.28. The floor is 1.28 because the agent
relies on `endpoints/v1` GA, structured config types from VPA `v0.13`,
and the projected-token audience claim that landed in 1.27.

## Verify

```
kubectl version --short
```

If the server version is < 1.28, the agent's informer caches will
hit `the server could not find the requested resource` on at least
one of the GVRs it watches, and the dashboard's Agent Health pill
will flap.

## Fix

Upgrade the cluster control plane to 1.28+ before installing the
agent. For EKS:

```
eksctl upgrade cluster --name <cluster> --version 1.31 --approve
```

For self-managed clusters follow your distro's upgrade path. Cluster
upgrades land cleanly during maintenance windows; do not retry the
agent install until the apiserver reports the new version.

## Why we don't soften the floor

1.27 was patched out of upstream support in 2024. Customers running
1.27 or below typically have other 90+ day maintenance debt; routing
Optiqor through that environment hides the real problem from the
platform team. The pre-flight failing closed is the right answer.
