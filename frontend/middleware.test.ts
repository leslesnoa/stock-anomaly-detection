import { describe, it, expect } from "vitest";
import { NextRequest } from "next/server";
import { middleware } from "./middleware";

describe("middleware", () => {
  it("redirects to /login when accessing /watchlist without a token cookie", () => {
    const req = new NextRequest("http://localhost:3000/watchlist");

    const res = middleware(req);

    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe("http://localhost:3000/login");
  });

  it("passes through to /watchlist when a token cookie is present", () => {
    const req = new NextRequest("http://localhost:3000/watchlist", {
      headers: { cookie: "token=jwt-token" },
    });

    const res = middleware(req);

    expect(res.status).toBe(200);
  });

  it("redirects to /watchlist when accessing /login with a token cookie", () => {
    const req = new NextRequest("http://localhost:3000/login", {
      headers: { cookie: "token=jwt-token" },
    });

    const res = middleware(req);

    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe("http://localhost:3000/watchlist");
  });

  it("passes through to /login when no token cookie is present", () => {
    const req = new NextRequest("http://localhost:3000/login");

    const res = middleware(req);

    expect(res.status).toBe(200);
  });
});
