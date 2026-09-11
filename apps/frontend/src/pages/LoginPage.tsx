import { PageHeader } from "@/components/common/PageHeader";

/**
 * LoginPage is a placeholder registered by the hosted route table so
 * RequireAuth has somewhere to send an unauthenticated visitor. Task 23
 * fills in the credentials form; this task only wires the route.
 */
export function LoginPage() {
  return (
    <div className="mx-auto max-w-md p-10">
      <PageHeader title="Log in" description="Sign in to your Converge account." />
    </div>
  );
}
