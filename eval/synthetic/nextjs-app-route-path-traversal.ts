import fs from "node:fs";

export async function GET(request: any) {
  const file = request.nextUrl.searchParams.get("file");
  const target = "/srv/uploads/" + file;

  fs.readFile(target, () => {});
  return Response.json({ ok: true });
}
