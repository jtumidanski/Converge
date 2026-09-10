import { useEffect, useState } from "react";
import { Input } from "@/components/ui/input";

interface ChangeSearchProps {
  value: string;
  onChange: (value: string) => void;
}

/** ChangeSearch debounces keystrokes by 300 ms before querying. */
export function ChangeSearch({ value, onChange }: ChangeSearchProps) {
  // Re-derive the draft from `value` during render rather than syncing via
  // useEffect (react-hooks/set-state-in-effect), per useSelection.ts and
  // SelectRepositoryPage.tsx's precedent.
  const [state, setState] = useState<{ source: string; draft: string }>(() => ({
    source: value,
    draft: value,
  }));
  const draft = state.source === value ? state.draft : value;
  if (state.source !== value) {
    setState({ source: value, draft: value });
  }

  useEffect(() => {
    const timer = setTimeout(() => {
      if (draft !== value) {
        onChange(draft);
      }
    }, 300);
    return () => clearTimeout(timer);
  }, [draft, onChange, value]);

  return (
    <Input
      className="w-80"
      placeholder="Search by number, title or author"
      value={draft}
      onChange={(event) => setState({ source: value, draft: event.target.value })}
      aria-label="Search PRs/MRs"
    />
  );
}
