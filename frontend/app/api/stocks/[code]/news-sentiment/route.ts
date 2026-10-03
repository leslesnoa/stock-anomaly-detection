import { NextResponse } from "next/server";
import { requireToken } from "@/lib/auth";
import { fetchNewsSentiment } from "@/lib/go-api-client";

// Server Action ではなく Route Handler にしているのは、Server Action が直列に実行されポーリング中に他の操作を待たせるため。
// go-api はキャッシュ切れの時に最大20秒待ってから応答する。
export const maxDuration = 30;

export async function GET(
  _request: Request,
  ctx: { params: Promise<{ code: string }> },
) {
  const tokenResult = await requireToken();
  if (!tokenResult.ok) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }
  const { code } = await ctx.params;
  const result = await fetchNewsSentiment(tokenResult.token, code);
  if (!result.ok) {
    return NextResponse.json(
      { error: result.error },
      { status: result.status },
    );
  }
  return NextResponse.json(result.sentiment);
}
