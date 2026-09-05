import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { strings } from "@/lib/strings";
import type { Provider } from "@/types/models/provider";

interface ProviderPickerProps {
  providers: Provider[];
  value: string | undefined;
  onChange: (providerId: string) => void;
  loading?: boolean;
}

export function ProviderPicker({
  providers,
  value,
  onChange,
  loading = false,
}: ProviderPickerProps) {
  if (loading) {
    return <Skeleton className="h-9 w-64" />;
  }
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor="provider-picker" className="text-sm font-medium text-foreground">
        {strings.provider}
      </label>
      <Select {...(value !== undefined ? { value } : {})} onValueChange={onChange}>
        <SelectTrigger id="provider-picker" className="w-64">
          <SelectValue placeholder={`Select a ${strings.provider.toLowerCase()}`} />
        </SelectTrigger>
        <SelectContent>
          {providers.map((provider) => (
            <SelectItem key={provider.id} value={provider.id}>
              {provider.attributes.displayName} ({provider.attributes.kind})
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
