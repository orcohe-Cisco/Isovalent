# Scenario 12 — Gaining environment information

**Coverage:** 🧩 Complementary · 🔍 Identify · Control: **Tetragon file/secret-read detection**
**Target:** any pod — dumps env vars, mounts, and mounted Kubernetes secrets

## The attack
Post-compromise reconnaissance inside a container: read env vars (which often
hold secrets), the mounted service-account token, and secret volumes.

```bash
printenv                                        # secrets in env
cat /proc/self/cgroup ; mount ; ls -la /home/
cat /var/run/secrets/kubernetes.io/serviceaccount/token
```

## Identify (Tetragon)
There's no network flow here — this is entirely on-host, which is exactly where
eBPF runtime visibility earns its keep. Detect reads of the SA token / secret
material by a shell or curl:

```bash
kubectl apply -f policies/tetragon/12-16-serviceaccount-token.yaml
kubectl -n kube-system exec ds/tetragon -c tetragon -- tetra getevents -o compact
# ⚡ file open .../serviceaccount/token  by /bin/cat
```

## Block (with care)
You *can* add `- action: Sigkill` to the token-read policy, but many runtimes
read the token legitimately — scope with `matchBinaries` (shells/curl only)
before enforcing. The safer default is detect + alert, then pair with scenario
16's network control so a stolen token can't reach the API server.

## Talk track
> "The interesting thing here is there's no packet to catch — it's a process
> reading a file. That's the Tetragon half of the platform: it sees the token
> read at the syscall level and tells you which binary did it. A monitoring agent
> reading its own token is fine; `/bin/cat` reading it is not, and now you know."

## Limitations / honesty
The primary fixes are not storing secrets in env vars, using projected tokens
with short TTLs, and RBAC. Tetragon adds detection of the theft attempt; it does
not remove the secret from the pod.
