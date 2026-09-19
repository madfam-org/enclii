# redis-sentinel offline tests

Two self-contained checks for the two pieces of `infra/k8s/redis-sentinel/` that decide
who is the master. Both render the manifests with `kustomize`, run the **real** rendered
config or script on the **pinned** images in docker, assert, and clean up. Run them
before changing the proxy health-check or the `config-init` script; CI does not (the ARC
runners have no docker).

| script | exercises | runtime |
|---|---|---|
| `proxy-check-test.sh` | the HAProxy backend check in `redis-ha-proxy.yaml`: only the master is UP (`role:master` and `connected_slaves ≥ 1`), slots start DOWN until checked (`init-state down`), the master goes DOWN without a replica and UP when it returns | ~1 min |
| `config-init-test.sh` | the boot decision in `redis-ha.yaml`'s `config-init`: cold start, refusing peers with a late Sentinel, forced fallback, replica restart, healthy master restart, master death mid-failover, a dead non-self master | ~2 min |

```bash
tests/redis-sentinel/proxy-check-test.sh
tests/redis-sentinel/config-init-test.sh
```

Needs `docker`, `kustomize`, `python3` with PyYAML. Exit code 0 = all assertions passed.
Background and the drills these tests were distilled from:
`docs/runbooks/redis-sentinel-migration.md` and `docs/runbooks/redis-failover-log.md`.
