import { describe, it, expect, vi, beforeEach } from "vitest";

vi.mock("@/lib/go-api-client", () => ({
  registerUser: vi.fn(),
  loginUser: vi.fn(),
}));
vi.mock("@/lib/session", () => ({
  setSessionToken: vi.fn(),
}));
vi.mock("next/navigation", () => ({
  redirect: vi.fn(),
}));

import { registerAction } from "./actions";
import * as goApiClient from "@/lib/go-api-client";
import * as session from "@/lib/session";
import { redirect } from "next/navigation";

function formData(email: string, password: string): FormData {
  const fd = new FormData();
  fd.set("email", email);
  fd.set("password", password);
  return fd;
}

describe("registerAction", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("returns an error when registration fails", async () => {
    vi.mocked(goApiClient.registerUser).mockResolvedValue({
      ok: false,
      error: "email already exists",
      status: 409,
    });

    const result = await registerAction(
      {},
      formData("test@example.com", "password123"),
    );

    expect(result).toEqual({ error: "email already exists" });
    expect(goApiClient.loginUser).not.toHaveBeenCalled();
  });

  it("logs in and redirects to /watchlist on successful registration", async () => {
    vi.mocked(goApiClient.registerUser).mockResolvedValue({ ok: true });
    vi.mocked(goApiClient.loginUser).mockResolvedValue({
      ok: true,
      token: "jwt-token",
    });

    await registerAction({}, formData("test@example.com", "password123"));

    expect(session.setSessionToken).toHaveBeenCalledWith("jwt-token");
    expect(redirect).toHaveBeenCalledWith("/watchlist");
  });

  it("returns an error when the post-registration login fails", async () => {
    vi.mocked(goApiClient.registerUser).mockResolvedValue({ ok: true });
    vi.mocked(goApiClient.loginUser).mockResolvedValue({
      ok: false,
      error: "invalid credentials",
      status: 401,
    });

    const result = await registerAction(
      {},
      formData("test@example.com", "password123"),
    );

    expect(result).toEqual({ error: "invalid credentials" });
    expect(redirect).not.toHaveBeenCalled();
  });
});
