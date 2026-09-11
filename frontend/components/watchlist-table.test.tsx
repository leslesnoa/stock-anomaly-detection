import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { WatchlistTable } from "./watchlist-table";

vi.mock("@/app/watchlist/actions", () => ({
  removeAction: vi.fn(),
}));

import * as actions from "@/app/watchlist/actions";

describe("WatchlistTable", () => {
  it("renders each watchlist item's stock code", () => {
    render(
      <WatchlistTable
        items={[
          { id: "1", stock_code: "7203", alert_threshold: 2.5 },
          { id: "2", stock_code: "9984", alert_threshold: 3.0 },
        ]}
      />,
    );

    expect(screen.getByText("7203")).toBeInTheDocument();
    expect(screen.getByText("9984")).toBeInTheDocument();
  });

  it("renders nothing in the body when items is empty", () => {
    render(<WatchlistTable items={[]} />);

    expect(screen.getAllByRole("row")).toHaveLength(1);
  });
});

describe("WatchlistTable row actions", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("calls removeAction with the item id when the delete button is clicked", async () => {
    vi.mocked(actions.removeAction).mockResolvedValue({ ok: true });

    render(
      <WatchlistTable
        items={[{ id: "1", stock_code: "7203", alert_threshold: 2.5 }]}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "削除" }));

    await waitFor(() => {
      expect(actions.removeAction).toHaveBeenCalledWith("1");
    });
  });
});
