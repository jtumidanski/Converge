import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { repositoriesService } from "@/services/api";
import { messageFor } from "@/lib/api/errors";
import { repositorySchema, type RepositoryFormData } from "@/lib/schemas/repository";
import type { Repository } from "@/types/models/repository";

interface ManualRepositoryFormProps {
  providerId: string | undefined;
  onResolved: (repository: Repository) => void;
}

export function ManualRepositoryForm({ providerId, onResolved }: ManualRepositoryFormProps) {
  const [serverError, setServerError] = useState<string | null>(null);
  const [checking, setChecking] = useState(false);
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<RepositoryFormData>({
    resolver: zodResolver(repositorySchema),
    defaultValues: { repository: "" },
  });

  async function onSubmit(values: RepositoryFormData) {
    if (!providerId) {
      setServerError("Select a provider first.");
      return;
    }
    setServerError(null);
    setChecking(true);
    try {
      onResolved(await repositoriesService.get(providerId, values.repository));
    } catch (error: unknown) {
      setServerError(messageFor(error, "That repository could not be read."));
    } finally {
      setChecking(false);
    }
  }

  return (
    <form className="flex flex-col gap-2" onSubmit={handleSubmit(onSubmit)} noValidate>
      <label htmlFor="manual-repository" className="text-sm font-medium text-foreground">
        Repository
      </label>
      <div className="flex items-start gap-2">
        <Input
          id="manual-repository"
          placeholder="owner/name"
          className="w-72"
          {...register("repository")}
        />
        <Button type="submit" disabled={checking}>
          {checking ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          Use repository
        </Button>
      </div>
      {errors.repository ? (
        <p className="text-sm text-destructive">{errors.repository.message}</p>
      ) : null}
      {serverError ? <p className="text-sm text-destructive">{serverError}</p> : null}
    </form>
  );
}
