import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { strings } from "@/lib/strings";
import type { Review } from "@/types/models/review";

interface ReviewErrorPanelProps {
  review: Review;
  onDiscard: () => void;
  discarding: boolean;
}

export function ReviewErrorPanel({ review, onDiscard, discarding }: ReviewErrorPanelProps) {
  const error = review.attributes.error;
  if (!error) return null;
  const diagnostics = error.diagnostics;
  return (
    <section className="flex flex-col gap-4 rounded-md border border-destructive/40 bg-destructive/5 p-6">
      <div>
        <h2 className="text-lg font-semibold text-foreground">
          {error.code === "CONFLICT" ? strings.conflict : "This review could not be built"}
        </h2>
        <p className="mt-1 text-sm text-foreground">{error.message}</p>
      </div>
      {error.conflictingFiles && error.conflictingFiles.length > 0 ? (
        <div>
          <p className="text-sm font-medium text-foreground">Files involved</p>
          <ul className="mt-1 list-inside list-disc text-sm text-muted-foreground">
            {error.conflictingFiles.map((path) => (
              <li key={path} className="font-mono">
                {path}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      {error.appliedChanges && error.appliedChanges.length > 0 ? (
        <p className="text-sm text-muted-foreground">
          Applied before the problem: {error.appliedChanges.map((n) => `#${n}`).join(", ")}
        </p>
      ) : null}
      <Collapsible>
        <CollapsibleTrigger asChild>
          <Button variant="outline" size="sm">
            {strings.diagnostics}
          </Button>
        </CollapsibleTrigger>
        <CollapsibleContent className="mt-2 rounded-md border border-border bg-background p-3 font-mono text-xs text-muted-foreground">
          <dl className="grid grid-cols-[10rem_1fr] gap-1">
            <dt>Error code</dt>
            <dd>{error.code}</dd>
            {review.attributes.baseSha ? (
              <>
                <dt>Base SHA</dt>
                <dd>{review.attributes.baseSha}</dd>
              </>
            ) : null}
            {error.commit ? (
              <>
                <dt>Commit</dt>
                <dd>{error.commit}</dd>
              </>
            ) : null}
            {diagnostics?.strategy ? (
              <>
                <dt>Strategy</dt>
                <dd>{diagnostics.strategy}</dd>
              </>
            ) : null}
            {diagnostics?.branch ? (
              <>
                <dt>Branch</dt>
                <dd>{diagnostics.branch}</dd>
              </>
            ) : null}
            {diagnostics?.workspacePath ? (
              <>
                <dt>Workspace path</dt>
                <dd>{diagnostics.workspacePath}</dd>
              </>
            ) : null}
          </dl>
        </CollapsibleContent>
      </Collapsible>
      <div>
        <Button variant="destructive" onClick={onDiscard} disabled={discarding}>
          {discarding ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          {strings.discardReview}
        </Button>
      </div>
    </section>
  );
}
