"use client";

import { useActionState } from "react";
import { loginAction, type AuthFormState } from "@/app/login/actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const initialState: AuthFormState = {};

export function LoginForm() {
  const [state, formAction, isPending] = useActionState(
    loginAction,
    initialState,
  );

  return (
    <form action={formAction} className="flex flex-col gap-4">
      <div>
        <Label htmlFor="email">メールアドレス</Label>
        <Input id="email" name="email" type="email" required />
      </div>
      <div>
        <Label htmlFor="password">パスワード</Label>
        <Input id="password" name="password" type="password" required />
      </div>
      <Button type="submit" disabled={isPending}>
        ログイン
      </Button>
      {state.error && <p className="text-sm text-red-500">{state.error}</p>}
    </form>
  );
}
