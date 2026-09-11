import { CommandInput } from "@/components/ui/command";
import { strings } from "@/lib/strings";

interface RepositorySearchProps {
  value: string;
  onChange: (value: string) => void;
  onEnter: () => void;
}

/**
 * RepositorySearch is the Command's own input, so arrow keys and Enter reach
 * the results list through cmdk's roving focus rather than a hand-rolled
 * listbox. onEnter only fires when cmdk did not already consume the key.
 */
export function RepositorySearch({ value, onChange, onEnter }: RepositorySearchProps) {
  return (
    <CommandInput
      placeholder={strings.searchRepositoriesPlaceholder}
      value={value}
      onValueChange={onChange}
      onKeyDown={(event) => {
        if (event.key === "Enter" && !event.defaultPrevented) onEnter();
      }}
    />
  );
}
