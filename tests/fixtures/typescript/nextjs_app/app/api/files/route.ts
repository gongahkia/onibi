const db = {
  query(sql: string) {
    return sql;
  },
};

const fs = {
  readFile(path: string, callback: () => void) {
    callback();
    return path;
  },
};

export async function GET(request: any) {
  const trace = request.headers.get("x-trace-id");
  const file = request.nextUrl.searchParams.get("file");
  const target = "/srv/uploads/" + file;

  fs.readFile(target, () => {});
  return Response.json({ trace });
}

export async function POST(request: any) {
  const body = await request.json();
  const raw = await request.text();
  const form = await request.formData();
  const lookup = "SELECT * FROM files WHERE id = '" + body.id + "'";

  db.query(lookup);
  return Response.json({ raw, name: form.get("name") });
}
