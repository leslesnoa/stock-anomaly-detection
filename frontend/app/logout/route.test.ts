import { describe, it, expect, vi, beforeEach } from "vitest";

vi.mock("@/lib/session", () => ({
  clearSessionToken: vi.fn(),
}));

import { GET } from "./route";
import * as session from "@/lib/session";

describe("GET /logout", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("clears the session cookie and redirects to /login", async () => {
    const request = new Request("http://localhost:3000/logout");

    const response = await GET(request);

    expect(session.clearSessionToken).toHaveBeenCalled();
    expect(response.status).toBe(307);
    expect(response.headers.get("location")).toBe(
      "http://localhost:3000/login",
    );
  });
});
