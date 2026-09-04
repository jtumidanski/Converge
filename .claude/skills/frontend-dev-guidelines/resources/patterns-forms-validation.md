# Forms & Validation Patterns

## Overview

Forms use **react-hook-form** with **Zod** validation via `@hookform/resolvers/zod`. Schemas live in `lib/schemas/`, and form UI uses shadcn/ui `Form` components for consistent field rendering.

## Zod Schema Pattern

### Basic Schema

```typescript
// lib/schemas/bucket.schema.ts
import { z } from 'zod';

export const createBucketSchema = z.object({
  name: z
    .string()
    .min(1, 'Bucket name is required')
    .max(100, 'Bucket name must be 100 characters or less'),
  region: z
    .string()
    .min(1, 'Region is required'),
  quotaGb: z
    .number()
    .int('Quota must be an integer')
    .nonnegative('Quota must be non-negative'),
});

// Infer TypeScript type from schema
export type CreateBucketFormData = z.infer<typeof createBucketSchema>;

// Default values for form reset
export const createBucketDefaults: CreateBucketFormData = {
  name: '',
  region: '',
  quotaGb: 0,
};
```

### Schema with Cross-Field Validation

```typescript
// Inline in component file for form-specific logic
const formSchema = z.object({
  banType: z.nativeEnum(BanType),
  value: z.string().min(1, "Value is required"),
  permanent: z.boolean(),
  expiresAt: z.string().optional(),
}).refine((data) => {
  // IP validation for IP type bans
  if (data.banType === BanType.IP && !ipRegex.test(data.value)) {
    return false;
  }
  return true;
}, {
  message: "Invalid IP address or CIDR format",
  path: ["value"],    // ← Attach error to specific field
}).refine((data) => {
  // Expiration required for non-permanent bans
  if (!data.permanent && !data.expiresAt) return false;
  return true;
}, {
  message: "Expiration date is required for non-permanent bans",
  path: ["expiresAt"],
});
```

### Discriminated Union Schema

```typescript
// lib/schemas/policy.schema.ts
export const readPolicySchema = z.object({
  type: z.literal('read-policy'),
  resources: z.array(resourceSchema),
});

export const writePolicySchema = z.object({
  type: z.literal('write-policy'),
  resources: z.array(resourceSchema),
});

// Combined with discriminated union
export const policySchema = z.discriminatedUnion('type', [
  readPolicySchema,
  writePolicySchema,
  adminPolicySchema,
]);
```

### Reusable Validation Primitives

```typescript
export const portSchema = z
  .number()
  .int('Port must be an integer')
  .min(1, 'Port must be at least 1')
  .max(65535, 'Port must be at most 65535');

export const byteIdSchema = z
  .number()
  .int('ID must be an integer')
  .min(0, 'ID must be at least 0')
  .max(255, 'ID must be at most 255');
```

## Form Component Pattern

### Using shadcn/ui Form Components (Preferred)

```tsx
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";

export function CreateBanDialog({ open, onOpenChange, onSuccess }: Props) {
  const form = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      banType: BanType.IP,
      value: "",
      permanent: false,
    },
  });

  const isPermanent = form.watch("permanent");  // ← Watch for conditional rendering

  const onSubmit = async (values: FormValues) => {
    try {
      await bansService.createBan(mapToRequest(values));
      toast.success("Ban created successfully");
      form.reset();
      onOpenChange(false);
      onSuccess?.();
    } catch (error) {
      toast.error("Failed to create ban: " + (error instanceof Error ? error.message : "Unknown error"));
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <Form {...form}>
          <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">

            {/* Select field */}
            <FormField
              control={form.control}
              name="banType"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Ban Type</FormLabel>
                  <Select
                    onValueChange={(value) => field.onChange(Number(value))}
                    defaultValue={field.value.toString()}
                  >
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue placeholder="Select ban type" />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      {Object.entries(BanTypeLabels).map(([value, label]) => (
                        <SelectItem key={value} value={value}>{label}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FormMessage />
                </FormItem>
              )}
            />

            {/* Text input */}
            <FormField
              control={form.control}
              name="value"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Value</FormLabel>
                  <FormControl>
                    <Input placeholder="Enter value" {...field} />
                  </FormControl>
                  <FormDescription>The target to ban</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            {/* Switch (boolean) */}
            <FormField
              control={form.control}
              name="permanent"
              render={({ field }) => (
                <FormItem className="flex flex-row items-center justify-between rounded-lg border p-3">
                  <div className="space-y-0.5">
                    <FormLabel>Permanent Ban</FormLabel>
                    <FormDescription>This ban will never expire</FormDescription>
                  </div>
                  <FormControl>
                    <Switch checked={field.value} onCheckedChange={field.onChange} />
                  </FormControl>
                </FormItem>
              )}
            />

            {/* Conditional field */}
            {!isPermanent && (
              <FormField
                control={form.control}
                name="expiresAt"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Expiration Date</FormLabel>
                    <FormControl>
                      <Input type="datetime-local" {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={form.formState.isSubmitting}>
                {form.formState.isSubmitting && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                Create Ban
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
```

### Using register() (Simpler forms)

```tsx
const { register, handleSubmit, setValue, watch, reset, formState: { errors, isSubmitting } } = useForm<CreateBucketFormData>({
  resolver: zodResolver(createBucketSchema),
  defaultValues: createBucketDefaults,
});

// Text input
<Input id="name" placeholder="Enter name" {...register("name")} disabled={isSubmitting} />
{errors.name && <p className="text-sm text-destructive">{errors.name.message}</p>}

// Select (manual setValue)
<Select value={selectedRegion} onValueChange={(v) => setValue("region", v)}>
  <SelectTrigger><SelectValue placeholder="Select region" /></SelectTrigger>
  <SelectContent>
    {regions.map(r => <SelectItem key={r} value={r}>{r}</SelectItem>)}
  </SelectContent>
</Select>
```

## Cascading Dropdown Pattern

For dependent selects (e.g., Region → Zone → Subnet):

```tsx
const selectedRegion = watch("region");
const selectedZone = watch("zone");

// Filter options based on selected parent
const availableZones = useMemo(() => {
  if (!selectedRegion) return [];
  return [...new Set(
    options.filter(o => o.attributes.region === selectedRegion)
      .map(o => o.attributes.zone)
  )].sort();
}, [options, selectedRegion]);

// Reset dependent fields when parent changes
const handleRegionChange = (region: string) => {
  setValue("region", region);
  setValue("zone", "");      // ← Reset child
  setValue("subnet", "");    // ← Reset grandchild
};
```

## Error Display Pattern

```tsx
// Field-level errors (via FormMessage or manual)
{errors.name && <p className="text-sm text-destructive">{errors.name.message}</p>}

// General form errors (via toast on submit)
toast.error("Failed to create: " + (error instanceof Error ? error.message : "Unknown error"));

// Typed error handling
if (error instanceof BucketNotFoundError) {
  toast.error("Bucket no longer exists.");
} else if (error instanceof PolicyCreationError) {
  toast.error(`Created but policy failed. ID: ${error.bucketId}`);
} else {
  toast.error("An unexpected error occurred.");
}
```

## Dialog Close Behavior

- Prevent close during submission: `if (!isSubmitting) { onOpenChange(newOpen); }`
- Reset form on close: `if (!newOpen) { reset(defaults); }`
- Navigate after success: `window.location.replace('/resource/' + id)` or `onSuccess?.()`
