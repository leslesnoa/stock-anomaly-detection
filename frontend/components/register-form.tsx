"use client";

import { useActionState } from "react";
import { registerAction, type AuthFormState } from "@/app/register/actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const initialState: AuthFormState = {};

export function RegisterForm() {
  const [state, formAction, isPending] = useActionState(
    registerAction,
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
        <Input
          id="password"
          name="password"
          type="password"
          required
          minLength={8}
        />
      </div>
      <Button type="submit" disabled={isPending}>
        登録
      </Button>
      {state.error && <p className="text-sm text-red-500">{state.error}</p>}
    </form>
  );
}
