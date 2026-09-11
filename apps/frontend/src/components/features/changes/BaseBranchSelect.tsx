import { useMemo, useState } from "react";
import { Check, ChevronsUpDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useBranches } from "@/lib/hooks/api/useBranches";
import { useDebouncedValue } from "@/lib/hooks/useDebouncedValue";
import { strings } from "@/lib/strings";
import { cn } from "@/lib/utils";

interface BaseBranchSelectProps {
  providerId: string;
  repository: string;
  value: string;
  defaultBranch: string | undefined;
  onChange: (branch: string) => void;
  onOpenChange?: (open: boolean) => void;
}

/**
 * BaseBranchSelect is a searchable combobox rather than a plain select:
 * repositories routinely have hundreds of branches, and the endpoint pages.
 *
 * The repository's own default is pinned at the top regardless of what the
 * current page contains, and a typed value that matches nothing is still
 * offered -- a branch past the first page has to be reachable, and the backend
 * validates the name on build anyway.
 */
export function BaseBranchSelect({
  providerId,
  repository,
  value,
  defaultBranch,
  onChange,
  onOpenChange,
}: BaseBranchSelectProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const debounced = useDebouncedValue(query, 250);
  const branches = useBranches(
    providerId,
    repository,
    debounced.trim() === "" ? {} : { search: debounced.trim() },
    open,
  );

  const names = useMemo(() => {
    const fromServer = (branches.data?.items ?? []).map((b) => b.attributes.name);
    const pinned =
      defaultBranch !== undefined &&
      defaultBranch !== "" &&
      (debounced.trim() === "" || defaultBranch.includes(debounced.trim()))
        ? [defaultBranch]
        : [];
    return [...pinned, ...fromServer.filter((n) => n !== defaultBranch)];
  }, [branches.data, defaultBranch, debounced]);

  const typed = query.trim();
  const offerTyped = typed !== "" && !names.includes(typed);

  function setOpenState(next: boolean): void {
    setOpen(next);
    onOpenChange?.(next);
    if (!next) setQuery("");
  }

  function choose(branch: string): void {
    onChange(branch);
    setOpenState(false);
  }

  return (
    <div className="flex flex-col gap-1">
      <label htmlFor="base-branch" className="text-sm font-medium text-foreground">
        {strings.base}
      </label>
      <Popover open={open} onOpenChange={setOpenState}>
        <PopoverTrigger asChild>
          <Button
            id="base-branch"
            variant="outline"
            role="combobox"
            aria-expanded={open}
            aria-label={strings.base}
            className="w-64 justify-between font-mono text-sm"
          >
            <span className="truncate">{value || "—"}</span>
            <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-72 p-0" align="start">
          {/* shouldFilter=false: the server filters, and cmdk's fuzzy filter
              would drop server results that do not fuzzy-match the query. */}
          <Command shouldFilter={false}>
            <CommandInput
              placeholder={strings.filterBranchesPlaceholder}
              value={query}
              onValueChange={setQuery}
            />
            <CommandList>
              <CommandEmpty>{strings.noBranchesMatchFilter}</CommandEmpty>
              <CommandGroup>
                {names.map((name) => (
                  <CommandItem key={name} value={name} onSelect={() => choose(name)}>
                    <Check
                      className={cn("mr-2 h-4 w-4", name === value ? "opacity-100" : "opacity-0")}
                    />
                    <span className="truncate font-mono text-sm">{name}</span>
                    {name === defaultBranch ? (
                      <span className="ml-auto text-xs text-muted-foreground">
                        {strings.branchDefault}
                      </span>
                    ) : null}
                  </CommandItem>
                ))}
                {offerTyped ? (
                  <CommandItem value={`use-${typed}`} onSelect={() => choose(typed)}>
                    <span className="truncate text-sm">{strings.useTypedBranch(typed)}</span>
                  </CommandItem>
                ) : null}
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
    </div>
  );
}
