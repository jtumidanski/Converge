import { z } from "zod";

/** Mirrors the backend username rule (FR-2.1). */
const username = z
  .string()
  .regex(
    /^[a-zA-Z0-9][a-zA-Z0-9._-]{2,31}$/,
    "3 to 32 characters, starting with a letter or digit; letters, digits, dots, underscores and hyphens.",
  );

/**
 * Mirrors the backend password rule (FR-2.2): length is the only requirement.
 * There are deliberately no composition rules here, because there are none on
 * the server — adding one would reject a password the server accepts.
 */
const password = z.string().min(8, "At least 8 characters.").max(1024, "At most 1024 characters.");

export const loginSchema = z.object({ username, password });
export type LoginFormData = z.infer<typeof loginSchema>;

export const registerSchema = loginSchema
  .extend({ confirmPassword: z.string() })
  .refine((v) => v.password === v.confirmPassword, {
    message: "The passwords do not match.",
    path: ["confirmPassword"],
  });
export type RegisterFormData = z.infer<typeof registerSchema>;

export const changePasswordSchema = z
  .object({
    currentPassword: z.string().min(1, "Enter your current password."),
    newPassword: password,
    confirmPassword: z.string(),
  })
  .refine((v) => v.newPassword === v.confirmPassword, {
    message: "The passwords do not match.",
    path: ["confirmPassword"],
  });
export type ChangePasswordFormData = z.infer<typeof changePasswordSchema>;

/**
 * deleteAccountSchema is a factory because the confirmation compares against
 * the signed-in username (FR-8.6): deletion is irreversible, so it takes a
 * typed confirmation rather than a checkbox.
 */
export function deleteAccountSchema(expectedUsername: string) {
  return z.object({
    password: z.string().min(1, "Enter your password."),
    confirmUsername: z.literal(expectedUsername, `Type ${expectedUsername} to confirm.`),
  });
}
export type DeleteAccountFormData = { password: string; confirmUsername: string };
