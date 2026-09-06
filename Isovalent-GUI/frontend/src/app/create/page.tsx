import { PolicyComposer } from "@/components/create/PolicyComposer";
import { PageHeader } from "@/components/ui/PageHeader";

export const metadata = { title: "Create Policy — Isovalent Control" };

export default function CreatePage() {
  return (
    <>
      <PageHeader
        title="Create Policy"
        subtitle="Network and runtime policy in one place. Load what the cluster already has, edit it, dry run it against real traffic or real events, then apply."
      />
      <PolicyComposer />
    </>
  );
}
