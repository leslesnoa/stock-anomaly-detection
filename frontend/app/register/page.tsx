import Link from "next/link";
import { RegisterForm } from "@/components/register-form";
import { Logo } from "@/components/logo";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";

export default function RegisterPage() {
  return (
    <main className="flex min-h-dvh flex-col items-center justify-center gap-6 bg-muted/30 p-6">
      <Logo />
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <CardTitle className="text-xl">新規登録</CardTitle>
          <CardDescription>
            銘柄の異常を見逃さないために登録しましょう
          </CardDescription>
        </CardHeader>
        <CardContent>
          <RegisterForm />
        </CardContent>
        <CardFooter className="justify-center">
          <Link
            href="/login"
            className="text-sm text-primary underline-offset-4 hover:underline"
          >
            既にアカウントをお持ちの方はこちら
          </Link>
        </CardFooter>
      </Card>
    </main>
  );
}
