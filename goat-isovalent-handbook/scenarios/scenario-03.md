# Scenario 3 — SSRF in the Kubernetes world

**Coverage:** 🛑 Block · 🔍 Identify · Control: **Cilium egress deny (metadata CIDR + FQDN/identity allow-list)**
**Target:** `build-code` app, reaches `169.254.169.254` and internal `metadata-db`

## The attack
An SSRF lets the app make server-side requests. The attacker pivots to:
- the cloud instance metadata service `http://169.254.169.254/latest/meta-data/`
- an internal microservice `http://metadata-db/latest/secrets/kubernetes-goat`

```bash
# via the vulnerable param the server fetches:
http://169.254.169.254/latest/meta-data/iam/security-credentials/
http://metadata-db/latest/secrets/kubernetes-goat   # base64 flag
```

## Identify (Hubble)
The SSRF turns the app into a client for destinations it should never contact.
Both stand out in Hubble:

```bash
hubble observe --from-label app=build-code --to-ip 169.254.169.254 --follow
hubble observe --from-label app=build-code --to-label app=metadata-db --follow
```

A workload suddenly talking to the link-local metadata IP is one of the
highest-signal cloud-native detections there is.

## Block (Cilium egress)
```bash
kubectl apply -f policies/network/03-ssrf-block-metadata.yaml
```
This does two things: (1) a **cluster-wide hard deny** of `169.254.169.254/32`
for all non-system pods (deny rules always win), and (2) a per-app egress
allow-list so `build-code` can only reach DNS + its one legitimate dependency —
so the pivot to `metadata-db` also drops.

Re-run the SSRF: metadata fetch and internal pivot both show DROPPED.

## Talk track
> "SSRF is the app doing the attacker's networking for it. We can't un-write the
> bug, but we decide where that app is *allowed* to send packets. A blanket deny
> on the metadata IP plus an egress allow-list means the SSRF resolves to a
> dropped flow — and a Hubble alert — instead of cloud credentials."

## Limitations / honesty
Best paired with cloud-level metadata protections (IMDSv2 / hop limit on AWS).
Cilium enforces the network boundary; it does not fix the SSRF parsing bug in
the app.
