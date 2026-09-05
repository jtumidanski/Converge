import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { EmptyState } from "@/components/common/EmptyState";
import { shortSha, type Change } from "@/types/models/change";

interface ChangeTableProps {
  changes: Change[];
  loading: boolean;
  isSelected: (n: number) => boolean;
  onToggle: (change: Change) => void;
}

function formatDate(value: string | null): string {
  return value ? new Date(value).toLocaleDateString() : "—";
}

export function ChangeTable({ changes, loading, isSelected, onToggle }: ChangeTableProps) {
  if (loading) {
    return (
      <div className="space-y-2">
        {[0, 1, 2, 3, 4].map((row) => (
          <Skeleton key={row} className="h-10 w-full" />
        ))}
      </div>
    );
  }
  if (changes.length === 0) {
    return <EmptyState title="No merged PRs/MRs" description="Nothing matches this base branch and search." />;
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-10" />
          <TableHead className="w-20">Number</TableHead>
          <TableHead>Title</TableHead>
          <TableHead>Author</TableHead>
          <TableHead>Merged</TableHead>
          <TableHead>Branches</TableHead>
          <TableHead>Landing</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {changes.map((change) => {
          const { number, title, author, mergedAt, sourceBranch, targetBranch, landingSha } = change.attributes;
          return (
            <TableRow key={change.id}>
              <TableCell>
                <Checkbox
                  checked={isSelected(number)}
                  onCheckedChange={() => onToggle(change)}
                  aria-label={`Select #${number}`}
                />
              </TableCell>
              <TableCell className="font-mono text-sm">#{number}</TableCell>
              <TableCell className="font-medium">{title}</TableCell>
              <TableCell className="text-muted-foreground">{author}</TableCell>
              <TableCell className="text-muted-foreground">{formatDate(mergedAt)}</TableCell>
              <TableCell className="text-muted-foreground">
                {sourceBranch} to {targetBranch}
              </TableCell>
              <TableCell className="font-mono text-xs text-muted-foreground">{shortSha(landingSha)}</TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}
