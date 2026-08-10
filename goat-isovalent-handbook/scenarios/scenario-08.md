# Scenario 8 — NodePort exposed services

**Coverage:** 🛑 Block · 🔍 Identify · Control: **Cilium host firewall (CiliumClusterwideNetworkPolicy)**
**Target:** any NodePort service (30000–32767) reachable on the node's external IP

## The attack
NodePort exposes a service on every node's external IP with no firewall. The
attacker scans the default NodePort range and reaches services never meant to be
public:

```bash
kubectl get nodes -o wide          # external IPs
nmap -p 30000-32767 <EXTERNAL-IP>
nc -zv <EXTERNAL-IP> 30003          # internal service, now reachable
```

## Identify (Hubble)
With the host firewall enabled, traffic to the node itself gets an identity
(`reserved:host`). External hits on NodePorts are visible — and, once the policy
is applied, DROPPED:

```bash
hubble observe --to-identity host --follow
```

## Block (Cilium host firewall)
```bash
# host firewall must be enabled at install (scripts/00-install-isovalent.sh does this)
kubectl apply -f policies/network/08-nodeport-host-firewall.yaml
```
The `CiliumClusterwideNetworkPolicy` targets nodes (`nodeSelector`) and denies
world traffic to the 30000–32767 range while explicitly allowing what the node
legitimately needs (apiserver, health, SSH from a bastion).

## Talk track
> "NodePort is a hole in every node at once, and stock Kubernetes has no answer
> for it. Cilium's host firewall is a network policy for the node itself — we
> deny the world to the NodePort range and keep the ports the node actually
> needs. Same policy model as pods, extended to the host."

## Limitations / honesty
Cloud security groups / firewalls can also block this at the infrastructure
layer, and often should. Cilium's host firewall is valuable where you want the
control *in the cluster*, portable across clouds, and expressed as the same
policy CRD. Prefer LoadBalancer/Ingress with proper controls over NodePort.
