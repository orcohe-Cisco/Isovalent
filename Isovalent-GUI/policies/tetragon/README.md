# Default Tetragon TracingPolicies (best-practice baseline)

A curated set of runtime-security policies drawn from Tetragon's documented
best practices. Each is **organized by category** and carries an action
annotation so isovalent-control can list them and toggle enforcement per policy
between **monitor** (observe only — `Post`) and **enforce** (kill — `Sigkill`).

| File | Category | What it catches | Default action |
|---|---|---|---|
| `block-cloud-metadata.yaml` | egress | Connections to the cloud metadata IP `169.254.169.254` (SSRF / credential theft) | monitor |
| `file-integrity.yaml` | file | Access to `/etc/shadow`, `/etc/sudoers`, `/etc/passwd`, SSH keys | monitor |
| `block-tmp-exec.yaml` | exec | Executing binaries from `/tmp` or `/dev/shm` (dropper pattern) | monitor |
| `privilege-escalation.yaml` | privilege | `setuid`/`setgid` to root (0) | monitor |
| `sensitive-capabilities.yaml` | capability | Use of `CAP_SYS_ADMIN` / raw-socket capable syscalls | monitor |
| `block-crypto-mining.yaml` | egress | TCP connects to common mining-pool ports | monitor |

Convention used by isovalent-control:

```yaml
metadata:
  labels:
    app.kubernetes.io/managed-by: isovalent-control
    isovalent-control.io/category: egress      # egress|file|exec|privilege|capability
  annotations:
    isovalent-control.io/action: monitor        # monitor|enforce  (UI toggles this)
```

The UI's Active Policies screen groups these by `category` and offers a
one-click **Monitor / Kill** switch per policy, which rewrites every
`matchActions[].action` (`Post` ⇄ `Sigkill`) and re-applies via the K8s API.

Apply the whole set:

```bash
kubectl apply -f policies/tetragon/
```

> Start in **monitor** everywhere, watch the Runtime Security stream to confirm
> you're not about to kill legitimate workloads, then flip individual policies
> to **enforce**. On Kubernetes Goat these are a great way to demonstrate
> detection first, enforcement second.
