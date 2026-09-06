# Scenario 4 — Container escape to the host system

**Coverage:** 🛑 Block (runtime) · 🔍 Identify · Control: **Tetragon + admission (primary)**
**Target:** `system-monitor` (privileged, hostPath `/host-system` mounted)

## The attack
A privileged pod with the host filesystem mounted lets the attacker `chroot`
onto the node and steal node credentials:

```bash
capsh --print                       # full capabilities
mount                               # /host-system present
chroot /host-system bash
cat /etc/kubernetes/admin.conf      # or /var/lib/kubelet/kubeconfig  -> game over
kubectl --kubeconfig /etc/kubernetes/admin.conf get nodes
```

## Identify (Tetragon)
Tetragon sees the `chroot` into the mounted host root and the read of the node
kubeconfig — the two decisive steps:

```bash
kubectl apply -f policies/tetragon/04-container-escape.yaml   # includes observe-chroot
kubectl -n kube-system exec ds/tetragon -c tetragon -- tetra getevents -o compact --pods system-monitor
# ⚡ sys_chroot /host-system
# ⚡ file open .../kubernetes/admin.conf
```

## Block (Tetragon runtime backstop)
The same policy **Sigkills** any process that opens the node credential files
(`admin.conf`, kubelet `kubeconfig`). The escape can happen, but the credential
theft that makes it "game over" is killed:

```bash
kubectl apply -f policies/tetragon/04-container-escape.yaml
```

## Primary fix — admission control
The correct prevention is to **never schedule this pod**: no `privileged: true`,
no `hostPath` mount of `/`. Enforce with Kyverno (scenario 22) or Pod Security
Admission `restricted`. Tetragon is the runtime net for when a privileged pod
does slip through (e.g. a monitoring agent that genuinely needs some caps).

## Talk track
> "This one is honest about layering. Admission control should stop this pod
> from ever running — that's Kyverno's job. But privileged monitoring pods are
> real, so Tetragon watches what they actually do: the moment one chroots to the
> host and reaches for the kubelet kubeconfig, eBPF kills it and you get the
> event. Prevention at admission, detection-and-response at runtime."

## Limitations / honesty
Cilium network policy is largely irrelevant to the escape itself; it only helps
limit what a stolen node identity can reach afterwards. This is a Tetragon +
admission story, not a network story — say so.
