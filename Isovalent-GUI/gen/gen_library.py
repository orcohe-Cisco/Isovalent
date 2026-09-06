#!/usr/bin/env python3
"""
Builds frontend/src/lib/policyLibrary.ts from the real Tetragon example
policies plus the CVE policies we authored.

Everything in the console's library is a verbatim upstream spec — no
paraphrasing, no invented hooks — so what the SE demos is what upstream ships.
"""
import glob
import json
import os
import re
import sys

import yaml

TETRAGON = sys.argv[1] if len(sys.argv) > 1 else "/home/claude/work/tetragon"
AUTHORED = sys.argv[2] if len(sys.argv) > 2 else "/home/claude/work/gen/authored"
OUT = sys.argv[3] if len(sys.argv) > 3 else "/home/claude/work/ic/frontend/src/lib/policyLibrary.ts"

REPO_BLOB = "https://github.com/cilium/tetragon/blob/main/examples/"

SOURCE_ROOTS = [
    ("policylibrary", "examples/policylibrary"),
    ("tracingpolicy", "examples/tracingpolicy"),
    ("quickstart", "examples/quickstart"),
    ("other", "examples/other"),
]

# --------------------------------------------------------------- sections ---

SECTIONS = [
    ("cve", "CVE mitigations",
     "Policies that detect or block the exploitation of a specific published "
     "vulnerability. Each carries the CVE link and what the exploit actually does."),
    ("escape", "Container escape & namespaces",
     "The syscalls a process uses to leave its container: mount, pivot_root, "
     "ptrace, and unprivileged user-namespace creation."),
    ("privileges", "Privileges & credentials",
     "Credential changes, capability use and setuid/setgid transitions — the "
     "step between a foothold and a takeover."),
    ("execution", "Process execution",
     "What ran, from where, and with which parent. The cheapest high-value "
     "signal Tetragon produces."),
    ("files", "File access & integrity",
     "Reads and writes to sensitive paths, plus the link/symlink tricks used to "
     "redirect them."),
    ("network", "Network activity",
     "Connects, accepts, listens and datagrams, with socket tracking. Complements "
     "Hubble rather than duplicating it: this is the process behind the socket."),
    ("kernel", "Kernel, BPF & modules",
     "Module loading, BPF program loading and perf-event access — the tooling a "
     "rootkit needs."),
    ("syscalls", "Syscall tracing",
     "Broad syscall visibility. High volume; use for investigation rather than "
     "as a standing policy."),
    ("instrumentation", "Instrumentation & building blocks",
     "uprobes, USDT, stack traces, loader and option examples. These are how-to "
     "samples for writing your own policy, not security controls."),
]

# Ordered: first matching rule wins.
CLASSIFY = [
    ("cve", lambda p, n, h: "/cves/" in p or n.startswith("cve-")),
    ("escape", lambda p, n, h: any(
        k in h for k in ("sys_mount", "sys_pivot_root", "sys_ptrace", "create_user_ns"))
        or "linux-namespaces" in p or "fd_install_ns" in p),
    ("kernel", lambda p, n, h: any(
        k in h for k in ("bpf_check", "security_perf_event_alloc", "security_bpf",
                         "security_bpf_map", "security_bpf_prog",
                         "security_kernel_module_request", "security_kernel_read_file",
                         "sys_clock_settime"))),
    ("privileges", lambda p, n, h: any(
        k in h for k in ("commit_creds", "override_creds", "revert_creds", "cap_capable",
                         "security_capset", "__sys_setuid", "__sys_setgid", "sys_setuid",
                         "sys_setgid", "__sys_setreuid", "__sys_setresuid", "sys_setreuid",
                         "sys_setresuid", "sys_setfsuid"))
        or "privileges" in p or "process-credentials" in p),
    ("network", lambda p, n, h: any(
        k in h for k in ("tcp_connect", "tcp_close", "tcp_sendmsg", "tcp_set_state",
                         "sk_alloc", "sk_free", "__sk_free", "inet_csk_listen_start",
                         "ip_output", "ip_out", "__cgroup_bpf_run_filter_skb",
                         "dev_queue_xmit", "dev_hard_start_xmit", "security_socket_connect",
                         "kfree_skb_reason", "sk_filter_trim_cap", "tcp_v4_do_rcv",
                         "tcp_v6_do_rcv"))),
    ("execution", lambda p, n, h: any(
        k in h for k in ("security_bprm_check", "security_bprm_creds_from_file",
                         "sched_process_exec"))
        or "lsm_bprm" in p or p.endswith("loader.yaml") or "process-exec" in p),
    ("files", lambda p, n, h: any(
        k in h for k in ("security_file_permission", "security_file_open",
                         "security_mmap_file", "security_path_truncate", "fd_install",
                         "sys_openat", "sys_write", "sys_linkat", "sys_symlinkat",
                         "security_inode_follow_link", "security_inode_mkdir",
                         "tty_write", "security_inode_copy_up_xattr"))
        or "lsm_file_open" in p),
    ("syscalls", lambda p, n, h: any(
        k in h for k in ("sys_enter", "raw_syscalls"))
        or h and any(x.startswith("list:") for x in h)
        or "killer" in p),
]

# ------------------------------------------------------------ descriptions ---
# Written for the policies upstream ships without a comment header. Kept short
# and factual; the hook list is shown separately in the UI.
SUMMARY = {
    "tracingpolicy/cves/cve-2023-2640-overlayfs-ubuntu.yaml": "Stops an unprivileged user namespace copying up the security.capability xattr on Ubuntu overlayfs — the CVE-2023-2640 privilege escalation. Note the upstream metadata.name says 2460; that is their typo, kept verbatim.",
    "tracingpolicy/cves/cve-2024-3094-xz-ssh.yaml": "Detects sshd mapping the backdoored liblzma from the XZ Utils supply-chain compromise (CVE-2024-3094).",
    "policylibrary/bpf.yaml": "Tracks BPF program and map loading plus perf-event creation — the loading step of an eBPF rootkit.",
    "policylibrary/egress.yaml": "Reports every TCP connect leaving the cluster CIDR, so external egress is visible per process.",
    "policylibrary/library.yaml": "Enables the loader sensor, which reports shared-library loads. A building block, not a control.",
    "policylibrary/modules.yaml": "Reports kernel module load attempts, both by name request and by file read.",
    "policylibrary/sshd.yaml": "Socket-tracking example: follows accept/close on an sshd-style workload with full connection lifecycle.",
    "tracingpolicy/bpf.yaml": "Sample twin of the policy-library BPF policy: bpf_check, perf-event alloc and BPF map/prog access.",
    "tracingpolicy/datagram-with-selectors.yaml": "UDP/datagram visibility with selectors applied — sample for filtering datagram traffic.",
    "tracingpolicy/datagram-with-sock-tracking.yaml": "Datagram visibility that also tracks socket allocation and free, so events carry connection context.",
    "tracingpolicy/datagram.yaml": "Bare datagram (UDP) visibility via the cgroup skb filter hook.",
    "tracingpolicy/dev_hard_start_xmit.yaml": "Device-level transmit hook — packet egress at the driver boundary.",
    "tracingpolicy/dev_queue_xmit.yaml": "Device queue transmit hook — packet egress before the driver.",
    "tracingpolicy/fd_install_cap_changes.yaml": "fd_install with a capability-change selector: file descriptors installed by a process whose capabilities changed.",
    "tracingpolicy/fd_install_caps.yaml": "fd_install filtered on effective capabilities — which privileged processes are opening what.",
    "tracingpolicy/fd_install_ns.yaml": "fd_install filtered on Linux namespaces.",
    "tracingpolicy/fd_install_ns_host.yaml": "fd_install restricted to the host namespace — file opens outside any container.",
    "tracingpolicy/filename_monitoring.yaml": "Watches the classic sensitive paths (/etc/shadow, /boot, /root/.ssh) for read, write, truncate and mmap.",
    "tracingpolicy/filename_monitoring_filtered.yaml": "The same file monitoring with binary and path filters applied to cut the noise.",
    "tracingpolicy/killer.yaml": "Enforcer example: attaches to every syscall and uses NotifyEnforcer. Powerful and blunt — read it before applying it.",
    "tracingpolicy/kprobe_commit_creds.yaml": "Reports every credential commit — the kernel-side view of any privilege change.",
    "tracingpolicy/list-generated-ftrace.yaml": "Generated list of all ksys_ functions, used as a target list for enforcement examples.",
    "tracingpolicy/list-syscalls-generated.yaml": "Generated list of every syscall, used as a target list for enforcement examples.",
    "tracingpolicy/list-syscalls-tracepoint.yaml": "Syscall list via the raw tracepoint, rather than per-syscall kprobes.",
    "tracingpolicy/list-syscalls.yaml": "Syscall list example using a named list of calls.",
    "tracingpolicy/loader.yaml": "Enables the loader sensor: reports shared-object loads per process.",
    "tracingpolicy/lsm_bprm_check.yaml": "LSM hook on bprm_check_security — exec enforcement at the LSM boundary. Needs BPF-LSM enabled.",
    "tracingpolicy/lsm_file_open.yaml": "LSM hook on file_open. Needs BPF-LSM enabled.",
    "tracingpolicy/lsm_track_grandparent.yml": "LSM example that walks the process ancestry to the grandparent. Needs ancestor tracking enabled.",
    "tracingpolicy/open_dnsrequest.yaml": "DnsLookup action example: resolves the destination and annotates the event with the DNS name.",
    "tracingpolicy/open_geturl.yaml": "GetUrl action example: fires an HTTP request on match, for webhook-style integrations.",
    "tracingpolicy/override-security.yaml": "Override action example on security_inode_mkdir — returns an error instead of running the operation.",
    "tracingpolicy/raw_syscalls.yaml": "Every syscall via the raw tracepoint. Extremely high volume.",
    "tracingpolicy/rawtp.yaml": "Raw tracepoint on sched_process_exec — minimal exec visibility.",
    "tracingpolicy/security-socket-connect-block-others.yaml": "Allows connects to a named set of destinations and overrides everything else with an error.",
    "tracingpolicy/security-socket-connect.yaml": "Reports connects at the LSM socket_connect hook, with address arguments.",
    "tracingpolicy/security_bprm_check.yaml": "Exec visibility at security_bprm_check without exec-id correlation — the minimal exec sample.",
    "tracingpolicy/security_inode_follow_link.yaml": "Reports symlink traversal — the primitive behind most path-confusion attacks.",
    "tracingpolicy/stack_traces.yaml": "Attaches a kernel stack trace to events. Useful for investigation, expensive at volume.",
    "tracingpolicy/sys_clock_settime.yaml": "Reports clock changes — anti-forensics and certificate-validation bypass.",
    "tracingpolicy/sys_mount.yaml": "Reports mount(2). Container escapes almost always mount something first.",
    "tracingpolicy/sys_pivot_root.yaml": "Reports pivot_root(2) — changing the root filesystem out from under the container.",
    "tracingpolicy/sys_ptrace.yaml": "Reports ptrace(2) — process injection and credential theft from a sibling process.",
    "tracingpolicy/sys_setuid.yaml": "Reports setuid(2) at the syscall boundary.",
    "tracingpolicy/tcp-accept.yaml": "Inbound connection lifecycle with socket tracking: accept through close.",
    "tracingpolicy/tcp-connect-with-selectors.yaml": "Outbound connect/close/sendmsg with selectors applied.",
    "tracingpolicy/tcp-connect.yaml": "Outbound connect, close and sendmsg — per-process egress with byte counts.",
    "tracingpolicy/tcp-listen.yaml": "Reports a process starting to listen — an unexpected listener is a strong backdoor signal.",
    "tracingpolicy/tty.yaml": "Captures tty writes — what an interactive attacker typed and saw.",
    "tracingpolicy/tty_54.yaml": "tty capture variant for kernels 5.4 and older.",
    "tracingpolicy/uprobe-binaries.yaml": "uprobe attached to specific binaries — user-space instrumentation sample.",
    "tracingpolicy/uprobe-host.yaml": "uprobe restricted to host binaries.",
    "tracingpolicy/uprobe-pid.yaml": "uprobe filtered by PID.",
    "tracingpolicy/uprobe.yaml": "Minimal uprobe sample.",
    "tracingpolicy/usdt-set.yaml": "USDT probe with the Set action — user-statically-defined tracing sample.",
    "tracingpolicy/usdt.yaml": "Minimal USDT probe sample.",
    "tracingpolicy/write.yaml": "Reports write(2). The simplest possible policy; useful as a first test that Tetragon is working.",
    "quickstart/file_monitoring.yaml": "The quickstart file-monitoring policy: watches /etc/shadow and friends, alert only.",
    "quickstart/file_monitoring_enforce.yaml": "The quickstart file policy with Sigkill — kills the process that touches a sensitive file.",
    "quickstart/network_egress_cluster.yaml": "Reports TCP connects leaving the cluster CIDR. Alert only.",
    "quickstart/network_egress_cluster_enforce.yaml": "Kills processes connecting outside the cluster CIDR. The enforcement half of the quickstart pair.",
    "other/file-block-kubectl-exec-access.yaml": "Blocks a file from kubectl-exec sessions while the pod's own process keeps access — matchPIDs followForks demo.",
}

# Kubernetes Goat scenarios each policy is relevant to, for the demo narrative.
GOAT = {
    "escape": "Scenario 3 — container escape to the host",
    "cve": "Scenario 3 — container escape; Scenario 13 — kernel module load",
    "privileges": "Scenario 8 — hidden in layers; Scenario 18 — runtime escalation",
    "files": "Scenario 1 — sensitive keys in codebases; Scenario 6 — DIND exploitation",
    "network": "Scenario 7 — SSRF to cloud metadata; Scenario 10 — NodePort exposure",
    "kernel": "Scenario 13 — kernel module load",
    "execution": "Scenario 4 — attacking private registries",
}

CAVEAT_BY_SOURCE = {
    "tracingpolicy": "Upstream ships this as a sample only, with no security-observability guarantee. Evaluate it before relying on it.",
    "policylibrary": "From the upstream policy library — the documented, recommended set.",
    "quickstart": "From the upstream quickstart. Written to demo well.",
    "other": "Upstream demo policy.",
    "authored": "Authored for this console, not an upstream Tetragon example. Sources are linked.",
}

ENFORCE_ACTIONS = {"Sigkill", "Override", "NotifyEnforcer", "Signal"}


def lead_comment(raw: str) -> str:
    out = []
    for line in raw.split("\n"):
        if line.startswith("#"):
            out.append(line.lstrip("#").rstrip())
        elif line.strip() == "" and out:
            out.append("")
        else:
            break
    while out and not out[-1].strip():
        out.pop()
    return "\n".join(l[1:] if l.startswith(" ") else l for l in out).strip()


def hooks_of(spec):
    hooks = []
    for grp in ("kprobes", "tracepoints", "uprobes", "lsmhooks", "usdts"):
        for h in (spec.get(grp) or []):
            hooks.append(h.get("call") or h.get("event") or h.get("symbol") or grp)
    return hooks


def actions_of(spec):
    return sorted(set(re.findall(r'action:\s*"?(\w+)', yaml.dump(spec))))


def slug(s):
    return re.sub(r"[^a-z0-9]+", "-", s.lower()).strip("-")


def arg_types(spec):
    types = set()
    for grp in ("kprobes", "tracepoints", "uprobes", "lsmhooks"):
        for h in (spec.get(grp) or []):
            for a in (h.get("args") or []):
                if a.get("type"):
                    types.add(a["type"])
    return types


# Upstream files these where the hook-based classifier would not.
SECTION_OVERRIDE = {
    "policylibrary/privileges/privileges-raise.yaml": "privileges",
    "tracingpolicy/open_dnsrequest.yaml": "instrumentation",
    "tracingpolicy/open_geturl.yaml": "instrumentation",
    "tracingpolicy/stack_traces.yaml": "instrumentation",
    "tracingpolicy/loader.yaml": "instrumentation",
}


def classify(path, name, hooks):
    if path in SECTION_OVERRIDE:
        return SECTION_OVERRIDE[path]
    for sid, fn in CLASSIFY:
        try:
            if fn(path, name, hooks):
                return sid
        except Exception:
            pass
    return "instrumentation"


def collect():
    entries = []
    seen_paths = set()

    def add(fspath, relpath, source):
        raw = open(fspath).read()
        try:
            docs = [d for d in yaml.safe_load_all(raw) if isinstance(d, dict)]
        except Exception as e:
            print("  skip (parse error):", relpath, e, file=sys.stderr)
            return
        policies = [d for d in docs
                    if d.get("kind") in ("TracingPolicy", "TracingPolicyNamespaced")]
        if not policies:
            return
        doc = lead_comment(raw)
        for i, d in enumerate(policies):
            spec = d.get("spec") or {}
            meta = d.get("metadata") or {}
            name = meta.get("name") or os.path.basename(relpath)
            hooks = hooks_of(spec)
            acts = actions_of(spec)
            section = classify(relpath, name, hooks)
            eid = slug(relpath.rsplit(".", 1)[0])
            if len(policies) > 1:
                eid = f"{eid}-{i + 1}"
            ann = meta.get("annotations") or {}
            summary = (
                ann.get("description")
                or SUMMARY.get(relpath)
                or (doc.split("\n")[0] if doc else "")
                or f"Tetragon policy hooking {', '.join(hooks[:3])}."
            )
            entries.append({
                "id": eid,
                "name": name,
                "title": name.replace("-", " ").replace(".", " ").strip().capitalize(),
                "source": source,
                "path": relpath,
                "url": ann.get("url") or (REPO_BLOB + relpath if source != "authored" else ""),
                "section": section,
                "summary": summary,
                "doc": doc,
                "hooks": hooks,
                "actions": acts,
                "enforcing": bool(set(acts) & ENFORCE_ACTIONS),
                "caveat": CAVEAT_BY_SOURCE[source],
                **({"goat": GOAT[section]} if section in GOAT else {}),
                "kind": d["kind"],
                "argTypes": sorted(arg_types(spec)),
                "spec": spec,
            })

    for source, root in SOURCE_ROOTS:
        base = os.path.join(TETRAGON, root)
        for f in sorted(glob.glob(base + "/**/*.y*ml", recursive=True)):
            rel = os.path.relpath(f, os.path.join(TETRAGON, "examples"))
            if rel in seen_paths:
                continue
            seen_paths.add(rel)
            add(f, rel, source)

    for f in sorted(glob.glob(AUTHORED + "/*.yaml")):
        add(f, "authored/" + os.path.basename(f), "authored")

    return entries


def ts_literal(v, indent=2):
    return json.dumps(v, indent=indent, ensure_ascii=False)


def main():
    entries = collect()
    by_section = {}
    for e in entries:
        by_section.setdefault(e["section"], []).append(e["id"])

    # CVE policies first inside their section, then documented ones.
    order = {sid: i for i, (sid, _, _) in enumerate(SECTIONS)}
    entries.sort(key=lambda e: (order[e["section"]], 0 if e["source"] in ("authored", "policylibrary") else 1, e["name"]))

    header = f'''/**
 * The Tetragon policy library.
 *
 * Every entry here is a real TracingPolicy: the `spec` is copied verbatim from
 * the upstream cilium/tetragon examples, or — for the CVE policies marked
 * source "authored" — written here against the published advisory and linked
 * to it. Nothing in this file is a paraphrase, so what the console applies is
 * exactly what upstream ships.
 *
 *   examples/policylibrary   -> documented, recommended policies
 *   examples/tracingpolicy   -> samples; upstream gives no security guarantee
 *   examples/tracingpolicy/cves -> per-CVE mitigations
 *   examples/quickstart      -> the demo pair (monitor + enforce)
 *   examples/other           -> misc demos
 *
 * Generated by gen/gen_library.py — {len(entries)} policies. Do not edit by hand.
 */

export type SectionId =
{chr(10).join(f'  | "{sid}"' for sid, _, _ in SECTIONS)};

export type PolicySource =
  | "policylibrary"
  | "tracingpolicy"
  | "quickstart"
  | "other"
  | "authored";

/** Alert = Post everywhere. Block = Sigkill/Override where the policy supports it. */
export type PolicyMode = "alert" | "block";

export interface LibraryPolicy {{
  id: string;
  /** metadata.name as upstream ships it. Collisions are real — see uniqueName(). */
  name: string;
  title: string;
  source: PolicySource;
  /** Path inside cilium/tetragon examples/. */
  path: string;
  url: string;
  section: SectionId;
  summary: string;
  /** The upstream leading comment block, verbatim. */
  doc: string;
  hooks: string[];
  actions: string[];
  /** Already contains Sigkill/Override/NotifyEnforcer as shipped. */
  enforcing: boolean;
  caveat: string;
  goat?: string;
  kind: "TracingPolicy" | "TracingPolicyNamespaced";
  argTypes: string[];
  spec: Record<string, unknown>;
}}

export interface Section {{
  id: SectionId;
  label: string;
  blurb: string;
}}

export const SECTIONS: Section[] = {ts_literal([{"id": s, "label": l, "blurb": b} for s, l, b in SECTIONS])};

export const POLICIES: LibraryPolicy[] = {ts_literal(entries)};

export const POLICIES_BY_ID: Record<string, LibraryPolicy> = Object.fromEntries(
  POLICIES.map((p) => [p.id, p]),
);

/** Free-text search across the fields someone would actually search by. */
export function matchesQuery(p: LibraryPolicy, query: string): boolean {{
  const q = query.trim().toLowerCase();
  if (!q) return true;
  const hay = [p.title, p.name, p.summary, p.doc, p.path, p.goat ?? "", ...p.hooks]
    .join(" ")
    .toLowerCase();
  return q.split(/\\s+/).every((t) => hay.includes(t));
}}
'''
    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    with open(OUT, "w") as f:
        f.write(header)

    print(f"wrote {OUT}  ({len(entries)} policies, {os.path.getsize(OUT)} bytes)")
    for sid, label, _ in SECTIONS:
        ids = by_section.get(sid, [])
        print(f"  {label:36s} {len(ids)}")


if __name__ == "__main__":
    main()
