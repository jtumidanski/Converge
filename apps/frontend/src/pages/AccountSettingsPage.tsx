import { useMemo } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { PageHeader } from "@/components/common/PageHeader";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useChangePassword, useCurrentUser, useDeleteAccount } from "@/lib/hooks/api/useAuth";
import {
  changePasswordSchema,
  deleteAccountSchema,
  type ChangePasswordFormData,
  type DeleteAccountFormData,
} from "@/lib/schemas/auth";
import { applyServerError } from "@/lib/api/formErrors";

/**
 * AccountSettingsPage hosts two independent react-hook-form instances: change
 * password and delete account. They are kept separate rather than merged
 * into one form because they submit to different endpoints, validate against
 * different schemas, and fail independently.
 */
export function AccountSettingsPage() {
  const { data: user } = useCurrentUser();

  return (
    <div className="mx-auto max-w-2xl p-10">
      <PageHeader
        title="Account settings"
        description="Change your password or delete your account."
      />
      <div className="mt-6 flex flex-col gap-8">
        <AccountDetails username={user?.username} createdAt={user?.createdAt} />
        <ChangePasswordForm />
        <DeleteAccountForm username={user?.username ?? ""} />
      </div>
    </div>
  );
}

function AccountDetails({
  username,
  createdAt,
}: {
  username: string | undefined;
  createdAt: string | undefined;
}) {
  return (
    <div className="flex flex-col gap-1 rounded-md border border-border p-4">
      <span className="text-sm text-muted-foreground">Username</span>
      <span className="font-medium text-foreground">{username ?? "—"}</span>
      <span className="mt-2 text-sm text-muted-foreground">Member since</span>
      <span className="font-medium text-foreground">
        {createdAt ? new Date(createdAt).toLocaleDateString() : "—"}
      </span>
    </div>
  );
}

function ChangePasswordForm() {
  const changePassword = useChangePassword();
  const {
    register,
    handleSubmit,
    reset,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<ChangePasswordFormData>({
    resolver: zodResolver(changePasswordSchema),
    defaultValues: { currentPassword: "", newPassword: "", confirmPassword: "" },
  });

  async function onSubmit(values: ChangePasswordFormData) {
    try {
      await changePassword.mutateAsync({
        currentPassword: values.currentPassword,
        newPassword: values.newPassword,
      });
      reset();
      toast.success("Password changed.");
    } catch (error: unknown) {
      // INVALID_CREDENTIALS here means the current password was wrong, not
      // that the session is gone (see client.ts's onUnauthorized doc
      // comment) -- it lands on the field, and the user stays signed in.
      applyServerError(error, setError, {
        INVALID_CREDENTIALS: "currentPassword",
        WEAK_PASSWORD: "newPassword",
      });
    }
  }

  return (
    <form
      className="flex flex-col gap-4 rounded-md border border-border p-4"
      onSubmit={handleSubmit(onSubmit)}
      noValidate
    >
      <h2 className="text-sm font-semibold text-foreground">Change password</h2>
      <div className="flex flex-col gap-1.5">
        <label htmlFor="current-password" className="text-sm font-medium text-foreground">
          Current password
        </label>
        <Input
          id="current-password"
          type="password"
          autoComplete="current-password"
          aria-invalid={!!errors.currentPassword}
          {...register("currentPassword")}
        />
        {errors.currentPassword ? (
          <p className="text-sm text-destructive">{errors.currentPassword.message}</p>
        ) : null}
      </div>
      <div className="flex flex-col gap-1.5">
        <label htmlFor="new-password" className="text-sm font-medium text-foreground">
          New password
        </label>
        <Input
          id="new-password"
          type="password"
          autoComplete="new-password"
          aria-invalid={!!errors.newPassword}
          {...register("newPassword")}
        />
        {errors.newPassword ? (
          <p className="text-sm text-destructive">{errors.newPassword.message}</p>
        ) : null}
      </div>
      <div className="flex flex-col gap-1.5">
        <label htmlFor="confirm-new-password" className="text-sm font-medium text-foreground">
          Confirm new password
        </label>
        <Input
          id="confirm-new-password"
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
      <Button type="submit" disabled={isSubmitting} className="self-start">
        {isSubmitting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
        Change password
      </Button>
    </form>
  );
}

function DeleteAccountForm({ username }: { username: string }) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const deleteAccount = useDeleteAccount();
  // The schema depends on the signed-in username, so it is rebuilt when that
  // changes rather than captured once (FR-8.6).
  const resolver = useMemo(() => zodResolver(deleteAccountSchema(username)), [username]);
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors, isSubmitting, isValid },
  } = useForm<DeleteAccountFormData>({
    resolver,
    mode: "onChange",
    defaultValues: { password: "", confirmUsername: "" },
  });

  async function onSubmit(values: DeleteAccountFormData) {
    try {
      await deleteAccount.mutateAsync({ password: values.password });
      // Deletion ends the session server-side; nothing cached for this
      // account (or any other) should survive in this tab, matching
      // useLogout's full clear rather than a targeted invalidation.
      queryClient.clear();
      void navigate("/login", { replace: true });
    } catch (error: unknown) {
      applyServerError(error, setError, { INVALID_CREDENTIALS: "password" });
    }
  }

  return (
    <form
      className="flex flex-col gap-4 rounded-md border border-destructive p-4"
      onSubmit={handleSubmit(onSubmit)}
      noValidate
    >
      <div className="flex flex-col gap-1">
        <h2 className="text-sm font-semibold text-destructive">Delete account</h2>
        <p className="text-sm text-muted-foreground">
          This permanently removes your account, its provider configurations, its review sessions
          and workspaces, and its repository mirrors. This cannot be undone: there is no password
          reset and no administrator who can restore it.
        </p>
      </div>
      <div className="flex flex-col gap-1.5">
        <label htmlFor="delete-password" className="text-sm font-medium text-foreground">
          Password
        </label>
        <Input
          id="delete-password"
          type="password"
          autoComplete="current-password"
          aria-invalid={!!errors.password}
          {...register("password")}
        />
        {errors.password ? (
          <p className="text-sm text-destructive">{errors.password.message}</p>
        ) : null}
      </div>
      <div className="flex flex-col gap-1.5">
        <label htmlFor="confirm-username" className="text-sm font-medium text-foreground">
          Type {username} to confirm
        </label>
        <Input
          id="confirm-username"
          autoComplete="off"
          aria-invalid={!!errors.confirmUsername}
          {...register("confirmUsername")}
        />
        {errors.confirmUsername ? (
          <p className="text-sm text-destructive">{errors.confirmUsername.message}</p>
        ) : null}
      </div>
      {errors.root ? <p className="text-sm text-destructive">{errors.root.message}</p> : null}
      <Button
        type="submit"
        variant="destructive"
        disabled={isSubmitting || !isValid}
        className="self-start"
      >
        {isSubmitting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
        Delete account
      </Button>
    </form>
  );
}
