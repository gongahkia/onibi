import { useEffect, useRef, useState } from "react";
import jsQR from "jsqr";

interface QRScannerProps {
  onDecoded: (text: string) => void;
  onClose: () => void;
}

/**
 * Camera-based scanner. Uses the native BarcodeDetector where available,
 * falls back to jsQR on a <canvas> snapshot. Streaming stops as soon as
 * a payload is decoded or the component unmounts.
 */
export function QRScanner({ onDecoded, onClose }: QRScannerProps): JSX.Element {
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const onDecodedRef = useRef(onDecoded);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    onDecodedRef.current = onDecoded;
  }, [onDecoded]);

  useEffect(() => {
    let stopped = false;
    let stream: MediaStream | null = null;
    let frame = 0;

    const runOnce = async () => {
      const video = videoRef.current;
      const canvas = canvasRef.current;
      if (!video || !canvas || stopped) return;

      if (video.readyState < video.HAVE_CURRENT_DATA) {
        frame = requestAnimationFrame(runOnce);
        return;
      }

      const width = video.videoWidth;
      const height = video.videoHeight;
      if (!width || !height) {
        frame = requestAnimationFrame(runOnce);
        return;
      }

      canvas.width = width;
      canvas.height = height;
      const ctx = canvas.getContext("2d", { willReadFrequently: true });
      if (!ctx) return;

      ctx.drawImage(video, 0, 0, width, height);
      const imageData = ctx.getImageData(0, 0, width, height);
      const code = jsQR(imageData.data, width, height, { inversionAttempts: "attemptBoth" });
      if (code && code.data) {
        stopped = true;
        onDecodedRef.current(code.data);
        return;
      }

      frame = requestAnimationFrame(runOnce);
    };

    (async () => {
      if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
        setError("This browser does not support camera access. Paste the payload instead.");
        return;
      }
      try {
        stream = await navigator.mediaDevices.getUserMedia({
          video: { facingMode: { ideal: "environment" } },
          audio: false
        });
        if (stopped) {
          for (const track of stream.getTracks()) track.stop();
          return;
        }
        const video = videoRef.current;
        if (!video) return;
        video.srcObject = stream;
        try {
          await video.play();
        } catch (playErr) {
          if (playErr instanceof Error && (playErr.name === "AbortError" || /interrupted/i.test(playErr.message))) {
            return;
          }
          throw playErr;
        }
        if (stopped) return;
        runOnce();
      } catch (err) {
        const message = err instanceof Error ? err.message : "Unknown camera error";
        setError(`Camera denied or unavailable: ${message}`);
      }
    })();

    return () => {
      stopped = true;
      cancelAnimationFrame(frame);
      if (stream) {
        for (const track of stream.getTracks()) {
          track.stop();
        }
      }
    };
  }, []);

  return (
    <div className="mf-qr-scanner">
      <header>
        <span>Point the camera at the Onibi QR</span>
        <button type="button" className="button-secondary" onClick={onClose}>
          Cancel
        </button>
      </header>
      <video ref={videoRef} playsInline muted autoPlay />
      <canvas ref={canvasRef} style={{ display: "none" }} />
      {error && (
        <p className="mf-alert mf-alert-warning" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
