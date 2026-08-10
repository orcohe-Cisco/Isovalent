import type { Metadata } from "next";
import Link from "next/link";
import "./globals.css";
import { NavLinks } from "@/components/NavLinks";
import { StatusBar } from "@/components/StatusBar";

export const metadata: Metadata = {
  title: "Isovalent Control",
  description:
    "Unified GUI for Cilium, Hubble and Tetragon — observability, policy and runtime security.",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body className="min-h-screen antialiased">
        <div className="flex min-h-screen">
          <aside className="flex w-56 shrink-0 flex-col border-r border-neutral-800 bg-[color:var(--surface-1)]">
            <Link href="/" className="flex items-center gap-2 px-5 py-5">
              <span className="inline-block h-3 w-3 rounded-full bg-series-blue shadow-[0_0_12px_2px_rgba(57,135,229,0.6)]" />
              <span className="text-sm font-semibold tracking-wide">
                ISOVALENT&nbsp;CONTROL
              </span>
            </Link>
            <NavLinks />
            <div className="mt-auto px-5 py-4 text-[11px] leading-relaxed text-neutral-500">
              Cilium · Hubble · Tetragon
              <br />
              community edition
            </div>
          </aside>
          <main className="min-w-0 flex-1 p-6">
            <StatusBar />
            {children}
          </main>
        </div>
      </body>
    </html>
  );
}
