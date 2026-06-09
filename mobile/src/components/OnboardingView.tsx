import { useEffect, useState } from "react";
import { installStateBody, installStateTitle } from "../install-prompt";
import type { BeforeInstallPromptEvent } from "../types";

export function OnboardingView() {
  const ios = typeof navigator !== "undefined" && /iPad|iPhone|iPod/.test(navigator.userAgent);
  const standalone =
    typeof window !== "undefined" &&
    (window.matchMedia?.("(display-mode: standalone)").matches ||
      (navigator as Navigator & { standalone?: boolean }).standalone === true);
  const [installPrompt, setInstallPrompt] = useState<BeforeInstallPromptEvent | null>(null);
  const [installed, setInstalled] = useState(standalone);

  useEffect(() => {
    const onBeforeInstallPrompt = (event: Event) => {
      event.preventDefault();
      setInstallPrompt(event as BeforeInstallPromptEvent);
    };
    const onInstalled = () => {
      setInstalled(true);
      setInstallPrompt(null);
    };
    window.addEventListener("beforeinstallprompt", onBeforeInstallPrompt);
    window.addEventListener("appinstalled", onInstalled);
    return () => {
      window.removeEventListener("beforeinstallprompt", onBeforeInstallPrompt);
      window.removeEventListener("appinstalled", onInstalled);
    };
  }, []);

  const promptInstall = async () => {
    const prompt = installPrompt;
    if (!prompt) {
      return;
    }
    await prompt.prompt();
    await prompt.userChoice;
    setInstallPrompt(null);
  };

  return (
    <div className="onboarding">
      <strong>{installStateTitle({ standalone: installed, ios, installable: Boolean(installPrompt) })}</strong>
      <span>
        {installStateBody({ standalone: installed, ios, installable: Boolean(installPrompt) })}
      </span>
      {installPrompt && !installed ? (
        <button type="button" className="ghost-button" onClick={() => void promptInstall()}>
          Install
        </button>
      ) : null}
    </div>
  );
}
