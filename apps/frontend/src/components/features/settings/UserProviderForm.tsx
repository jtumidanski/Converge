import type { ReactNode } from "react";
import { useForm, useWatch } from "react-hook-form";
import type { UseFormRegisterReturn } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { applyServerError } from "@/lib/api/formErrors";
import { useCreateUserProvider, useUpdateUserProvider } from "@/lib/hooks/api/useUserProviders";
import {
  userProviderCreateSchema,
  userProviderEditSchema,
  type UserProviderCreateFormData,
  type UserProviderEditFormData,
} from "@/lib/schemas/userProvider";
import type { UserProviderPatch } from "@/services/api/userProviders";
import type { ProviderKind, UserProvider } from "@/types/models/auth";

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
 *
 * Everything the two instances render in common lives in UserProviderFormBody
 * below: the variants differ only in their hook, their submit handler, the
 * slug slot, and two strings.
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

interface UserProviderFormBodyProps {
  onSubmit: (event: React.FormEvent<HTMLFormElement>) => void;
  onCancel: () => void;
  isSubmitting: boolean;
  submitLabel: string;
  /** The slug row: an editable Input on create, immutable text on edit. */
  slugRow: ReactNode;
  displayNameField: UseFormRegisterReturn;
  baseUrlField: UseFormRegisterReturn;
  tokenField: UseFormRegisterReturn;
  kind: ProviderKind;
  onKindChange: (kind: ProviderKind) => void;
  tokenPlaceholder: string;
  /**
   * Messages rather than FieldError objects: the two variants' error maps have
   * different field unions, and only the message is ever rendered.
   */
  displayNameError?: string | undefined;
  baseUrlError?: string | undefined;
  tokenError?: string | undefined;
  rootError?: string | undefined;
}

/**
 * UserProviderFormBody is the shared body of the create and edit forms: the
 * display name, kind, base URL and token rows, the form-level error, and the
 * buttons. It is presentational — it holds no form state of its own, and
 * receives each field already registered by its caller's useForm instance.
 */
function UserProviderFormBody({
  onSubmit,
  onCancel,
  isSubmitting,
  submitLabel,
  slugRow,
  displayNameField,
  baseUrlField,
  tokenField,
  kind,
  onKindChange,
  tokenPlaceholder,
  displayNameError,
  baseUrlError,
  tokenError,
  rootError,
}: UserProviderFormBodyProps) {
  return (
    <form
      className="flex flex-col gap-4 rounded-md border border-border p-4"
      onSubmit={onSubmit}
      noValidate
    >
      {slugRow}
      <div className="flex flex-col gap-1.5">
        <label htmlFor="provider-display-name" className="text-sm font-medium text-foreground">
          Display name
        </label>
        <Input id="provider-display-name" aria-invalid={!!displayNameError} {...displayNameField} />
        {displayNameError ? <p className="text-sm text-destructive">{displayNameError}</p> : null}
      </div>
      <div className="flex flex-col gap-1.5">
        <label htmlFor="provider-kind" className="text-sm font-medium text-foreground">
          Kind
        </label>
        {/*
          The shipped Radix Select, matching ProviderPicker: it brings the
          focus ring, the popup animation, and dark-mode theming that a native
          <select> popup does not get. It is not an <input>, so the value is
          driven by the caller's form state rather than by register().
        */}
        <Select value={kind} onValueChange={(value) => onKindChange(value as ProviderKind)}>
          <SelectTrigger id="provider-kind" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="github">GitHub</SelectItem>
            <SelectItem value="gitlab">GitLab</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="flex flex-col gap-1.5">
        <label htmlFor="provider-base-url" className="text-sm font-medium text-foreground">
          Base URL
        </label>
        <Input
          id="provider-base-url"
          placeholder="https://gitlab.example.com"
          aria-invalid={!!baseUrlError}
          {...baseUrlField}
        />
        {baseUrlError ? <p className="text-sm text-destructive">{baseUrlError}</p> : null}
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
          placeholder={tokenPlaceholder}
          aria-invalid={!!tokenError}
          {...tokenField}
        />
        {tokenError ? <p className="text-sm text-destructive">{tokenError}</p> : null}
      </div>
      {rootError ? <p className="text-sm text-destructive">{rootError}</p> : null}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={isSubmitting}>
          {isSubmitting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          {submitLabel}
        </Button>
      </div>
    </form>
  );
}

function CreateUserProviderForm({ onSubmitted, onCancel }: FormShellProps) {
  const create = useCreateUserProvider();
  const {
    register,
    handleSubmit,
    setError,
    setValue,
    control,
    formState: { errors, isSubmitting },
  } = useForm<UserProviderCreateFormData>({
    resolver: zodResolver(userProviderCreateSchema),
    defaultValues: { slug: "", displayName: "", kind: "github", baseUrl: "", token: "" },
  });
  // useWatch rather than watch(): the subscription form keeps the React
  // Compiler able to memoize this component, which watch() defeats.
  const kind = useWatch({ control, name: "kind" });

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
    <UserProviderFormBody
      onSubmit={handleSubmit(onSubmit)}
      onCancel={onCancel}
      isSubmitting={isSubmitting}
      submitLabel="Add provider"
      slugRow={
        <div className="flex flex-col gap-1.5">
          <label htmlFor="provider-slug" className="text-sm font-medium text-foreground">
            Slug
          </label>
          <Input id="provider-slug" aria-invalid={!!errors.slug} {...register("slug")} />
          {errors.slug ? <p className="text-sm text-destructive">{errors.slug.message}</p> : null}
        </div>
      }
      displayNameField={register("displayName")}
      baseUrlField={register("baseUrl")}
      tokenField={register("token")}
      kind={kind}
      onKindChange={(kind) => setValue("kind", kind, { shouldValidate: true })}
      tokenPlaceholder="Access token"
      displayNameError={errors.displayName?.message}
      baseUrlError={errors.baseUrl?.message}
      tokenError={errors.token?.message}
      rootError={errors.root?.message}
    />
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
    setValue,
    control,
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
  const kind = useWatch({ control, name: "kind" });

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
    <UserProviderFormBody
      onSubmit={handleSubmit(onSubmit)}
      onCancel={onCancel}
      isSubmitting={isSubmitting}
      submitLabel="Save changes"
      slugRow={
        <div className="flex flex-col gap-1.5">
          <span className="text-sm font-medium text-foreground">Slug</span>
          {/* The slug is immutable once a provider exists, so edit shows it as
              plain text rather than an editable input. */}
          <p className="text-sm text-muted-foreground">{provider.slug}</p>
        </div>
      }
      displayNameField={register("displayName")}
      baseUrlField={register("baseUrl")}
      tokenField={register("token")}
      kind={kind}
      onKindChange={(kind) => setValue("kind", kind, { shouldValidate: true })}
      tokenPlaceholder="Leave blank to keep the current token"
      displayNameError={errors.displayName?.message}
      baseUrlError={errors.baseUrl?.message}
      tokenError={errors.token?.message}
      rootError={errors.root?.message}
    />
  );
}
