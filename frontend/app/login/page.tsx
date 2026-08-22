import { LoginForm } from "@/components/login-form";

export default function LoginPage() {
  return (
    <main className="mx-auto flex max-w-sm flex-col gap-6 p-8">
      <h1 className="text-2xl font-bold">ログイン</h1>
      <LoginForm />
      <a href="/register" className="text-sm underline">
        アカウントをお持ちでない方はこちら
      </a>
    </main>
  );
}
