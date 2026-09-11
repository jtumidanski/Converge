import { apiDeleteWithBody, apiGet, apiPost, apiPostNoContent } from "@/lib/api/client";
import { unwrapOne, type Document, type Resource } from "@/types/api/jsonapi";
import type { AuthMode, CurrentUser } from "@/types/models/auth";

type ModeResource = Resource<"modes", AuthMode>;
type UserResource = Resource<
  "users",
  Omit<CurrentUser, "id" | "providerCount"> & { providerCount?: number }
>;

function toUser(r: UserResource): CurrentUser {
  return { id: r.id, ...r.attributes, providerCount: r.attributes.providerCount ?? 0 };
}

export const authService = {
  async mode(): Promise<AuthMode> {
    const doc = await apiGet<Document<ModeResource>>("/api/auth/mode");
    return unwrapOne(doc).attributes;
  },
  async me(): Promise<CurrentUser> {
    const doc = await apiGet<Document<UserResource>>("/api/auth/me");
    return toUser(unwrapOne(doc));
  },
  async login(username: string, password: string): Promise<CurrentUser> {
    const doc = await apiPost<Document<UserResource>>("/api/auth/login", {
      data: { type: "credentials", attributes: { username, password } },
    });
    return toUser(unwrapOne(doc));
  },
  async register(username: string, password: string): Promise<CurrentUser> {
    const doc = await apiPost<Document<UserResource>>("/api/auth/register", {
      data: { type: "credentials", attributes: { username, password } },
    });
    return toUser(unwrapOne(doc));
  },
  async logout(): Promise<void> {
    await apiPostNoContent("/api/auth/logout");
  },
  async changePassword(currentPassword: string, newPassword: string): Promise<void> {
    await apiPostNoContent("/api/auth/password", {
      data: { type: "passwords", attributes: { currentPassword, newPassword } },
    });
  },
  async deleteAccount(password: string): Promise<void> {
    await apiDeleteWithBody("/api/auth/me", {
      data: { type: "accountDeletions", attributes: { password } },
    });
  },
};
