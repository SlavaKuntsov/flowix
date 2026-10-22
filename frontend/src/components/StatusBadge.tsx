import type { VideoStatus } from "@/lib/api";

const MAP: Record<VideoStatus, { label: string; cls: string }> = {
  ready: {
    label: "Ready",
    cls: "bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300",
  },
  processing: {
    label: "Processing",
    cls: "bg-amber-100 text-amber-800 dark:bg-amber-500/15 dark:text-amber-300",
  },
  uploaded: {
    label: "Uploaded",
    cls: "bg-sky-100 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300",
  },
  failed: {
    label: "Failed",
    cls: "bg-red-100 text-red-700 dark:bg-red-500/15 dark:text-red-300",
  },
};

export default function StatusBadge({ status, className = "" }: { status: VideoStatus; className?: string }) {
  const s = MAP[status] ?? MAP.uploaded;
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium ${s.cls} ${className}`}>
      <span className="size-1.5 rounded-full bg-current opacity-70" />
      {s.label}
    </span>
  );
}
