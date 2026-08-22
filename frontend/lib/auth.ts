import { redirect } from "next/navigation";
import type { ApiFailure } from "@/lib/go-api-client";
import { getSessionToken, clearSessionToken } from "@/lib/session";

export type ActionResult = { ok: true } | { ok: false; error: string };

export async function requireToken(): Promise<
  { ok: true; token: string } | { ok: false; error: string }
> {
  const token = await getSessionToken();
  if (!token) {
    return { ok: false, error: "unauthorized" };
  }
  return { ok: true, token };
}

export async function redirectIfUnauthorized(
  result: ApiFailure,
): Promise<void> {
  if (result.status === 401) {
    await clearSessionToken();
    redirect("/login");
  }
}
