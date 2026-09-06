import { LibraryWizard } from "@/components/library/LibraryWizard";
import { POLICIES } from "@/lib/policyLibrary";

export const metadata = {
  title: "Policy Library — Isovalent Control",
};

export default function LibraryPage() {
  const cves = POLICIES.filter((p) => p.section === "cve").length;
  return (
    <div className="space-y-5">
      <header>
        <h1 className="text-[17px] font-semibold">Tetragon Policy Library</h1>
        <p className="mt-1 max-w-3xl text-sm leading-relaxed text-[color:var(--text-secondary)]">
          {POLICIES.length} TracingPolicies, organised by what they watch — including{" "}
          {cves} CVE mitigations. Every spec is copied verbatim from the upstream{" "}
          <span className="mono">cilium/tetragon</span> examples, except the CVE
          policies marked <em>authored</em>, which are written here against the
          published advisory and linked to it.
        </p>
      </header>
      <LibraryWizard />
    </div>
  );
}
