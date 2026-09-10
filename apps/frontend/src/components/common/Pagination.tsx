import { Button } from "@/components/ui/button";

interface PaginationProps {
  page: number;
  hasNext: boolean;
  onChange: (page: number) => void;
  disabled?: boolean;
}

export function Pagination({ page, hasNext, onChange, disabled = false }: PaginationProps) {
  return (
    <nav className="flex items-center justify-end gap-2" aria-label="Pagination">
      <Button
        variant="outline"
        size="sm"
        disabled={disabled || page <= 1}
        onClick={() => onChange(page - 1)}
      >
        Previous
      </Button>
      <span className="text-sm text-muted-foreground">Page {page}</span>
      <Button
        variant="outline"
        size="sm"
        disabled={disabled || !hasNext}
        onClick={() => onChange(page + 1)}
      >
        Next
      </Button>
    </nav>
  );
}
