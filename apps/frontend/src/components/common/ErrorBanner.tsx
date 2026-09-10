import { Button } from "@/components/ui/button";

interface ErrorBannerProps {
  title: string;
  detail?: string;
  onRetry?: () => void;
}

export function ErrorBanner({ title, detail, onRetry }: ErrorBannerProps) {
  return (
    <div
      role="alert"
      className="flex items-start justify-between gap-4 rounded-md border border-destructive/40 bg-destructive/10 p-4"
    >
      <div>
        <p className="text-sm font-medium text-foreground">{title}</p>
        {detail ? <p className="mt-1 text-sm text-muted-foreground">{detail}</p> : null}
      </div>
      {onRetry ? (
        <Button variant="outline" size="sm" onClick={onRetry}>
          Try again
        </Button>
      ) : null}
    </div>
  );
}
