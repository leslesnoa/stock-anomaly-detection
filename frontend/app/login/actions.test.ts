import { describe, it, expect, vi, beforeEach } from "vitest";

vi.mock("@/lib/go-api-client", () => ({
  loginUser: vi.fn(),
}));
vi.mock("@/lib/session", () => ({
  setSessionToken: vi.fn(),
}));
vi.mock("next/navigation", () => ({
  redirect: vi.fn(),
}));

import { loginAction } from "./actions";
import * as goApiClient from "@/lib/go-api-client";
import * as session from "@/lib/session";
import { redirect } from "next/navigation";

function formData(email: string, password: string): FormData {
  const fd = new FormData();
  fd.set("email", email);
  fd.set("password", password);
  return fd;
}

describe("loginAction", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("returns an error when login fails", async () => {
    vi.mocked(goApiClient.loginUser).mockResolvedValue({
      ok: false,
      error: "invalid credentials",
      status: 401,
    });

    const result = await loginAction(
      {},
      formData("test@example.com", "wrong-password"),
    );

    expect(result).toEqual({ error: "invalid credentials" });
  });

  it("sets the session cookie and redirects to /watchlist on success", async () => {
    vi.mocked(goApiClient.loginUser).mockResolvedValue({
      ok: true,
      token: "jwt-token",
    });

    await loginAction({}, formData("test@example.com", "password123"));

    expect(session.setSessionToken).toHaveBeenCalledWith("jwt-token");
    expect(redirect).toHaveBeenCalledWith("/watchlist");
  });
});
