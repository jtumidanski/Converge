import { useState } from "react";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/common/EmptyState";
import { ErrorBanner } from "@/components/common/ErrorBanner";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { messageFor } from "@/lib/api/errors";
import { useDeleteUserProvider } from "@/lib/hooks/api/useUserProviders";
import type { UserProvider } from "@/types/models/auth";

interface UserProviderListProps {
  providers: UserProvider[];
  /** loading is the query's isLoading -- a first load only, never a background refetch. */
  loading: boolean;
  error?: unknown;
  onRetry: () => void;
  onEdit: (provider: UserProvider) => void;
}

const kindLabel: Record<UserProvider["kind"], string> = {
  github: "GitHub",
  gitlab: "GitLab",
};

/** body renders exactly one of error, loading, empty, or the table -- in that order. */
export function UserProviderList({
  providers,
  loading,
  error,
  onRetry,
  onEdit,
}: UserProviderListProps) {
  if (error !== undefined) {
    return (
      <ErrorBanner
        title="Could not load providers"
        detail={messageFor(error, "Try again in a moment.")}
        onRetry={onRetry}
      />
    );
  }

  if (loading) {
    return (
      <div className="space-y-2">
        {[0, 1, 2].map((row) => (
          <Skeleton key={row} className="h-10 w-full" />
        ))}
      </div>
    );
  }

  if (providers.length === 0) {
    return (
      <EmptyState
        title="No providers yet"
        description="Add a provider above to start reviewing its pull or merge requests."
      />
    );
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Kind</TableHead>
          <TableHead>Display name</TableHead>
          <TableHead>Base URL</TableHead>
          <TableHead>Token</TableHead>
          <TableHead className="text-right">Actions</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {providers.map((provider) => (
          <UserProviderRow key={provider.id} provider={provider} onEdit={onEdit} />
        ))}
      </TableBody>
    </Table>
  );
}

function UserProviderRow({
  provider,
  onEdit,
}: {
  provider: UserProvider;
  onEdit: (provider: UserProvider) => void;
}) {
  const [confirming, setConfirming] = useState(false);
  const [deleteError, setDeleteError] = useState<unknown>(undefined);
  const del = useDeleteUserProvider();

  async function confirmDelete() {
    setConfirming(false);
    setDeleteError(undefined);
    try {
      await del.mutateAsync(provider.id);
    } catch (error: unknown) {
      setDeleteError(error);
    }
  }

  return (
    <>
      <TableRow>
        <TableCell>{kindLabel[provider.kind]}</TableCell>
        <TableCell className="font-medium text-foreground">{provider.displayName}</TableCell>
        <TableCell className="text-muted-foreground">{provider.baseUrl || "—"}</TableCell>
        <TableCell>
          {/* Only the four-character tail the server sends; there is nothing else to show. */}
          <span className="font-mono text-muted-foreground">•••• {provider.tokenLast4}</span>
        </TableCell>
        <TableCell className="text-right">
          {confirming ? (
            <div className="flex justify-end gap-2">
              <Button variant="outline" size="sm" autoFocus onClick={() => setConfirming(false)}>
                Cancel
              </Button>
              <Button
                variant="destructive"
                size="sm"
                disabled={del.isPending}
                onClick={() => void confirmDelete()}
              >
                {del.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                Confirm delete
              </Button>
            </div>
          ) : (
            <div className="flex justify-end gap-2">
              <Button
                variant="outline"
                size="sm"
                aria-label={`Edit ${provider.displayName}`}
                onClick={() => onEdit(provider)}
              >
                Edit
              </Button>
              <Button
                variant="outline"
                size="sm"
                aria-label={`Delete ${provider.displayName}`}
                onClick={() => setConfirming(true)}
              >
                Delete
              </Button>
            </div>
          )}
        </TableCell>
      </TableRow>
      {deleteError !== undefined ? (
        <TableRow>
          <TableCell colSpan={5}>
            <ErrorBanner
              title="Could not delete provider"
              detail={messageFor(deleteError, "Try again in a moment.")}
            />
          </TableCell>
        </TableRow>
      ) : null}
    </>
  );
}
