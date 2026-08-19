import { NextResponse } from "next/server";
import { clearSessionToken } from "@/lib/session";

export async function GET(request: Request) {
  await clearSessionToken();
  return NextResponse.redirect(new URL("/login", request.url));
}
