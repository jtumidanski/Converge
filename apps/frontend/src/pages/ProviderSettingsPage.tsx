import { useState } from "react";
import { Plus } from "lucide-react";
import { PageHeader } from "@/components/common/PageHeader";
import { Button } from "@/components/ui/button";
import { UserProviderForm } from "@/components/features/settings/UserProviderForm";
import { UserProviderList } from "@/components/features/settings/UserProviderList";
import { useUserProviders } from "@/lib/hooks/api/useUserProviders";
import type { UserProvider } from "@/types/models/auth";

type Panel = { mode: "closed" } | { mode: "create" } | { mode: "edit"; provider: UserProvider };

/**
 * ProviderSettingsPage manages the GitHub/GitLab tokens a hosted user attaches
 * to their account. The create and edit forms share the token-never-round-
 * trips contract described on UserProviderForm.
 */
export function ProviderSettingsPage() {
  const { data: providers, isLoading, error, refetch } = useUserProviders();
  const [panel, setPanel] = useState<Panel>({ mode: "closed" });

  function close() {
    setPanel({ mode: "closed" });
  }

  return (
    <div className="mx-auto max-w-3xl p-10">
      <PageHeader
        title="Provider settings"
        description="Manage the GitHub and GitLab tokens Converge uses on your behalf."
        actions={
          panel.mode === "closed" ? (
            <Button size="sm" onClick={() => setPanel({ mode: "create" })}>
              <Plus className="mr-2 h-4 w-4" />
              Add provider
            </Button>
          ) : undefined
        }
      />
      <div className="mt-6 flex flex-col gap-6">
        {panel.mode === "create" ? (
          <UserProviderForm mode="create" onSubmitted={close} onCancel={close} />
        ) : null}
        {panel.mode === "edit" ? (
          <UserProviderForm
            mode="edit"
            provider={panel.provider}
            onSubmitted={close}
            onCancel={close}
          />
        ) : null}
        <UserProviderList
          providers={providers ?? []}
          loading={isLoading}
          error={error ?? undefined}
          onRetry={() => void refetch()}
          onEdit={(provider) => setPanel({ mode: "edit", provider })}
        />
      </div>
    </div>
  );
}
