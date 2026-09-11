import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { strings } from "@/lib/strings";
import type { IncludedChange } from "@/types/models/review";

function mergedLabel(value: string | null): string {
  if (value === null) return "—";
  const ms = Date.parse(value);
  return Number.isNaN(ms) ? "—" : new Date(ms).toLocaleDateString();
}

/** IncludedChangesPopover keeps the per-change detail out of the status line. */
export function IncludedChangesPopover({ included }: { included: IncludedChange[] }) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="sm" className="h-6 px-1.5 text-xs">
          {strings.details} ▾
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[26rem] p-0">
        <ul className="divide-y divide-border">
          {included.map((change) => (
            <li key={change.number} className="flex flex-col gap-0.5 px-3 py-2">
              <a
                href={change.webUrl}
                target="_blank"
                rel="noreferrer"
                className="flex items-baseline gap-1.5 hover:underline"
              >
                <span className="shrink-0 font-mono text-xs text-muted-foreground">
                  #{change.number}
                </span>
                <span className="truncate text-sm font-medium">{change.title}</span>
              </a>
              <span className="text-xs text-muted-foreground">
                {change.author} · {mergedLabel(change.mergedAt)} · {change.strategy}
              </span>
            </li>
          ))}
        </ul>
      </PopoverContent>
    </Popover>
  );
}
