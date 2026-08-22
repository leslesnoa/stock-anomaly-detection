import { RegisterForm } from "@/components/register-form";

export default function RegisterPage() {
  return (
    <main className="mx-auto flex max-w-sm flex-col gap-6 p-8">
      <h1 className="text-2xl font-bold">新規登録</h1>
      <RegisterForm />
      <a href="/login" className="text-sm underline">
        既にアカウントをお持ちの方はこちら
      </a>
    </main>
  );
}
