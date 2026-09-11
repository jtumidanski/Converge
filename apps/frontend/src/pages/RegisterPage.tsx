import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Link, useNavigate } from "react-router";
import { Loader2 } from "lucide-react";
import { PageHeader } from "@/components/common/PageHeader";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useRegister } from "@/lib/hooks/api/useAuth";
import { registerSchema, type RegisterFormData } from "@/lib/schemas/auth";
import { applyServerError } from "@/lib/api/formErrors";

/**
 * RegisterPage creates a new Converge account. A fresh account always has
 * providerCount: 0, so a successful registration goes straight to provider
 * settings rather than to an empty repository picker (FR-5.9).
 */
export function RegisterPage() {
  const navigate = useNavigate();
  const register_ = useRegister();
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<RegisterFormData>({
    resolver: zodResolver(registerSchema),
    defaultValues: { username: "", password: "", confirmPassword: "" },
  });

  async function onSubmit(values: RegisterFormData) {
    try {
      await register_.mutateAsync(values);
      void navigate("/settings/providers", { replace: true });
    } catch (error: unknown) {
      applyServerError(error, setError, {
        USERNAME_TAKEN: "username",
        INVALID_USERNAME: "username",
        WEAK_PASSWORD: "password",
      });
    }
  }

  return (
    <div className="mx-auto max-w-md p-10">
      <PageHeader title="Create an account" description="Register a new Converge account." />
      <form className="mt-6 flex flex-col gap-4" onSubmit={handleSubmit(onSubmit)} noValidate>
        <div className="flex flex-col gap-1.5">
          <label htmlFor="register-username" className="text-sm font-medium text-foreground">
            Username
          </label>
          <Input
            id="register-username"
            autoComplete="username"
            aria-invalid={!!errors.username}
            {...register("username")}
          />
          {errors.username ? (
            <p className="text-sm text-destructive">{errors.username.message}</p>
          ) : null}
        </div>
        <div className="flex flex-col gap-1.5">
          <label htmlFor="register-password" className="text-sm font-medium text-foreground">
            Password
          </label>
          <Input
            id="register-password"
            type="password"
            autoComplete="new-password"
            aria-invalid={!!errors.password}
            {...register("password")}
          />
          {errors.password ? (
            <p className="text-sm text-destructive">{errors.password.message}</p>
          ) : null}
        </div>
        <div className="flex flex-col gap-1.5">
          <label
            htmlFor="register-confirm-password"
            className="text-sm font-medium text-foreground"
          >
            Confirm password
          </label>
          <Input
            id="register-confirm-password"
            type="password"
            autoComplete="new-password"
            aria-invalid={!!errors.confirmPassword}
            {...register("confirmPassword")}
          />
          {errors.confirmPassword ? (
            <p className="text-sm text-destructive">{errors.confirmPassword.message}</p>
          ) : null}
        </div>
        {errors.root ? <p className="text-sm text-destructive">{errors.root.message}</p> : null}
        <Button type="submit" disabled={isSubmitting}>
          {isSubmitting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          Create account
        </Button>
      </form>
      <p className="mt-4 text-sm text-muted-foreground">
        Already have an account?{" "}
        <Link to="/login" className="font-medium text-foreground underline">
          Log in
        </Link>
      </p>
    </div>
  );
}
