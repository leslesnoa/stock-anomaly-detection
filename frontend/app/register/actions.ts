"use server";

import { redirect } from "next/navigation";
import { registerUser, loginUser } from "@/lib/go-api-client";
import { setSessionToken } from "@/lib/session";

export type AuthFormState = {
  error?: string;
};

export async function registerAction(
  _prevState: AuthFormState,
  formData: FormData,
): Promise<AuthFormState> {
  const email = formData.get("email");
  const password = formData.get("password");
  if (typeof email !== "string" || typeof password !== "string") {
    return { error: "invalid form data" };
  }

  const registerResult = await registerUser(email, password);
  if (!registerResult.ok) {
    return { error: registerResult.error };
  }

  const loginResult = await loginUser(email, password);
  if (!loginResult.ok) {
    return { error: loginResult.error };
  }

  await setSessionToken(loginResult.token);
  redirect("/watchlist");
}
