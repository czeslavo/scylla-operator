# Scylla Doctor bundle image (proof of concept)

The image SODA (`scylla-operator diagnose`) takes Scylla Doctor from. Release Engineering will own the real build;
this one exists to make the PoC runnable. See
[enhancements/proposals/jira-551-soda-scylla-doctor](../../enhancements/proposals/jira-551-soda-scylla-doctor).

| Path | What it is |
|---|---|
| `/opt/scylla-doctor/collect/scylla_doctor.pyz` | Scylla Doctor's collection build; SODA streams it into each scylla container |
| `/opt/scylla-doctor/analyze/scylla_doctor_full.pyz` | the full build, for per-node analysis |
| `/opt/scylla-doctor/analyze/scylla_doctor_cluster.pyz`, `.ini` | Scylla Doctor Cluster and its shipped configuration |
| `/opt/scylla-doctor/runtime-contract.json` | what Scylla Doctor needs from the ScyllaDB image |
| `/opt/scylla-doctor/bundle.json` | Scylla Doctor version and the sha256 of every file above |
| `/usr/bin/python3.12` | Python with deepdiff and PyYAML, pinned with hashes, for analysis |

It runs as UID 65532 and has `tar`, so SODA can stream files in and `kubectl cp` works.

## Building

The inputs are not in git; the full build and Scylla Doctor Cluster are private.

```bash
./prepare-inputs.sh scylla-doctor-1.15.tar.gz scylla-doctor-full-1.15.tar.gz path/to/scylla-doctor-repo
podman build -t registry.example.com/scylla-doctor-bundle:1.15 .
```

`prepare-inputs.sh` builds Scylla Doctor Cluster from the repository (`make -C scylla-doctor-cluster portable`)
when it isn't built yet. Check out the tag matching the tarballs first.
