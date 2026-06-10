import { useState } from "react";
import type { FormEvent } from "react";
import { authedFetch } from "../http";
import { connectionId } from "../connections";
import { chooseBaseUrl, parsePairingInput } from "../pairing";
import { createPushSubscription } from "../push";
import { scanQrImage } from "../qr";
import { DEVICE_LABEL } from "../constants";
import type { Connection, PairResponse } from "../types";
import { OnboardingView } from "./OnboardingView";

export function PairingView({
  onPaired,
  embedded = false,
}: {
  onPaired: (connection: Connection) => void;
  embedded?: boolean;
}) {
  const [raw, setRaw] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("Paste the pairing payload from Onibi.");

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setMessage("Pairing...");
    try {
      const payload = parsePairingInput(raw);
      const baseUrl = chooseBaseUrl(payload);
      const vapidPublicKey = payload.vapid_public_key ?? payload.vapidPublicKey;
      const pushSubscription = await createPushSubscription(vapidPublicKey);
      const response = await authedFetch(baseUrl, payload.token, "/v1/pair", {
        method: "POST",
        body: JSON.stringify({
          deviceLabel: DEVICE_LABEL,
          pushSubscription,
        }),
      });
      const paired = (await response.json()) as PairResponse;
      onPaired({
        id: connectionId(paired.machineId, paired.deviceId),
        baseUrl,
        token: payload.token,
        deviceId: paired.deviceId,
        machineId: paired.machineId,
        scope: paired.scope ?? payload.scope ?? "full",
        vapidPublicKey,
        transports: payload.transports ?? [],
      });
    } catch (error) {
      setMessage(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  };

  const scanFile = async (file: File | undefined) => {
    if (!file) {
      return;
    }
    setBusy(true);
    setMessage("Scanning QR image...");
    try {
      setRaw(await scanQrImage(file));
      setMessage("QR payload scanned.");
    } catch (error) {
      setMessage(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  };

  const content = (
    <section className="pairing-panel" aria-labelledby="pair-title">
        <div className="brand-row">
          <img src="/favicon.svg" alt="" className="brand-mark" />
          <div>
            <p className="eyebrow">Onibi</p>
            <h1 id="pair-title">Pair this device</h1>
          </div>
        </div>

        <form onSubmit={submit} className="pair-form">
          <textarea
            value={raw}
            onChange={(event) => setRaw(event.target.value)}
            placeholder='{"token":"...","transports":[{"url":"https://..."}]}'
            rows={8}
            aria-label="Pairing payload"
          />
          <div className="pair-actions">
            <label className="secondary-button">
              Scan QR image
              <input
                type="file"
                accept="image/*"
                onChange={(event) => void scanFile(event.target.files?.[0])}
              />
            </label>
            <button type="submit" disabled={busy || raw.trim().length === 0}>
              Pair
            </button>
          </div>
        </form>
        <p className="status-line">{message}</p>
        <OnboardingView />
    </section>
  );
  return embedded ? content : <main className="pairing-screen">{content}</main>;
}
