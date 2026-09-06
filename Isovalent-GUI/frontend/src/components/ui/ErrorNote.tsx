export function ErrorNote({ error }: { error?: string | null }) {
  if (!error) return null;
  return (
    <div
      className="mb-4 rounded-lg px-3.5 py-2.5 text-[13px] leading-relaxed"
      style={{ background: "rgba(230,103,103,0.12)", color: "#f0a3a3" }}
      role="alert"
    >
      {error}
    </div>
  );
}
