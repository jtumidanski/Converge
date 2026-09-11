import { PageHeader } from "@/components/common/PageHeader";

/**
 * ProviderSettingsPage is a placeholder registered by the hosted route
 * table, guarded by RequireAuth. Task 24 fills in the provider list and
 * token forms; this task only wires the route.
 */
export function ProviderSettingsPage() {
  return (
    <div className="mx-auto max-w-3xl p-10">
      <PageHeader
        title="Provider settings"
        description="Manage the GitHub and GitLab tokens Converge uses on your behalf."
      />
    </div>
  );
}
