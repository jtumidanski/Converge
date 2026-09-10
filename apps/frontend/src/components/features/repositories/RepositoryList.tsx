import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState } from "@/components/common/EmptyState";
import type { Repository } from "@/types/models/repository";

interface RepositoryListProps {
  repositories: Repository[];
  loading: boolean;
  onSelect: (repository: Repository) => void;
}

export function RepositoryList({ repositories, loading, onSelect }: RepositoryListProps) {
  if (loading) {
    return (
      <div className="space-y-2">
        {[0, 1, 2, 3].map((row) => (
          <Skeleton key={row} className="h-10 w-full" />
        ))}
      </div>
    );
  }
  if (repositories.length === 0) {
    return (
      <EmptyState
        title="No repositories"
        description="This token cannot see any repositories on this provider."
      />
    );
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Repository</TableHead>
          <TableHead>Namespace</TableHead>
          <TableHead>Default branch</TableHead>
          <TableHead className="w-24" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {repositories.map((repository) => (
          <TableRow key={repository.id}>
            <TableCell className="font-medium">{repository.id}</TableCell>
            <TableCell className="text-muted-foreground">
              {repository.attributes.namespace}
            </TableCell>
            <TableCell>{repository.attributes.defaultBranch}</TableCell>
            <TableCell>
              <Button size="sm" variant="outline" onClick={() => onSelect(repository)}>
                Select
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
