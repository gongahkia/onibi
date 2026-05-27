import type { RealtimeConnectionState } from "../api/realtimeClient";

interface StaleBannerProps {
  realtimeState: RealtimeConnectionState;
  reconnectAttempts: number;
  threshold?: number;
  onRetry: () => void;
}

export function StaleBanner({
  realtimeState,
  reconnectAttempts,
  threshold = 3,
  onRetry
}: StaleBannerProps): JSX.Element | null {
  const disconnected = realtimeState === "disconnected" || realtimeState === "reconnecting";
  if (!disconnected || reconnectAttempts < threshold) {
    return null;
  }

  return (
    <div className="mf-stale-banner" role="status" aria-live="polite">
      <span>
        Lost realtime connection. Retrying (attempt {reconnectAttempts}). Check the host is awake and the tunnel is up.
      </span>
      <button type="button" className="button-secondary" onClick={onRetry}>
        Retry now
      </button>
    </div>
  );
}
