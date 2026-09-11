import { Suspense, lazy, useLayoutEffect, useRef } from "react";
import { ErrorBanner } from "@/components/common/ErrorBanner";
import { Skeleton } from "@/components/ui/skeleton";
import { FileFooter } from "@/components/features/review/FileFooter";
import { FileHeader } from "@/components/features/review/FileHeader";
import { messageFor } from "@/lib/api/errors";
import { strings } from "@/lib/strings";
import type { ReviewFileDiff } from "@/types/models/reviewFile";

// Shiki is heavy; keep the diff renderer out of the initial bundle.
const FileDiff = lazy(async () => ({
  default: (await import("@/components/features/review/FileDiff")).FileDiff,
}));

interface DiffPaneProps {
  fileDiff: ReviewFileDiff | undefined;
  loading: boolean;
  error?: unknown;
  onRetry?: () => void;
  href: string | undefined;
  providerName: string;
  viewed: boolean;
  onToggleViewed: () => void;
  index: number;
  total: number;
  nextName: string | undefined;
  isLast?: boolean;
  onNext: () => void;
}

export function DiffPane({
  fileDiff,
  loading,
  error,
  onRetry,
  href,
  providerName,
  viewed,
  onToggleViewed,
  index,
  total,
  nextName,
  isLast = false,
  onNext,
}: DiffPaneProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const path = fileDiff?.attributes.path;

  // Selecting a file must start at the top of that file, not wherever the
  // previous file was scrolled to (FR-37).
  useLayoutEffect(() => {
    if (scrollRef.current) scrollRef.current.scrollTop = 0;
  }, [path]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-auto">
        {fileDiff ? (
          <FileHeader
            path={fileDiff.attributes.path}
            status={fileDiff.attributes.status}
            additions={fileDiff.attributes.additions}
            deletions={fileDiff.attributes.deletions}
            href={href}
            providerName={providerName}
            viewed={viewed}
            onToggleViewed={onToggleViewed}
          />
        ) : null}
        <div className="p-3">
          {error ? (
            <ErrorBanner
              title={strings.couldNotLoadFileDiff}
              detail={messageFor(error, "Try again in a moment.")}
              {...(onRetry ? { onRetry } : {})}
            />
          ) : loading || !fileDiff ? (
            <div className="space-y-2">
              {Array.from({ length: 12 }, (_, row) => (
                <Skeleton key={row} className="h-4 w-full" />
              ))}
            </div>
          ) : (
            <Suspense fallback={<Skeleton className="h-96 w-full" />}>
              <FileDiff file={fileDiff} />
            </Suspense>
          )}
        </div>
      </div>
      {total > 0 ? (
        <FileFooter
          index={index}
          total={total}
          nextName={nextName}
          isLast={isLast}
          onNext={onNext}
        />
      ) : null}
    </div>
  );
}
