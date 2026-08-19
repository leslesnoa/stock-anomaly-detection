"use server";

import { redirect } from "next/navigation";
import { loginUser } from "@/lib/go-api-client";
import { setSessionToken } from "@/lib/session";

export type AuthFormState = {
  error?: string;
};

export async function loginAction(
  _prevState: AuthFormState,
  formData: FormData,
): Promise<AuthFormState> {
  const email = formData.get("email");
  const password = formData.get("password");
  if (typeof email !== "string" || typeof password !== "string") {
    return { error: "invalid form data" };
  }

  const result = await loginUser(email, password);
  if (!result.ok) {
    return { error: result.error };
  }

  await setSessionToken(result.token);
  redirect("/watchlist");
}
