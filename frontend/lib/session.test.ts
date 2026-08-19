import { describe, it, expect, vi, beforeEach } from "vitest";

const mockCookieStore = {
  set: vi.fn(),
  get: vi.fn(),
  delete: vi.fn(),
};

vi.mock("next/headers", () => ({
  cookies: vi.fn(async () => mockCookieStore),
}));

import { setSessionToken, getSessionToken, clearSessionToken } from "./session";

describe("session", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("setSessionToken sets an httpOnly cookie named token", async () => {
    await setSessionToken("jwt-token");

    expect(mockCookieStore.set).toHaveBeenCalledWith(
      "token",
      "jwt-token",
      expect.objectContaining({
        httpOnly: true,
        sameSite: "lax",
        maxAge: 60 * 60 * 24,
        path: "/",
      }),
    );
  });

  it("getSessionToken returns the cookie value", async () => {
    mockCookieStore.get.mockReturnValue({ value: "jwt-token" });

    const token = await getSessionToken();

    expect(token).toBe("jwt-token");
    expect(mockCookieStore.get).toHaveBeenCalledWith("token");
  });

  it("getSessionToken returns undefined when cookie is absent", async () => {
    mockCookieStore.get.mockReturnValue(undefined);

    const token = await getSessionToken();

    expect(token).toBeUndefined();
  });

  it("clearSessionToken deletes the cookie", async () => {
    await clearSessionToken();

    expect(mockCookieStore.delete).toHaveBeenCalledWith("token");
  });
});
