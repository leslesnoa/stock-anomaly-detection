import { describe, it, expect, vi, beforeEach } from "vitest";

vi.mock("@/lib/session", () => ({
  getSessionToken: vi.fn(),
  clearSessionToken: vi.fn(),
}));
vi.mock("next/navigation", () => ({
  redirect: vi.fn(),
}));

import { requireToken, redirectIfUnauthorized } from "./auth";
import * as session from "@/lib/session";
import { redirect } from "next/navigation";

describe("requireToken", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("returns the token when a session cookie exists", async () => {
    vi.mocked(session.getSessionToken).mockResolvedValue("jwt-token");

    const result = await requireToken();

    expect(result).toEqual({ ok: true, token: "jwt-token" });
  });

  it("returns an error when no session cookie exists", async () => {
    vi.mocked(session.getSessionToken).mockResolvedValue(undefined);

    const result = await requireToken();

    expect(result).toEqual({ ok: false, error: "unauthorized" });
  });
});

describe("redirectIfUnauthorized", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("clears the session and redirects on a 401 failure", async () => {
    await redirectIfUnauthorized({
      ok: false,
      error: "missing user context",
      status: 401,
    });

    expect(session.clearSessionToken).toHaveBeenCalled();
    expect(redirect).toHaveBeenCalledWith("/login");
  });

  it("does nothing on a non-401 failure", async () => {
    await redirectIfUnauthorized({
      ok: false,
      error: "internal server error",
      status: 500,
    });

    expect(session.clearSessionToken).not.toHaveBeenCalled();
    expect(redirect).not.toHaveBeenCalled();
  });
});
