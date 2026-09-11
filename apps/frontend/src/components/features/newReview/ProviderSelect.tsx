import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { strings } from "@/lib/strings";
import type { Provider } from "@/types/models/provider";

interface ProviderSelectProps {
  providers: Provider[];
  value: string | undefined;
  onChange: (providerId: string) => void;
  loading?: boolean;
}

/** ProviderSelect is the drawer's provider picker (was features/providers/ProviderPicker). */
export function ProviderSelect({
  providers,
  value,
  onChange,
  loading = false,
}: ProviderSelectProps) {
  if (loading) return <Skeleton className="h-9 w-full" />;
  return (
    <Select {...(value !== undefined ? { value } : {})} onValueChange={onChange}>
      <SelectTrigger id="provider-select" className="w-full">
        <SelectValue placeholder={strings.selectAProvider} />
      </SelectTrigger>
      <SelectContent>
        {providers.map((provider) => (
          <SelectItem key={provider.id} value={provider.id}>
            <span className="flex items-center gap-2">
              <Badge variant="outline">{provider.attributes.kind}</Badge>
              {provider.attributes.displayName}
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
