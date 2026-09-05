import { z } from "zod";

/** Mirrors the backend repository rule (FR-3.7). */
export const repositorySchema = z.object({
  repository: z
    .string()
    .min(1, "Enter a repository as owner/name")
    .regex(/^[A-Za-z0-9_.-]+(\/[A-Za-z0-9_.-]+)+$/, "Enter a repository as owner/name")
    .refine((value) => !value.includes(".."), "Path segments cannot contain ..")
    .refine((value) => !/^[-./]/.test(value), "A repository cannot start with -, . or /"),
});

export type RepositoryFormData = z.infer<typeof repositorySchema>;
