import type { Metadata } from "next";
import Link from "next/link";
import "./globals.css";
import { AppShell } from "@/components/AppShell";
import { CommandPalette } from "@/components/CommandPalette";
import { NavLinks } from "@/components/NavLinks";
import { VersionBar } from "@/components/VersionBar";

export const metadata: Metadata = {
  title: "Isovalent Control",
  description:
    "Unified console for Cilium, Hubble and Tetragon — observability, policy and runtime security.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body className="min-h-screen antialiased">
        <AppShell>
          <div className="flex min-h-screen">
            <aside
              className="hairline-r sticky top-0 flex h-screen w-60 shrink-0 flex-col"
              style={{ background: "var(--surface-1)" }}
            >
              <Link
                href="/"
                className="flex items-center gap-2.5 px-5 py-5 transition-opacity duration-200 hover:opacity-80"
              >
                <span
                  className="inline-block h-2.5 w-2.5 rounded-full"
                  style={{
                    background: "var(--accent)",
                    boxShadow: "0 0 14px 2px rgba(57,135,229,0.55)",
                  }}
                />
                <span className="text-[13px] font-semibold tracking-[0.02em]">
                  Isovalent Control
                </span>
              </Link>
              <div className="flex-1 overflow-y-auto pb-4">
                <NavLinks />
              </div>
              <div className="hairline-t">
                <VersionBar />
              </div>
            </aside>
            <main className="min-w-0 flex-1 px-7 py-6">{children}</main>
          </div>
          <CommandPalette />
        </AppShell>
      </body>
    </html>
  );
}
