import { useMemo } from "react";
import { ProgressBar } from "@/components/common/ProgressBar";
import { useStore } from "@/lib/storage/store";
import { viewedStore } from "@/lib/storage/viewed";
import { stageLabel } from "@/lib/stageLabel";
import { strings } from "@/lib/strings";
import type { Review } from "@/types/models/review";

/**
 * ReviewProgressCell reads the same browser viewed-state the review page
 * writes, through useSyncExternalStore, so marking a file viewed updates this
 * cell in another tab without a server round trip.
 */
export function ReviewProgressCell({ review }: { review: Review }) {
  const { status, stage, totals, error } = review.attributes;
  const [viewed] = useStore(viewedStore(review.id));
  const total = totals?.files ?? 0;
  const count = useMemo(() => {
    // Only paths still plausible for this review are counted; the review page
    // prunes against the real file list, and the cell has no file list here.
    return Math.min(viewed.length, total);
  }, [viewed, total]);

  if (status === "CREATING") {
    const label = stage
      ? `${strings.statusBuilding} · ${stageLabel(stage)}`
      : strings.statusBuilding;
    return <ProgressBar value={0} label={label} indeterminate />;
  }
  if (status === "CONFLICTED" || status === "FAILED") {
    return <span className="font-mono text-xs text-destructive">{error?.code ?? status}</span>;
  }
  const percent = total === 0 ? 0 : Math.round((count / total) * 100);
  return <ProgressBar value={percent} label={`${count} of ${total} files viewed`} />;
}
