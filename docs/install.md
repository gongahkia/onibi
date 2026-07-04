# Installer Publication

`https://get.onibi.sh` should be a tiny GitHub Pages site whose root document is the rendered installer script. It must not serve the raw template from `scripts/install.sh`, because the raw template intentionally fails closed until the release GPG public key is embedded.

## Render

Export the same public key used by the release workflow:

```bash
GPG_PUBLIC_KEY_B64="$(gpg --armor --export "$GPG_FINGERPRINT" | base64 | tr -d '\n')"
scripts/render-install.sh /path/to/get-onibi-pages/index.html
```

The rendered `index.html` is shell script content. GitHub Pages serves it for `curl -sSf https://get.onibi.sh`; the file extension only gives Pages a root document.

## GitHub Pages

Use a small Pages repository or branch dedicated to the installer endpoint:

```text
index.html
CNAME
```

`CNAME` must contain:

```text
get.onibi.sh
```

DNS must point `get.onibi.sh` at GitHub Pages for that site. After DNS and Pages publish, verify:

```bash
curl -sSf https://get.onibi.sh >/tmp/onibi-install.sh
! grep -q '__ONIBI_RELEASE_GPG_KEY_B64__' /tmp/onibi-install.sh
sh -n /tmp/onibi-install.sh
```

The issue is not complete until:

```bash
curl -sSf https://get.onibi.sh
```

returns the rendered installer.
