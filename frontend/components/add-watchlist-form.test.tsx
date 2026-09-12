import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

vi.mock("@/app/watchlist/actions", () => ({
  addAction: vi.fn(async () => ({
    ok: false,
    error: "stock already in watchlist",
  })),
}));

import { AddWatchlistForm } from "./add-watchlist-form";

describe("AddWatchlistForm", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders the stock code field", () => {
    render(<AddWatchlistForm />);

    expect(screen.getByLabelText("証券コード")).toBeInTheDocument();
    expect(screen.queryByLabelText("閾値")).not.toBeInTheDocument();
  });

  it("shows the error message returned by addAction", async () => {
    render(<AddWatchlistForm />);

    fireEvent.change(screen.getByLabelText("証券コード"), {
      target: { value: "7203" },
    });
    fireEvent.click(screen.getByRole("button", { name: "追加" }));

    await waitFor(() => {
      expect(
        screen.getByText("stock already in watchlist"),
      ).toBeInTheDocument();
    });
  });
});
