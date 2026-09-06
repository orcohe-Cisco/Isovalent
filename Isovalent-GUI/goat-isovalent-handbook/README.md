# Kubernetes GOAT × Isovalent — Attack & Defend Handbook

A demo runbook for **running every Kubernetes GOAT scenario and then detecting
(Identify) and stopping (Block) it with Isovalent** — Cilium (NetworkPolicy, L7,
DNS, Hubble), Tetragon (eBPF runtime enforcement), Egress Gateway / transparent
encryption, and Isovalent Enterprise features (Timescape, Hubble Enterprise UI,
policy recommendation, SIEM export).

Audience: **SE running a customer demo.** Each scenario page is written as a
runbook — the attack, what the customer sees light up in Hubble/Tetragon, the
policy that stops it, and a short talk track.

> ⚠️ Kubernetes GOAT is *intentionally vulnerable*. Only deploy it in a
> throwaway cluster you control. Never run it near production workloads.

---

## How to use this handbook

1. **Stand up the platform** — `scripts/00-install-isovalent.sh` installs Cilium
   (with L7 + DNS proxy + host firewall + Hubble) and Tetragon.
2. **Deploy GOAT** — clone [kubernetes-goat](https://github.com/madhuakula/kubernetes-goat)
   and run `bash setup-kubernetes-goat.sh`, then `bash access-kubernetes-goat.sh`.
3. **Attack first, defenceless** — run the scenario, and watch it in Hubble /
   Tetragon (the *Identify* half). This is the "before".
4. **Apply the defence** — `scripts/apply-all-defenses.sh`, or the single policy
   named on the scenario page. Re-run the attack — it now drops or is killed.
5. **Tell the story** — each page has a talk-track line for the "after".

The demo flow that lands best: **Observe → Identify → Block.** Cilium/Tetragon
in observe mode first *sees* the attack (differentiator vs. a black-box CNI),
then the same telemetry becomes the policy that enforces.

---

## Coverage matrix

Legend: **🛑 Block** = Isovalent prevents it · **🔍 Identify** = Isovalent
detects/observes it · **🧩 Complementary** = Isovalent helps but the primary fix
is admission control / RBAC / posture (noted on the page).

| # | GOAT scenario | Primary Isovalent control | Block | Identify | Page |
|---|---------------|---------------------------|:----:|:-------:|------|
| 1 | Sensitive keys in codebases (.git leak) | Cilium L7 HTTP allow-list | 🛑 | 🔍 | [s01](scenarios/scenario-01.md) |
| 2 | DIND / container socket exploitation | Tetragon socket kill + egress lock | 🛑 | 🔍 | [s02](scenarios/scenario-02.md) |
| 3 | SSRF → cloud metadata / internal svc | Cilium egress deny (metadata CIDR) | 🛑 | 🔍 | [s03](scenarios/scenario-03.md) |
| 4 | Container escape to host | Tetragon (kill cred theft) + admission | 🛑 | 🔍 | [s04](scenarios/scenario-04.md) |
| 5 | Docker CIS benchmarks | Tetragon socket-mount telemetry | 🧩 | 🔍 | [s05](scenarios/scenario-05.md) |
| 6 | Kubernetes CIS benchmarks | Posture (Enterprise) | 🧩 | 🔍 | [s06](scenarios/scenario-06.md) |
| 7 | Attacking private registry | Cilium L7 (deny `_catalog`) | 🛑 | 🔍 | [s07](scenarios/scenario-07.md) |
| 8 | NodePort exposed services | Cilium host firewall (CCNP) | 🛑 | 🔍 | [s08](scenarios/scenario-08.md) |
| 9 | Helm v2 Tiller (deprecated) | Cilium port block | 🛑 | 🔍 | [s09](scenarios/scenario-09.md) |
| 10 | Crypto miner container | Tetragon kill + DNS/egress lock | 🛑 | 🔍 | [s10](scenarios/scenario-10.md) |
| 11 | Kubernetes namespace bypass | Cilium namespace isolation ⭐ | 🛑 | 🔍 | [s11](scenarios/scenario-11.md) |
| 12 | Gaining environment info / secrets | Tetragon token-read detect | 🧩 | 🔍 | [s12](scenarios/scenario-12.md) |
| 13 | DoS memory/CPU (stress-ng) | Tetragon kill + LimitRange | 🛑 | 🔍 | [s13](scenarios/scenario-13.md) |
| 14 | Hacker container preview | Cilium default-deny + Tetragon | 🛑 | 🔍 | [s14](scenarios/scenario-14.md) |
| 15 | Hidden in layers | Registry/posture + Tetragon | 🧩 | 🔍 | [s15](scenarios/scenario-15.md) |
| 16 | RBAC least-privilege misconfig | Cilium API-server egress deny | 🛑 | 🔍 | [s16](scenarios/scenario-16.md) |
| 17 | KubeAudit | Posture (Enterprise) | 🧩 | 🔍 | [s17](scenarios/scenario-17.md) |
| 18 | Falco runtime monitoring | **Tetragon = the Isovalent engine** | 🛑 | 🔍 | [s18](scenarios/scenario-18.md) |
| 19 | Popeye cluster sanitizer | Posture (Enterprise) | 🧩 | 🔍 | [s19](scenarios/scenario-19.md) |
| 20 | Secure network boundaries (NSP) | **Cilium — upgrade NSP to L7** | 🛑 | 🔍 | [s20](scenarios/scenario-20.md) |
| 21 | Cilium Tetragon | **This is Isovalent** | 🛑 | 🔍 | [s21](scenarios/scenario-21.md) |
| 22 | Kyverno policy engine | Admission (pairs with Tetragon) | 🧩 | 🔍 | [s22](scenarios/scenario-22.md) |

⭐ Scenario 11 is the flagship network-security demo. If you only have 20
minutes, run 11, then 3 and 10.

### Honest scoping

Isovalent is a **network + runtime** platform. Scenarios that are *analysis
tools* (5, 6, 17, 19) or *supply-chain / build-time* issues (15) are not things
a CNI or eBPF runtime "blocks" — those pages say so plainly and point at the
right complementary control (admission control with Kyverno/scenario 22, image
scanning, CIS posture in Isovalent Enterprise). Selling the parts that don't fit
as if they did will cost you credibility in the room; the pages are written to
keep you honest.

---

## Repo layout

```
scenarios/            one runbook page per GOAT scenario (01-22)
policies/
  network/            CiliumNetworkPolicy / CiliumClusterwideNetworkPolicy
  dns-l7/             DNS visibility + FQDN egress allow-list building block
  tetragon/           TracingPolicy runtime detect/enforce
  egress/             Egress Gateway + transparent encryption notes
scripts/
  00-install-isovalent.sh   Cilium + Hubble + Tetragon install
  apply-all-defenses.sh     apply every policy in this repo
```

## Capability cheat-sheet

| Isovalent capability | What it gives the demo |
|----------------------|------------------------|
| Hubble (`hubble observe`, UI) | Live flow visibility, verdicts (FORWARDED/DROPPED), L7 HTTP/DNS |
| CiliumNetworkPolicy | Identity-based L3/L4 allow/deny, namespace isolation |
| L7 HTTP policy | Per-method/path allow-list, 403 on disallowed requests |
| DNS / FQDN policy | Egress allow-list by name, every lookup observable |
| CiliumClusterwideNetworkPolicy | Cluster guardrails + **host firewall** for NodePort |
| Tetragon TracingPolicy | eBPF process/file/socket/capability detection + **Sigkill/Override** |
| Egress Gateway | Fixed, attributable egress IP for exfil control |
| Transparent encryption | WireGuard/IPsec pod-to-pod, defeats on-wire sniffing |
| **Enterprise** Timescape | Historical flow retention — investigate after the pod is gone |
| **Enterprise** Hubble UI + process ancestry | Visual attack path, process trees |
| **Enterprise** policy recommendation | Auto-generate least-privilege policies from observed flows |
| **Enterprise** SIEM export | Ship Hubble/Tetragon events to Splunk/Elastic |

Sources: the scenario descriptions and commands are drawn from the upstream
[Kubernetes GOAT guide](https://github.com/madhuakula/kubernetes-goat); policy
syntax follows [Cilium](https://docs.cilium.io) and
[Tetragon](https://tetragon.io) documentation.
