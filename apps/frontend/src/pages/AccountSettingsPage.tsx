import { PageHeader } from "@/components/common/PageHeader";

/**
 * AccountSettingsPage is a placeholder registered by the hosted route
 * table, guarded by RequireAuth. Task 25 fills in the password-change and
 * delete-account forms; this task only wires the route.
 */
export function AccountSettingsPage() {
  return (
    <div className="mx-auto max-w-2xl p-10">
      <PageHeader
        title="Account settings"
        description="Change your password or delete your account."
      />
    </div>
  );
}
