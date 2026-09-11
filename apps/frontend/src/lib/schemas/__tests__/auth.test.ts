import { describe, expect, it } from "vitest";
import {
  changePasswordSchema,
  deleteAccountSchema,
  loginSchema,
  registerSchema,
} from "@/lib/schemas/auth";
import { userProviderCreateSchema, userProviderEditSchema } from "@/lib/schemas/userProvider";

describe("loginSchema", () => {
  it("accepts a valid username and password", () => {
    expect(loginSchema.safeParse({ username: "alice", password: "12345678" }).success).toBe(true);
  });

  it("rejects a 2-character username", () => {
    expect(loginSchema.safeParse({ username: "al", password: "12345678" }).success).toBe(false);
  });

  it("rejects a 33-character username", () => {
    expect(loginSchema.safeParse({ username: "a".repeat(33), password: "12345678" }).success).toBe(
      false,
    );
  });

  it("rejects a username starting with an underscore", () => {
    expect(loginSchema.safeParse({ username: "_alice", password: "12345678" }).success).toBe(false);
  });

  it("rejects a 7-character password", () => {
    expect(loginSchema.safeParse({ username: "alice", password: "1234567" }).success).toBe(false);
  });

  it("accepts a password of eight lowercase letters (no composition rules)", () => {
    expect(loginSchema.safeParse({ username: "alice", password: "abcdefgh" }).success).toBe(true);
  });
});

describe("registerSchema", () => {
  it("requires confirmPassword to equal password, attached to confirmPassword", () => {
    const result = registerSchema.safeParse({
      username: "alice",
      password: "12345678",
      confirmPassword: "different",
    });
    expect(result.success).toBe(false);
    if (!result.success) {
      expect(result.error.issues[0]?.path).toEqual(["confirmPassword"]);
    }
  });

  it("accepts matching passwords", () => {
    const result = registerSchema.safeParse({
      username: "alice",
      password: "12345678",
      confirmPassword: "12345678",
    });
    expect(result.success).toBe(true);
  });
});

describe("changePasswordSchema", () => {
  it("requires currentPassword non-empty", () => {
    const result = changePasswordSchema.safeParse({
      currentPassword: "",
      newPassword: "12345678",
      confirmPassword: "12345678",
    });
    expect(result.success).toBe(false);
  });

  it("requires newPassword to be 8-1024 characters", () => {
    const result = changePasswordSchema.safeParse({
      currentPassword: "old",
      newPassword: "short",
      confirmPassword: "short",
    });
    expect(result.success).toBe(false);
  });

  it("requires confirmPassword to match newPassword", () => {
    const result = changePasswordSchema.safeParse({
      currentPassword: "old",
      newPassword: "12345678",
      confirmPassword: "different",
    });
    expect(result.success).toBe(false);
  });

  it("accepts a valid change", () => {
    const result = changePasswordSchema.safeParse({
      currentPassword: "old",
      newPassword: "12345678",
      confirmPassword: "12345678",
    });
    expect(result.success).toBe(true);
  });
});

describe("deleteAccountSchema", () => {
  it("requires password non-empty", () => {
    const schema = deleteAccountSchema("alice");
    expect(schema.safeParse({ password: "", confirmUsername: "alice" }).success).toBe(false);
  });

  it("requires confirmUsername to equal the expected username, erroring on confirmUsername", () => {
    const schema = deleteAccountSchema("alice");
    const result = schema.safeParse({ password: "secret", confirmUsername: "bob" });
    expect(result.success).toBe(false);
    if (!result.success) {
      expect(result.error.issues[0]?.path).toEqual(["confirmUsername"]);
    }
  });

  it("accepts a matching confirmation", () => {
    const schema = deleteAccountSchema("alice");
    expect(schema.safeParse({ password: "secret", confirmUsername: "alice" }).success).toBe(true);
  });
});

describe("userProviderCreateSchema", () => {
  const valid = {
    slug: "gitlab-internal",
    displayName: "Internal GitLab",
    kind: "gitlab" as const,
    baseUrl: "https://gitlab.example.com",
    token: "glpat-xxxx",
  };

  it("accepts a valid slug/kind/baseUrl/token", () => {
    expect(userProviderCreateSchema.safeParse(valid).success).toBe(true);
  });

  it("rejects an uppercase/underscore slug", () => {
    expect(userProviderCreateSchema.safeParse({ ...valid, slug: "Bad_Slug" }).success).toBe(false);
  });

  it("rejects an unknown kind", () => {
    expect(userProviderCreateSchema.safeParse({ ...valid, kind: "bitbucket" }).success).toBe(false);
  });

  it("rejects a relative base URL", () => {
    expect(userProviderCreateSchema.safeParse({ ...valid, baseUrl: "/relative" }).success).toBe(
      false,
    );
  });

  it("rejects an empty token", () => {
    expect(userProviderCreateSchema.safeParse({ ...valid, token: "" }).success).toBe(false);
  });

  it("accepts an empty baseUrl when kind is github", () => {
    const result = userProviderCreateSchema.safeParse({
      ...valid,
      kind: "github",
      baseUrl: "",
    });
    expect(result.success).toBe(true);
  });
});

describe("userProviderEditSchema", () => {
  const base = { displayName: "GitHub", kind: "github" as const, baseUrl: "" };

  it("accepts an empty token, meaning keep the current one", () => {
    expect(userProviderEditSchema.safeParse({ ...base, token: "" }).success).toBe(true);
  });

  it("accepts a valid non-empty token", () => {
    expect(userProviderEditSchema.safeParse({ ...base, token: "ghp_xxxx" }).success).toBe(true);
  });
});
