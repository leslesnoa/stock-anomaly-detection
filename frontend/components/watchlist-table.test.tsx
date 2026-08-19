import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { WatchlistTable } from "./watchlist-table";

describe("WatchlistTable", () => {
  it("renders each watchlist item's stock code and threshold", () => {
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
