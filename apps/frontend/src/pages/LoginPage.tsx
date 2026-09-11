import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Link, useLocation, useNavigate } from "react-router";
import { Loader2 } from "lucide-react";
import { PageHeader } from "@/components/common/PageHeader";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useLogin } from "@/lib/hooks/api/useAuth";
import { loginSchema, type LoginFormData } from "@/lib/schemas/auth";
import { applyServerError, safeNext } from "@/lib/api/formErrors";

/**
 * LoginPage signs a visitor in and returns them to the path they were
 * attempting before RequireAuth redirected them (FR-8.2, FR-8.3).
 *
 * A failed login (INVALID_CREDENTIALS) never trips the global 401 handler —
 * see the doc comment on setUnauthorizedHandler in lib/api/client.ts — so it
 * must be surfaced here as a form-level error instead.
 */
export function LoginPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const login = useLogin();
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<LoginFormData>({
    resolver: zodResolver(loginSchema),
    defaultValues: { username: "", password: "" },
  });

  async function onSubmit(values: LoginFormData) {
    try {
      await login.mutateAsync(values);
      void navigate(safeNext(location.search), { replace: true });
    } catch (error: unknown) {
      applyServerError(error, setError, {});
    }
  }

  return (
    <div className="mx-auto max-w-md p-10">
      <PageHeader title="Log in" description="Sign in to your Converge account." />
      <form className="mt-6 flex flex-col gap-4" onSubmit={handleSubmit(onSubmit)} noValidate>
        <div className="flex flex-col gap-1.5">
          <label htmlFor="login-username" className="text-sm font-medium text-foreground">
            Username
          </label>
          <Input
            id="login-username"
            autoComplete="username"
            aria-invalid={!!errors.username}
            {...register("username")}
          />
          {errors.username ? (
            <p className="text-sm text-destructive">{errors.username.message}</p>
          ) : null}
        </div>
        <div className="flex flex-col gap-1.5">
          <label htmlFor="login-password" className="text-sm font-medium text-foreground">
            Password
          </label>
          <Input
            id="login-password"
            type="password"
            autoComplete="current-password"
            aria-invalid={!!errors.password}
            {...register("password")}
          />
          {errors.password ? (
            <p className="text-sm text-destructive">{errors.password.message}</p>
          ) : null}
        </div>
        {errors.root ? <p className="text-sm text-destructive">{errors.root.message}</p> : null}
        <Button type="submit" disabled={isSubmitting}>
          {isSubmitting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          Log in
        </Button>
      </form>
      <p className="mt-4 text-sm text-muted-foreground">
        Don&apos;t have an account?{" "}
        <Link to="/register" className="font-medium text-foreground underline">
          Register
        </Link>
      </p>
    </div>
  );
}
