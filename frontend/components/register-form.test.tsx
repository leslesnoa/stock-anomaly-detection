import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

vi.mock("@/app/register/actions", () => ({
  registerAction: vi.fn(async () => ({ error: "email already exists" })),
}));

import { RegisterForm } from "./register-form";

describe("RegisterForm", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders email and password fields", () => {
    render(<RegisterForm />);

    expect(screen.getByLabelText("メールアドレス")).toBeInTheDocument();
    expect(screen.getByLabelText("パスワード")).toBeInTheDocument();
  });

  it("shows the error message returned by registerAction", async () => {
    render(<RegisterForm />);

    fireEvent.change(screen.getByLabelText("メールアドレス"), {
      target: { value: "test@example.com" },
    });
    fireEvent.change(screen.getByLabelText("パスワード"), {
      target: { value: "password123" },
    });
    fireEvent.click(screen.getByRole("button", { name: "登録" }));

    await waitFor(() => {
      expect(screen.getByText("email already exists")).toBeInTheDocument();
    });
  });
});
