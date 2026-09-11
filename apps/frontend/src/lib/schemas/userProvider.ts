import { z } from "zod";

/** Mirrors the backend slug rule (FR-5.1). */
const slug = z
  .string()
  .regex(
    /^[a-z0-9][a-z0-9-]{0,31}$/,
    "1 to 32 characters of lowercase letters, digits and hyphens, starting with a letter or digit.",
  );

const kind = z.enum(["github", "gitlab"]);

/** Mirrors the backend base-URL rule (FR-5.2). */
const baseUrl = z
  .string()
  .refine((v) => v === "" || /^https?:\/\/[^\s/]+/.test(v), "Enter an absolute http or https URL.");

const providerFields = {
  slug,
  displayName: z.string().max(128, "At most 128 characters."),
  kind,
  baseUrl,
};

export const userProviderCreateSchema = z
  .object({ ...providerFields, token: z.string().min(1, "A token is required.") })
  .refine((v) => v.kind === "github" || v.baseUrl !== "", {
    message: "A GitLab provider needs a base URL.",
    path: ["baseUrl"],
  });
export type UserProviderCreateFormData = z.infer<typeof userProviderCreateSchema>;

/**
 * The edit form allows an empty token, which means "keep the current one"
 * (FR-5.5). The form never receives a token to render, so there is no
 * possibility of leaking one — FR-5.4 and FR-8.5 are the same mechanism seen
 * from two ends. slug is absent because it is immutable.
 */
export const userProviderEditSchema = z
  .object({
    displayName: providerFields.displayName,
    kind,
    baseUrl,
    token: z.string(),
  })
  .refine((v) => v.kind === "github" || v.baseUrl !== "", {
    message: "A GitLab provider needs a base URL.",
    path: ["baseUrl"],
  });
export type UserProviderEditFormData = z.infer<typeof userProviderEditSchema>;
