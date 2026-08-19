"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { addWatchlistItem, type ApiFailure } from "@/lib/go-api-client";
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

export async function addAction(
  _prevState: ActionResult | null,
  formData: FormData,
): Promise<ActionResult> {
  const tokenResult = await requireToken();
  if (!tokenResult.ok) {
    return tokenResult;
  }
  const stockCode = formData.get("stock_code");
  const alertThreshold = formData.get("alert_threshold");
  if (typeof stockCode !== "string" || typeof alertThreshold !== "string") {
    return { ok: false, error: "invalid form data" };
  }
  const result = await addWatchlistItem(
    tokenResult.token,
    stockCode,
    Number(alertThreshold),
  );
  if (!result.ok) {
    await redirectIfUnauthorized(result);
    return { ok: false, error: result.error };
  }
  revalidatePath("/watchlist");
  return { ok: true };
}
