export type WatchlistItem = {
  id: string;
  stock_code: string;
  alert_threshold: number;
};

export type ApiFailure = {
  ok: false;
  error: string;
  status: number;
};

type ApiErrorBody = { error: string };

const GO_API_URL = (() => {
  const url = process.env.GO_API_URL;
  if (!url) {
    if (process.env.NODE_ENV === "production") {
      throw new Error("GO_API_URL must be set in production");
    }
    return "http://localhost:8080";
  }
  return url;
})();

async function toFailure(res: Response): Promise<ApiFailure> {
  const body = (await res.json()) as ApiErrorBody;
  return { ok: false, error: body.error, status: res.status };
}

export async function registerUser(
  email: string,
  password: string,
): Promise<{ ok: true } | ApiFailure> {
  const res = await fetch(`${GO_API_URL}/auth/register`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
  if (res.status === 201) {
    return { ok: true };
  }
  return toFailure(res);
}

export async function loginUser(
  email: string,
  password: string,
): Promise<{ ok: true; token: string } | ApiFailure> {
  const res = await fetch(`${GO_API_URL}/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
  if (res.status === 200) {
    const body = (await res.json()) as { token: string };
    return { ok: true, token: body.token };
  }
  return toFailure(res);
}

export async function fetchWatchlist(
  token: string,
): Promise<{ ok: true; items: WatchlistItem[] } | ApiFailure> {
  const res = await fetch(`${GO_API_URL}/watchlist`, {
    headers: { Authorization: `Bearer ${token}` },
    cache: "no-store",
  });
  if (res.status === 200) {
    const items = (await res.json()) as WatchlistItem[];
    return { ok: true, items };
  }
  return toFailure(res);
}

export async function addWatchlistItem(
  token: string,
  stockCode: string,
): Promise<{ ok: true; item: WatchlistItem } | ApiFailure> {
  const res = await fetch(`${GO_API_URL}/watchlist`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({ stock_code: stockCode }),
  });
  if (res.status === 201) {
    const item = (await res.json()) as WatchlistItem;
    return { ok: true, item };
  }
  return toFailure(res);
}

export async function removeWatchlistItem(
  token: string,
  id: string,
): Promise<{ ok: true } | ApiFailure> {
  const res = await fetch(`${GO_API_URL}/watchlist/${id}`, {
    method: "DELETE",
    headers: { Authorization: `Bearer ${token}` },
  });
  if (res.status === 204) {
    return { ok: true };
  }
  return toFailure(res);
}
