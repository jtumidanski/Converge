# Component Patterns

## Component Organization

```
components/
├── ui/          # shadcn/ui primitives (button, dialog, input, select, etc.)
├── common/      # Shared presentational (ErrorPage, etc.)
├── features/    # Feature-specific containers
│   ├── buckets/       # BucketCard, CreateBucketDialog, BucketStatusBadge
│   ├── policies/      # PolicyCard, CreatePolicyDialog
│   ├── users/         # UserCard, CreateUserDialog
│   ├── navigation/    # BreadcrumbBar
│   └── settings/      # ThemeToggle, PreferencesForm
├── providers/   # React context wrappers (QueryProvider)
├── data-table.tsx       # Generic data table wrapper
├── app-sidebar.tsx      # Navigation sidebar
├── detail-sidebar.tsx   # Detail view sidebar
└── theme-toggle.tsx     # Dark/light mode toggle
```

## Component Types

### Presentational Components (`ui/`, `common/`)

Pure rendering, no data fetching. Props-driven.

```tsx
// components/common/ErrorPage.tsx
interface ErrorPageProps {
  statusCode: number;
  title?: string;
  message?: string;
  showRetryButton?: boolean;
  showBackButton?: boolean;
  onRetry?: () => void;
}

export function ErrorPage({ statusCode, title, message, ...props }: ErrorPageProps) {
  return (
    <Card className="mx-auto max-w-md">
      <CardHeader>
        <CardTitle>{title || getDefaultTitle(statusCode)}</CardTitle>
      </CardHeader>
      <CardContent>{message}</CardContent>
    </Card>
  );
}

// Pre-configured variants
export function Error404Page() {
  return <ErrorPage statusCode={404} showBackButton />;
}
```

### Feature Components (`features/`)

Contains business logic, data fetching, state management.

```tsx
// components/features/bans/CreateBanDialog.tsx
interface CreateBanDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSuccess?: () => void;
  prefill?: { banType?: BanType; value?: string };
}

export function CreateBanDialog({ open, onOpenChange, onSuccess, prefill }: CreateBanDialogProps) {
  const form = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: { /* ... */ },
  });

  const onSubmit = async (values: FormValues) => {
    // Business logic, API calls, toast feedback
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <Form {...form}>
          <form onSubmit={form.handleSubmit(onSubmit)}>
            {/* Form fields */}
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
```

## Component Structure Convention

```tsx
// 1. Imports (React, libraries, components, hooks, types)
import { useState, useEffect, useMemo } from "react";
import { cn } from "@/lib/utils";

// 2. Type definitions
interface ComponentProps {
  required: string;
  optional?: number;
  children?: React.ReactNode;
}

// 3. Component definition (named export, not default)
export function Component({ required, optional = 0, children }: ComponentProps) {
  // 4. Hooks
  const [state, setState] = useState(false);

  // 5. Effects
  useEffect(() => { /* ... */ }, []);

  // 6. Event handlers
  const handleClick = () => setState(!state);

  // 7. Render
  return <div className={cn("base-class", state && "active")}>{children}</div>;
}
```

## Data Table Pattern

The generic `DataTable` wraps TanStack React Table.

```tsx
// Usage in a page
import { DataTable } from "@/components/data-table";

// Define columns
const columns: ColumnDef<Ban>[] = [
  {
    accessorKey: "attributes.value",
    header: "Value",
  },
  {
    accessorKey: "attributes.banType",
    header: "Type",
    cell: ({ row }) => <BanTypeBadge type={row.original.attributes.banType} />,
  },
  {
    id: "actions",
    cell: ({ row }) => (
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon"><MoreHorizontal /></Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuItem onClick={() => handleDelete(row.original)}>
            Delete
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    ),
  },
];

// Render
<DataTable
  columns={columns}
  data={bans}
  onRefresh={fetchBans}
  initialVisibilityState={["id"]}  // Hidden columns
  headerActions={[
    { icon: <Plus />, label: "Create Ban", onClick: () => setCreateDialogOpen(true) },
  ]}
/>
```

**DataTable Props:**
```typescript
interface DataTableProps<TData, TValue> {
  columns: ColumnDef<TData, TValue>[];
  data: TData[];
  onRefresh?: () => void;
  initialVisibilityState?: string[];     // Column IDs to hide
  headerActions?: DataTableHeaderAction[];
}
```

## Dialog Pattern

Dialogs are controlled externally via `open`/`onOpenChange`:

```tsx
// In parent page
const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
const [selectedBan, setSelectedBan] = useState<Ban | null>(null);

// Open dialog with context
const handleDeleteClick = (ban: Ban) => {
  setSelectedBan(ban);
  setDeleteDialogOpen(true);
};

// Render dialog
<DeleteBanDialog
  open={deleteDialogOpen}
  onOpenChange={setDeleteDialogOpen}
  ban={selectedBan}
  onSuccess={() => fetchBans()}  // Refresh after action
/>
```

## Loading State Pattern

Use skeleton components for content areas:

```tsx
// Skeleton for a page
function BucketPageSkeleton() {
  return (
    <div className="space-y-4 p-4">
      <Skeleton className="h-8 w-48" />
      <div className="grid grid-cols-4 gap-4">
        {Array.from({ length: 8 }).map((_, i) => (
          <Skeleton key={i} className="h-24" />
        ))}
      </div>
    </div>
  );
}

// Usage in page
if (loading) return <BucketPageSkeleton />;
```

Use `Loader2` spinner only in submit buttons:

```tsx
<Button type="submit" disabled={isSubmitting}>
  {isSubmitting ? (
    <>
      <Loader2 className="mr-2 h-4 w-4 animate-spin" />
      Creating...
    </>
  ) : (
    "Create"
  )}
</Button>
```

## Badge Pattern

For status/type indicators:

```tsx
export function BanStatusBadge({ ban }: { ban: Ban }) {
  const active = isBanActive(ban);
  return (
    <Badge variant={active ? "destructive" : "secondary"}>
      {active ? "Active" : "Expired"}
    </Badge>
  );
}
```

## Collapsible Section Pattern

For expandable content areas (e.g., bucket sections):

```tsx
<Collapsible defaultOpen>
  <CollapsibleTrigger className="flex items-center gap-2">
    <ChevronDown className="h-4 w-4" />
    <span>Objects ({items.length})</span>
  </CollapsibleTrigger>
  <CollapsibleContent>
    {/* Content */}
  </CollapsibleContent>
</Collapsible>
```

## Text Casing Rules

All user-facing interactive text must use **title case** — capitalize the first letter of each significant word.

This applies to:
- **Button labels**: "Create Bucket", "Save Changes", "Try Again"
- **Dialog titles**: "Create Bucket", "Delete Policy"
- **Badge text**: "Active", "Read Only"
- **Tab / view-switcher labels**: "Overview", "Objects", "Settings"

```tsx
// ✅ Good — title case
<Button>Create Bucket</Button>
<DialogTitle>Edit Policy</DialogTitle>
<Badge>Read Only</Badge>

// ❌ Bad — sentence case or lowercase
<Button>Create bucket</Button>
<DialogTitle>Edit policy</DialogTitle>
<Badge>Read only</Badge>
```

**Exceptions**: Prepositions and articles in the middle of a label stay lowercase ("Move to Trash", "Import from File"). First word is always capitalized regardless.

**Enum display values**: When displaying backend enum values (e.g., `"public"`, `"private"`), capitalize the first letter for display. Keep the underlying value unchanged.

```tsx
// ✅ Good — capitalize for display only
<SelectItem value="public">Public</SelectItem>
<span>{kind.charAt(0).toUpperCase() + kind.slice(1)}</span>

// ❌ Bad — raw enum value shown to user
<SelectItem value="public">public</SelectItem>
```

## Cursor Behavior

All clickable elements must show `cursor-pointer` on hover. The base `<Button>` component already includes `cursor-pointer` in its CVA definition. For custom clickable elements (styled divs, card actions, etc.), add `cursor-pointer` explicitly:

```tsx
// ✅ Good — pointer cursor on clickable elements
<div className="cursor-pointer" onClick={handleClick}>...</div>

// ❌ Bad — default cursor on clickable element
<div onClick={handleClick}>...</div>
```

## Empty State Pattern

```tsx
{data.length === 0 && !loading && (
  <div className="flex flex-col items-center justify-center py-12 text-center">
    <p className="text-muted-foreground">No items found</p>
    <Button variant="outline" className="mt-4" onClick={handleCreate}>
      <Plus className="mr-2 h-4 w-4" />
      Create First Item
    </Button>
  </div>
)}
```
