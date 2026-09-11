"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { addWatchlistItem, removeWatchlistItem } from "@/lib/go-api-client";
import { clearSessionToken } from "@/lib/session";
import {
  requireToken,
  redirectIfUnauthorized,
  type ActionResult,
} from "@/lib/auth";

export type { ActionResult } from "@/lib/auth";

export async function addAction(
  _prevState: ActionResult | null,
  formData: FormData,
): Promise<ActionResult> {
  const tokenResult = await requireToken();
  if (!tokenResult.ok) {
    return tokenResult;
  }
  const stockCode = formData.get("stock_code");
  if (typeof stockCode !== "string") {
    return { ok: false, error: "invalid form data" };
  }
  const result = await addWatchlistItem(tokenResult.token, stockCode);
  if (!result.ok) {
    await redirectIfUnauthorized(result);
    return { ok: false, error: result.error };
  }
  revalidatePath("/watchlist");
  return { ok: true };
}

export async function removeAction(id: string): Promise<ActionResult> {
  const tokenResult = await requireToken();
  if (!tokenResult.ok) {
    return tokenResult;
  }
  const result = await removeWatchlistItem(tokenResult.token, id);
  if (!result.ok) {
    await redirectIfUnauthorized(result);
    return { ok: false, error: result.error };
  }
  revalidatePath("/watchlist");
  return { ok: true };
}

export async function logoutAction(): Promise<void> {
  await clearSessionToken();
  redirect("/login");
}
