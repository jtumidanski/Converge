import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { applyServerError } from "@/lib/api/formErrors";
import { useCreateUserProvider, useUpdateUserProvider } from "@/lib/hooks/api/useUserProviders";
import {
  userProviderCreateSchema,
  userProviderEditSchema,
  type UserProviderCreateFormData,
  type UserProviderEditFormData,
} from "@/lib/schemas/userProvider";
import type { UserProviderPatch } from "@/services/api/userProviders";
import type { UserProvider } from "@/types/models/auth";

interface UserProviderFormProps {
  mode: "create" | "edit";
  /** provider is required for mode "edit" and ignored for mode "create". */
  provider?: UserProvider;
  onSubmitted: () => void;
  onCancel: () => void;
}

/**
 * UserProviderForm dispatches to a create or edit variant. The two forms have
 * different fields (slug is immutable, so edit omits it entirely) and
 * different Zod schemas, so they get separate react-hook-form instances
 * rather than one form juggling two shapes.
 */
export function UserProviderForm({ mode, provider, onSubmitted, onCancel }: UserProviderFormProps) {
  if (mode === "edit" && provider) {
    return (
      <EditUserProviderForm provider={provider} onSubmitted={onSubmitted} onCancel={onCancel} />
    );
  }
  return <CreateUserProviderForm onSubmitted={onSubmitted} onCancel={onCancel} />;
}

interface FormShellProps {
  onSubmitted: () => void;
  onCancel: () => void;
}

function CreateUserProviderForm({ onSubmitted, onCancel }: FormShellProps) {
  const create = useCreateUserProvider();
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<UserProviderCreateFormData>({
    resolver: zodResolver(userProviderCreateSchema),
    defaultValues: { slug: "", displayName: "", kind: "github", baseUrl: "", token: "" },
  });

  async function onSubmit(values: UserProviderCreateFormData) {
    try {
      // validate: true asks the server to confirm the token works before the
      // provider is stored, rather than discovering an unauthorized token on
      // the first review attempt.
      await create.mutateAsync({ ...values, validate: true });
      onSubmitted();
    } catch (error: unknown) {
      applyServerError(error, setError, {
        PROVIDER_SLUG_TAKEN: "slug",
        PROVIDER_UNAUTHORIZED: "token",
        VALIDATION_ERROR: undefined,
      });
    }
  }

  return (
    <form
      className="flex flex-col gap-4 rounded-md border border-border p-4"
      onSubmit={handleSubmit(onSubmit)}
      noValidate
    >
      <div className="flex flex-col gap-1.5">
        <label htmlFor="provider-slug" className="text-sm font-medium text-foreground">
          Slug
        </label>
        <Input id="provider-slug" aria-invalid={!!errors.slug} {...register("slug")} />
        {errors.slug ? <p className="text-sm text-destructive">{errors.slug.message}</p> : null}
      </div>
      <div className="flex flex-col gap-1.5">
        <label htmlFor="provider-display-name" className="text-sm font-medium text-foreground">
          Display name
        </label>
        <Input
          id="provider-display-name"
          aria-invalid={!!errors.displayName}
          {...register("displayName")}
        />
        {errors.displayName ? (
          <p className="text-sm text-destructive">{errors.displayName.message}</p>
        ) : null}
      </div>
      <div className="flex flex-col gap-1.5">
        <label htmlFor="provider-kind" className="text-sm font-medium text-foreground">
          Kind
        </label>
        <select
          id="provider-kind"
          className="h-9 rounded-md border border-input bg-transparent px-3 text-sm shadow-xs"
          {...register("kind")}
        >
          <option value="github">GitHub</option>
          <option value="gitlab">GitLab</option>
        </select>
      </div>
      <div className="flex flex-col gap-1.5">
        <label htmlFor="provider-base-url" className="text-sm font-medium text-foreground">
          Base URL
        </label>
        <Input
          id="provider-base-url"
          placeholder="https://gitlab.example.com"
          aria-invalid={!!errors.baseUrl}
          {...register("baseUrl")}
        />
        {errors.baseUrl ? (
          <p className="text-sm text-destructive">{errors.baseUrl.message}</p>
        ) : null}
      </div>
      <div className="flex flex-col gap-1.5">
        <label htmlFor="provider-token" className="text-sm font-medium text-foreground">
          Access token
        </label>
        {/*
          The token input is always empty on load, type="password", and says
          so. The form never receives a token to render, because no API
          response carries one -- FR-5.4 and FR-8.5 are the same mechanism
          seen from two ends.
        */}
        <Input
          id="provider-token"
          type="password"
          autoComplete="new-password"
          placeholder="Access token"
          aria-invalid={!!errors.token}
          {...register("token")}
        />
        {errors.token ? <p className="text-sm text-destructive">{errors.token.message}</p> : null}
      </div>
      {errors.root ? <p className="text-sm text-destructive">{errors.root.message}</p> : null}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={isSubmitting}>
          {isSubmitting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          Add provider
        </Button>
      </div>
    </form>
  );
}

function EditUserProviderForm({
  provider,
  onSubmitted,
  onCancel,
}: FormShellProps & { provider: UserProvider }) {
  const update = useUpdateUserProvider();
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<UserProviderEditFormData>({
    resolver: zodResolver(userProviderEditSchema),
    defaultValues: {
      displayName: provider.displayName,
      kind: provider.kind,
      baseUrl: provider.baseUrl,
      token: "",
    },
  });

  async function onSubmit(values: UserProviderEditFormData) {
    const patch: UserProviderPatch = {
      displayName: values.displayName,
      kind: values.kind,
      baseUrl: values.baseUrl,
      // Omitted, not sent as "", so the request body genuinely has no token
      // key. The server treats both the same (FR-5.5), but omitting keeps
      // the wire honest about intent and keeps the empty string out of any
      // proxy log.
      ...(values.token !== "" ? { token: values.token } : {}),
    };
    try {
      await update.mutateAsync({ id: provider.id, patch });
      onSubmitted();
    } catch (error: unknown) {
      applyServerError(error, setError, {
        PROVIDER_UNAUTHORIZED: "token",
        VALIDATION_ERROR: undefined,
      });
    }
  }

  return (
    <form
      className="flex flex-col gap-4 rounded-md border border-border p-4"
      onSubmit={handleSubmit(onSubmit)}
      noValidate
    >
      <div className="flex flex-col gap-1.5">
        <span className="text-sm font-medium text-foreground">Slug</span>
        {/* The slug is immutable once a provider exists, so edit shows it as
            plain text rather than an editable input. */}
        <p className="text-sm text-muted-foreground">{provider.slug}</p>
      </div>
      <div className="flex flex-col gap-1.5">
        <label htmlFor="provider-display-name" className="text-sm font-medium text-foreground">
          Display name
        </label>
        <Input
          id="provider-display-name"
          aria-invalid={!!errors.displayName}
          {...register("displayName")}
        />
        {errors.displayName ? (
          <p className="text-sm text-destructive">{errors.displayName.message}</p>
        ) : null}
      </div>
      <div className="flex flex-col gap-1.5">
        <label htmlFor="provider-kind" className="text-sm font-medium text-foreground">
          Kind
        </label>
        <select
          id="provider-kind"
          className="h-9 rounded-md border border-input bg-transparent px-3 text-sm shadow-xs"
          {...register("kind")}
        >
          <option value="github">GitHub</option>
          <option value="gitlab">GitLab</option>
        </select>
      </div>
      <div className="flex flex-col gap-1.5">
        <label htmlFor="provider-base-url" className="text-sm font-medium text-foreground">
          Base URL
        </label>
        <Input
          id="provider-base-url"
          placeholder="https://gitlab.example.com"
          aria-invalid={!!errors.baseUrl}
          {...register("baseUrl")}
        />
        {errors.baseUrl ? (
          <p className="text-sm text-destructive">{errors.baseUrl.message}</p>
        ) : null}
      </div>
      <div className="flex flex-col gap-1.5">
        <label htmlFor="provider-token" className="text-sm font-medium text-foreground">
          Access token
        </label>
        <Input
          id="provider-token"
          type="password"
          autoComplete="new-password"
          placeholder="Leave blank to keep the current token"
          aria-invalid={!!errors.token}
          {...register("token")}
        />
        {errors.token ? <p className="text-sm text-destructive">{errors.token.message}</p> : null}
      </div>
      {errors.root ? <p className="text-sm text-destructive">{errors.root.message}</p> : null}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={isSubmitting}>
          {isSubmitting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          Save changes
        </Button>
      </div>
    </form>
  );
}
