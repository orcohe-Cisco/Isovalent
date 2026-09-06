# Tuning a noisy policy

A runtime policy that fires four thousand times an hour is not a detection. It
is a rule that has not been tuned yet, and the usual response — turning it off,
or leaving it in alert mode forever — is how runtime security ends up as a tab
nobody opens.

This is the workflow the Exclusions tab exists to support.

## Why it is not part of writing the policy

You cannot know what to exclude at the moment you author a rule. The
information does not exist yet. It arrives days later, in the form of a hit
count against a process you had not thought about — your log shipper, a backup
agent, the CSI driver.

So policy creation has no exclusion step at all. You apply the policy in alert
mode, let it run, and then work backwards from what actually happened.

## The five mechanisms

Tetragon can express an exclusion five ways, and only five. The console offers
exactly those, because an exclusion that silently does nothing is worse than no
exclusion — you stop looking.

| Dimension | Mechanism | What it does |
| --- | --- | --- |
| Process | `matchBinaries` `NotIn` | Filtered in the kernel; the event is never generated. The most effective of the five. |
| Parent process | `matchParentBinaries` `NotIn` | Exempts everything a known-good launcher spawns, rather than listing each child. |
| File path | `matchArgs` `NotPrefix` | Only on hooks that take a file or string argument, at the index the value came from. |
| Pod label | `podSelector` `matchExpressions` `NotIn` | The pod is not selected at all — so this suppresses enforcement, not just events. |
| Container | `containerSelector` `matchExpressions` `NotIn` | Exempts a sidecar by name across every pod. The istio-proxy case. |

The tab also shows user, namespace, workload, pod, node and hook breakdowns.
Those are diagnostic only, and it says so: Tetragon has no user-based selector,
and it scopes namespaces by *including* them via `TracingPolicyNamespaced`
rather than excluding them.

## The argument index

A `matchArgs` `NotPrefix` is written against a specific hook argument position.
Get it wrong and it matches nothing — no error, no warning, just a policy that
carries on firing while looking like it was tuned.

The console takes the index from the observed hit rather than guessing, which
is why path exclusions are offered from real events and not from a text box.

## Contradictions

If a selector already constrains an argument index — say `Prefix: /etc/` — the
console refuses to add a `NotPrefix` on the same index and tells you why. Two
`matchArgs` clauses on one index contradict each other, and Tetragon resolves
that in a way nobody predicts correctly. Narrow the existing clause instead.

## The loop

1. **Apply in alert mode.** Always.
2. **Wait.** Ten minutes on a busy cluster, longer on a quiet one.
3. **Open Exclusions.** Sort by hits. The top row is usually one process.
4. **Tick the legitimate values.** The count next to each one is how many times
   it fired; the red chip is how many of those were enforcement actions.
   Excluding something with a non-zero enforced count is a bigger decision.
5. **Preview.** You get the notes — which selectors the exclusion reached and
   which it could not — and a field-level diff.
6. **Apply.** The exclusion is merged into the live policy (merged, not
   duplicated: approving the same exclusion twice is a no-op), and the counters
   reset.
7. **Watch.** If the exclusion worked, the policy goes quiet. If it did not,
   you will know within minutes rather than assuming.
8. **Then enforce.** Dry-run the enforcing version against stored history
   first: it tells you exactly how many processes it would have killed, and
   which. If one binary accounts for most of them, go back to step 4.

## When exclusion is the wrong answer

If a policy's hits are spread evenly across dozens of unrelated processes, it
is probably matching something too broad — a hook that fires on normal
behaviour rather than on the thing you care about. No amount of excluding fixes
that. Narrow the hook, or drop the policy and pick a more specific one from the
library.

The Assistant will say this too, if you ask it. It is instructed to prefer
narrowing over disabling, and to say plainly when the evidence is insufficient
rather than inventing a recommendation.
