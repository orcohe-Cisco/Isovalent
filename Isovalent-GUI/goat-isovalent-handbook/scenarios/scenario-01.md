# Scenario 1 — Sensitive keys in codebases

**Coverage:** 🛑 Block · 🔍 Identify · Control: **Cilium L7 HTTP policy + Hubble**
**Target:** `build-code` deployment, `default` namespace, port 80 (`http://127.0.0.1:1230`)

## The attack
The `build-code` web app leaks a `.git` directory. The attacker discovers
`/.git/config`, clones the repo with `git-dumper`, walks the commit history and
finds an `.env` with hardcoded AWS keys and the flag.

```bash
python3 git-dumper.py http://localhost:1230/.git k8s-goat-git
cd k8s-goat-git && git log
git checkout d7c173ad183c574109cd5c4c648ffe551755b576
cat .env        # AWS keys + flag
```

## Identify (Hubble)
Turn on L7 visibility and watch the crawl — git-dumper makes hundreds of GETs
under `/.git/`:

```bash
hubble observe --to-label app=build-code --type l7 --follow
# GET /.git/config, /.git/HEAD, /.git/objects/... -> a burst from one client
```

In the Hubble UI the service map shows one source hammering the app with HTTP
GETs — an obvious enumeration signature.

## Block (Cilium L7)
Serve the app's real paths, reject anything touching `/.git`:

```bash
kubectl apply -f policies/network/01-build-code-git-l7.yaml
```

The policy allow-lists legitimate paths; the implicit default-deny means
`/.git/*` returns **403** at the Envoy L7 proxy. Re-run git-dumper — every
object fetch is denied:

```bash
hubble observe --to-label app=build-code --type l7 --http-status 403 --follow
```

## Talk track
> "Notice we didn't need to know about the `.git` leak in advance. We allow-list
> the three paths the app actually serves, and Cilium's L7 proxy returns 403 for
> everything else — including the leak the developer forgot about. And every one
> of those denied requests is a Hubble event you can alert on."

## Limitations / honesty
The *root cause* is a secret committed to git and a `.git` dir served by the web
server — fix those too (pre-commit secret scanning, web server config). L7 policy
is defence-in-depth that contains the exposure at the network edge. Cilium does
not scan the repo contents; it controls who can reach which HTTP paths.
