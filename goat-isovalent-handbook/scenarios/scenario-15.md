# Scenario 15 — Hidden in layers

**Coverage:** 🧩 Complementary · 🔍 Identify · Control: **Registry/posture (primary) + Tetragon runtime**
**Target:** a container image hiding sensitive data in a non-top layer

## What this scenario is
A **supply-chain / image-analysis** scenario: secrets are hidden in an
intermediate image layer, discoverable by inspecting the image history. There is
no live network attack to block — the exposure is baked into the artifact.

## Where Isovalent fits (honestly, at the edges)
Isovalent is a runtime/network platform, not an image scanner. Two legitimate
connections:
1. If the hidden layer contains a binary or credential that a compromised
   container later *executes or reads*, **Tetragon** sees that at runtime
   (process exec / file open) — reuse `policies/tetragon/18-falco-equivalents.yaml`.
2. If it holds an endpoint/URL the container later beacons to, **Cilium** egress
   allow-listing blocks the call and **Hubble** records it.

## Primary fix — supply chain
The real controls are image scanning (Trivy/Grype), signing & provenance
(cosign/SLSA), and admission policies that only permit signed images (scenario
22). Do that first.

## Talk track
> "This one I won't oversell. Hidden secrets in a layer is a supply-chain
> problem — scan and sign your images, that's the fix. Where Isovalent helps is
> the *next* step: if something in that layer runs or calls home, Tetragon and
> Hubble catch the behaviour. Prevention is in your pipeline; Isovalent is the
> runtime safety net."

## Honesty
Explicitly a non-network scenario. Don't map a CNI onto it beyond the runtime
behaviour angle above.
