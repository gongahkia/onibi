import { readFileSync } from "node:fs";
import { resolve } from "node:path";

type ManifestIcon = {
  src?: string;
  sizes?: string;
  type?: string;
  purpose?: string;
};

type WebManifest = {
  name?: string;
  short_name?: string;
  description?: string;
  id?: string;
  start_url?: string;
  scope?: string;
  display?: string;
  theme_color?: string;
  background_color?: string;
  icons?: ManifestIcon[];
  shortcuts?: Array<{
    name?: string;
    short_name?: string;
    url?: string;
    icons?: ManifestIcon[];
  }>;
};

test("served web app manifest is installable", () => {
  const manifest = readManifest("public/manifest.webmanifest");

  expect(manifest.name).toBe("Onibi");
  expect(manifest.short_name).toBe("Onibi");
  expect(manifest.description).toContain("Phone cockpit");
  expect(manifest.id).toBe("/");
  expect(manifest.start_url).toBe("/");
  expect(manifest.scope).toBe("/");
  expect(["standalone", "fullscreen"]).toContain(manifest.display);
  expect(manifest.theme_color).toMatch(/^#[0-9A-Fa-f]{6}$/);
  expect(manifest.background_color).toMatch(/^#[0-9A-Fa-f]{6}$/);
  expect(hasIcon(manifest, "192x192", "any")).toBe(true);
  expect(hasIcon(manifest, "512x512", "any")).toBe(true);
  expect(hasIcon(manifest, "192x192", "maskable")).toBe(true);
  expect(hasIcon(manifest, "512x512", "maskable")).toBe(true);
  expect(manifest.shortcuts?.map((shortcut) => shortcut.url)).toEqual(["/", "/#/sessions"]);
});

test("source manifest copy stays in sync with served manifest", () => {
  expect(readManifest("manifest.webmanifest")).toEqual(readManifest("public/manifest.webmanifest"));
});

test("index declares iOS and Android install metadata", () => {
  const html = readFileSync(resolve(process.cwd(), "index.html"), "utf8");

  expect(html).toContain('<link rel="manifest" href="/manifest.webmanifest">');
  expect(html).toContain('<meta name="mobile-web-app-capable" content="yes">');
  expect(html).toContain('<meta name="apple-mobile-web-app-capable" content="yes">');
  expect(html).toContain('<meta name="apple-mobile-web-app-title" content="Onibi">');
  expect(html).toContain(
    '<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">'
  );
  expect(html).toContain('<link rel="apple-touch-icon" sizes="512x512" href="/icons/onibi-512.png">');
});

function readManifest(path: string): WebManifest {
  return JSON.parse(readFileSync(resolve(process.cwd(), path), "utf8")) as WebManifest;
}

function hasIcon(manifest: WebManifest, size: string, purpose: string): boolean {
  return (manifest.icons ?? []).some((icon) => {
    const purposes = (icon.purpose ?? "any").split(/\s+/);
    return icon.sizes === size && purposes.includes(purpose) && icon.type === "image/png";
  });
}
