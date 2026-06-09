export function installStateTitle({
  standalone,
  ios,
  installable,
}: {
  standalone: boolean;
  ios: boolean;
  installable: boolean;
}): string {
  if (standalone) {
    return "Installed PWA.";
  }
  if (ios) {
    return "Add to Home Screen for push.";
  }
  return installable ? "Ready to install." : "Installable PWA.";
}

export function installStateBody({
  standalone,
  ios,
  installable,
}: {
  standalone: boolean;
  ios: boolean;
  installable: boolean;
}): string {
  if (standalone) {
    return "Lock-screen notification deep links open this paired machine.";
  }
  if (ios) {
    return "Open from Safari, share, then Add to Home Screen.";
  }
  return installable
    ? "Install this app for persistent notifications."
    : "Install from the browser menu for persistent notifications.";
}
