import { NextRequest, NextResponse } from "next/server";

const PROTECTED_PATHS = ["/watchlist"];
const AUTH_PATHS = ["/login", "/register"];

export function middleware(request: NextRequest) {
  const token = request.cookies.get("token")?.value;
  const { pathname } = request.nextUrl;

  if (PROTECTED_PATHS.some((p) => pathname.startsWith(p)) && !token) {
    return NextResponse.redirect(new URL("/login", request.url));
  }

  if (AUTH_PATHS.some((p) => pathname.startsWith(p)) && token) {
    return NextResponse.redirect(new URL("/watchlist", request.url));
  }

  return NextResponse.next();
}

export const config = {
  matcher: ["/watchlist/:path*", "/login", "/register"],
};
