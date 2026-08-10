# Tetragon TracingPolicies — suggested baseline

A curated set of runtime-security policies. Each carries a category label and an
action annotation so the **Runtime Policies** screen can group them and toggle
each between **Monitor** (`Post`) and **Kill** (`Sigkill`) — or remove it.

Everything ships in **monitor mode**. Watch the Security Events log first, confirm
you aren't about to kill legitimate workloads, then flip individual policies to Kill.

## Applied by default

| Policy | Category | Detects |
|---|---|---|
| `block-cloud-metadata` | egress | Connections to `169.254.169.254` (SSRF / credential theft) |
| `block-crypto-mining` | egress | TCP to common mining-pool ports |
| `block-tmp-exec` | exec | Execution from `/tmp`, `/dev/shm`, `/var/tmp` (dropper pattern) |
| `process-exec-elf` | exec | The ELF binary behind every exec — full execution provenance |
| `file-integrity` | file | Access to `/etc/shadow`, `/etc/sudoers`, `/etc/passwd`, SSH keys |
| `file-monitoring` | file | Broader sensitive-path reads/writes/truncates, with noise filters |
| `privilege-escalation` | privilege | `setuid(0)` |
| `privileges-raise` | privilege | `capset`, new user namespaces, the full setuid/setgid family |
| `process-creds-changed` | privilege | In-kernel credential changes (`commit_creds`, `override_creds`) |
| `sensitive-capabilities` | capability | Kernel-module loading (container escape / rootkit indicator) |

```bash
kubectl apply -f policies/tetragon/
```

`kubectl apply -f <dir>` is non-recursive, so this applies the default set only.

## Optional — requires BPF LSM

`optional/` holds policies built on **BPF LSM hooks**, which need a kernel booted
with BPF LSM enabled (`CONFIG_BPF_LSM=y` and `bpf` in `lsm=`). They will fail to
load otherwise — which is why they are not applied by default. On many managed
node images (including stock AKS Ubuntu) BPF LSM is not enabled.

| Policy | Category | Notes |
|---|---|---|
| `lsm-bprm-check` | exec | `bprm_check_security` — can **block** execution via `Override` |
| `lsm-file-open` | file | `file_open` hook |

```bash
kubectl apply -f policies/tetragon/optional/
```

Check support first:

```bash
grep -o 'bpf' /sys/kernel/security/lsm 2>/dev/null || echo "BPF LSM not enabled"
```

## Overlap

Several policies intentionally overlap (`privilege-escalation` ⊂ `privileges-raise`,
`block-tmp-exec` and `process-exec-elf` share a hook). Overlap costs a little
duplicate eventing; remove whichever you don't want from the Runtime Policies
screen. Start narrow if event volume matters.

## Attribution

`file-monitoring`, `privileges-raise`, `process-creds-changed`, `process-exec-elf`,
`lsm-bprm-check`, and `lsm-file-open` are adapted from
[gccloudone-aurora/tetragon-policies](https://github.com/gccloudone-aurora/tetragon-policies)
(MIT, © His Majesty the King in Right of Canada). Only metadata was added —
a category label, an action annotation, and a description — so the UI can
organize and toggle them. See `NOTICE.md`.
