import { PageHeader } from "@/components/common/PageHeader";

/**
 * RegisterPage is a placeholder registered by the hosted route table.
 * Task 23 fills in the registration form; this task only wires the route.
 */
export function RegisterPage() {
  return (
    <div className="mx-auto max-w-md p-10">
      <PageHeader title="Create an account" description="Register a new Converge account." />
    </div>
  );
}
